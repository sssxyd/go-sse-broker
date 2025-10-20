package sse

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

func getRealIP(c *fiber.Ctx) string {
	xff := c.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.SplitSeq(xff, ",")
		for ip := range ips {
			trimmedIP := strings.TrimSpace(ip)
			if trimmedIP != "" {
				return trimmedIP
			}
		}
	}
	xRealIP := c.Get("X-Real-IP")
	if xRealIP != "" {
		return xRealIP
	}
	remoteIP, _, err := net.SplitHostPort(c.Context().RemoteAddr().String())
	if err != nil {
		return c.Context().RemoteAddr().String()
	}
	return remoteIP
}

func HandleEvents(c *fiber.Ctx) error {
	// 设置SSE响应头
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	uid := c.Locals("_uid").(string)
	deviceId := c.Locals("_device_id").(string)
	deviceName := c.Locals("_device_name").(string)
	lastEventId := int64(0)
	if v := c.Locals("_last_event_id"); v != nil {
		lastEventId = int64(v.(int))
	}
	address := getRealIP(c)

	existDevice := globalInstance.getDevice(deviceId)
	if existDevice == nil {
		existDevice = getRedisDevice(deviceId)
	}
	if existDevice != nil {
		if existDevice.UID != uid {
			existDevice.delFrameCache()
		}
		slog.Info("Current instance and device", "instance", globalInstance.Address, "device", existDevice)
		if existDevice.isRemote() {
			DispatchInstruction(existDevice.InstanceAddress, Instruction{
				DeviceID: deviceId,
				Command:  CMD_EXTRUDE_OFFLINE,
				Data:     address,
				Event:    "",
			})
		} else {
			globalInstance.handleInstruction(&Instruction{
				DeviceID: deviceId,
				Command:  CMD_EXTRUDE_OFFLINE,
				Data:     address,
				Event:    "",
			})
		}
	}

	device := NewDevice(deviceId, deviceName, uid, globalInstance.Address, address)
	device.online()
	user := NewUser(uid)
	user.handleDeviceOnline(device)

	// Fiber没有Flusher接口，直接写入响应体并Flush
	// 发送连接成功事件
	if _, err := c.WriteString(fmt.Sprintf("event: %s\ndata: %s\n\n", EVT_SYS_CONNECTED, address)); err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "Streaming unsupported!"})
	}

	// 发送缓存的消息帧
	if lastEventId > 0 {
		frames := device.getCachedFrames(lastEventId)
		for _, frame := range frames {
			if frame.Event == "" {
				c.WriteString(fmt.Sprintf("id: %d\ndata: %s\n\n", frame.ID, frame.Data))
			} else {
				c.WriteString(fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", frame.ID, frame.Event, frame.Data))
			}
		}
		if len(frames) > 0 {
			slog.Info("Send cached frames to device", "count", len(frames), "device", deviceId)
		}
	}

	ticker := time.NewTicker(globalConfig.SSE.HeartbeatDuration)
	defer ticker.Stop()

	channel := make(chan *Instruction)

	deviceChannels.Store(deviceId, channel)
	deviceChannelWG.Add(1)

	for {
		select {
		case instraction := <-channel:
			if instraction.Command == CMD_SEND_FRAME {
				frame := device.addFrame(instraction.Event, instraction.Data)
				if frame.Event == "" {
					c.WriteString(fmt.Sprintf("id: %d\ndata: %s\n\n", frame.ID, frame.Data))
				} else {
					c.WriteString(fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", frame.ID, frame.Event, frame.Data))
				}
			} else if instraction.Command == CMD_KICK_OFFLINE {
				device.offline(DCR_KICK_OFFLINE, instraction.Data)
				user.handleDeviceOffline(device)
				c.WriteString(fmt.Sprintf("event: %s\ndata: %s\n\n", EVT_SYS_KICK_OFFLINE, instraction.Data))
				deviceChannelWG.Done()
				return nil
			} else if instraction.Command == CMD_EXTRUDE_OFFLINE {
				device.offline(DCR_EXTRUDE_OFFLINE, instraction.Data)
				user.handleDeviceOffline(device)
				c.WriteString(fmt.Sprintf("event: %s\ndata: %s\n\n", EVT_SYS_EXTRUDE_OFFLINE, instraction.Data))
				deviceChannelWG.Done()
				return nil
			} else if instraction.Command == CMD_INSTANCE_CLOSE {
				device.offline(DCR_INSTANCE_CLOSE, instraction.Data)
				user.handleDeviceOffline(device)
				c.WriteString(fmt.Sprintf("event: %s\ndata: %s\n\n", EVT_SYS_INSTANCE_CLOSE, instraction.Data))
				deviceChannelWG.Done()
				return nil
			} else {
				slog.Error("Unknown instruction", "instruction", instraction)
			}
		case <-ticker.C:
			_, err := c.WriteString(fmt.Sprintf("%s\n\n", PAYLOAD_HEARTBEAT))
			if err != nil {
				device.offline(DCR_HEARTBEAT_FAIL, "")
				user.handleDeviceOffline(device)
				deviceChannelWG.Done()
				return nil
			}
			device.touch()
			user.touch()
		case <-c.Context().Done():
			slog.Info("Client disconnected", "client", address, "device", deviceName)
			device.offline(DCR_DEVICE_DISCONNECT, "")
			user.handleDeviceOffline(device)
			deviceChannelWG.Done()
			return nil
		}
	}
}
