package sse

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func HandleEvents(c *gin.Context) {
	// 设置SSE响应头
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	uid := c.GetString("_uid")
	deviceId := c.GetString("_device_id")
	deviceName := c.GetString("_device_name")
	lastEventId := c.GetInt64("_last_event_id")
	address := c.Request.RemoteAddr

	// 将本设备登录的其他连接挤下线
	existDevice := globalInstance.getDevice(deviceId)
	if existDevice != nil {
		// 同一设备上新老用户ID不一致时，删除老用户的帧缓存
		if existDevice.UID != uid {
			existDevice.delFrameCache()
		}
		if existDevice.isRemote() {
			DispachInstruction(existDevice.InstanceAddress, Instruction{
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

	// 获取Flusher
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Streaming unsupported!"})
		return
	}

	// 发送连接成功事件
	fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", EVT_SYS_CONNECTED, address)
	flusher.Flush()

	// 发送缓存的消息帧
	if lastEventId > 0 {
		frames := device.getCachedFrames(lastEventId)
		for _, frame := range frames {
			if frame.Event == "" {
				fmt.Fprintf(c.Writer, "id: %d\ndata: %s\n\n", frame.ID, frame.Data)
			} else {
				fmt.Fprintf(c.Writer, "id: %d\nevent: %s\ndata: %s\n\n", frame.ID, frame.Event, frame.Data)
			}
		}
		if len(frames) > 0 {
			log.Printf("Send %d cached frames to device %s\n", len(frames), deviceId)
			flusher.Flush()
		}
	}

	// 创建一个 ticker，每一个心跳周期触发一次
	ticker := time.NewTicker(globalConfig.SSE.HeartbeatDuration)
	defer ticker.Stop()

	channel := make(chan *Instruction)
	deviceChannels.Store(deviceId, channel)

	for {
		select {
		case instraction := <-channel:
			// log.Printf("Receive instruction: %v\n", instraction)
			if instraction.Command == CMD_SEND_FRAME {
				frame := device.addFrame(instraction.Event, instraction.Data)
				if frame.Event == "" {
					fmt.Fprintf(c.Writer, "id: %d\ndata: %s\n\n", frame.ID, frame.Data)
				} else {
					fmt.Fprintf(c.Writer, "id: %d\nevent: %s\ndata: %s\n\n", frame.ID, frame.Event, frame.Data)
				}
				flusher.Flush()
			} else if instraction.Command == CMD_KICK_OFFLINE {
				device.offline(DCR_KICK_OFFLINE, instraction.Data)
				user.handleDeviceOffline(device)
				fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", EVT_SYS_KICK_OFFLINE, instraction.Data)
				return
			} else if instraction.Command == CMD_EXTRUDE_OFFLINE {
				device.offline(DCR_EXTRUDE_OFFLINE, instraction.Data)
				user.handleDeviceOffline(device)
				fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", EVT_SYS_EXTRUDE_OFFLINE, instraction.Data)
				return
			} else if instraction.Command == CMD_INSTANCE_CLOSE {
				device.offline(DCR_INSTANCE_CLOSE, instraction.Data)
				user.handleDeviceOffline(device)
				fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", EVT_SYS_INSTANCE_CLOSE, instraction.Data)
				return
			} else {
				log.Printf("Unknown instruction: %v\n", instraction)
			}
		case <-ticker.C:
			// 发送心跳
			// log.Printf("Send heartbeat to device %s\n", deviceId)
			_, err := fmt.Fprintf(c.Writer, "%s\n\n", PAYLOAD_HEARTBEAT)
			flusher.Flush()
			if err != nil {
				device.offline(DCR_HEARTBEAT_FAIL, "")
				user.handleDeviceOffline(device)
				return
			}
			device.touch()
			user.touch()
		case <-c.Writer.CloseNotify():
			log.Printf("Client %s on Device %s is offline\n", address, deviceName)
			device.offline(DCR_DISCONNECT, "")
			user.handleDeviceOffline(device)
			return
		}
	}
}
