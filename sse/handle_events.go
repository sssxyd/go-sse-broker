package sse

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
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

	// 注册设备上线
	device := NewDevice(deviceId, deviceName, uid, globalInstance.Address, address)
	device.online()
	user := NewUser(uid)
	user.handleDeviceOnline(device)

	// 创建设备消息通道
	channel := make(chan *Instruction)
	deviceChannels.Store(deviceId, channel)
	deviceChannelWG.Add(1)

	// 暂存上下文，避免在 goroutine 中引用 c 导致并发问题
	ctx := c.Context()
	// 使用SetBodyStreamWriter保持连接不断开, 流式写入
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ticker := time.NewTicker(globalConfig.SSE.HeartbeatDuration)
		defer ticker.Stop()

		// 发送连接成功事件
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", EVT_SYS_CONNECTED, address)
		_ = w.Flush()

		// 发送缓存的消息帧
		if lastEventId > 0 {
			frames := device.getCachedFrames(lastEventId)
			for _, frame := range frames {
				if frame.Event == "" {
					fmt.Fprintf(w, "id: %d\ndata: %s\n\n", frame.ID, frame.Data)
				} else {
					fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", frame.ID, frame.Event, frame.Data)
				}
			}
			if len(frames) > 0 {
				_ = w.Flush()
				slog.Info("Send cached frames to device", "count", len(frames), "device", deviceId)
			}
		}

		for {
			select {
			case instraction := <-channel: // 收到指令
				switch instraction.Command {
				case CMD_SEND_FRAME: // 发送消息帧
					frame := device.addFrame(instraction.Event, instraction.Data)
					if frame.Event == "" {
						fmt.Fprintf(w, "id: %d\ndata: %s\n\n", frame.ID, frame.Data)
					} else {
						fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", frame.ID, frame.Event, frame.Data)
					}
					_ = w.Flush()
				case CMD_KICK_OFFLINE: // 踢下线
					device.offline(DCR_KICK_OFFLINE, instraction.Data)
					user.handleDeviceOffline(device)
					fmt.Fprintf(w, "event: %s\ndata: %s\n\n", EVT_SYS_KICK_OFFLINE, instraction.Data)
					_ = w.Flush()
					deviceChannelWG.Done()
					return
				case CMD_EXTRUDE_OFFLINE: // 相同设备登录挤下线
					device.offline(DCR_EXTRUDE_OFFLINE, instraction.Data)
					user.handleDeviceOffline(device)
					fmt.Fprintf(w, "event: %s\ndata: %s\n\n", EVT_SYS_EXTRUDE_OFFLINE, instraction.Data)
					_ = w.Flush()
					deviceChannelWG.Done()
					return
				case CMD_INSTANCE_CLOSE: // 服务实例关闭连接
					device.offline(DCR_INSTANCE_CLOSE, instraction.Data)
					user.handleDeviceOffline(device)
					fmt.Fprintf(w, "event: %s\ndata: %s\n\n", EVT_SYS_INSTANCE_CLOSE, instraction.Data)
					_ = w.Flush()
					deviceChannelWG.Done()
					return
				default:
					slog.Error("Unknown instruction", "instruction", instraction)
				}
			case <-ticker.C: // 心跳检测
				_, err := fmt.Fprintf(w, "%s\n\n", PAYLOAD_HEARTBEAT)
				if err != nil {
					device.offline(DCR_HEARTBEAT_FAIL, "")
					user.handleDeviceOffline(device)
					deviceChannelWG.Done()
					slog.Error("write heartbeat failed", "error", err)
					return
				} else {
					_ = w.Flush()
					device.touch()
					user.touch()
				}
			case <-ctx.Done(): // 客户端断开连接
				slog.Info("Client disconnected", "client", address, "device", deviceName)
				device.offline(DCR_DEVICE_DISCONNECT, "")
				user.handleDeviceOffline(device)
				deviceChannelWG.Done()
				return
			}
		}
	})
	// 重要：处理函数本身返回 nil，让 Fiber 持续保持连接，由 StreamWriter 驱动输出
	return nil
}
