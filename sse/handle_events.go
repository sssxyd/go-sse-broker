package sse

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sse-broker/cfg"
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
	// 配置 TCP KeepAlive，更快检测连接断开（兜底保障）
	if conn := c.Context().Conn(); conn != nil {
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			// 启用 TCP KeepAlive（30秒探测间隔，作为应用层检测的兜底）
			if err := tcpConn.SetKeepAlive(true); err != nil {
				slog.Warn("Failed to set TCP KeepAlive", "error", err)
			}
			if err := tcpConn.SetKeepAlivePeriod(30 * time.Second); err != nil {
				slog.Warn("Failed to set TCP KeepAlive period", "error", err)
			}
		}
	}

	// 设置SSE响应头
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	// 安全地获取 Locals 值（防止 panic）
	uid, _ := c.Locals("_uid").(string)
	deviceId, _ := c.Locals("_device_id").(string)
	deviceName, _ := c.Locals("_device_name").(string)

	if uid == "" || deviceId == "" || deviceName == "" {
		slog.Error("Missing required authentication data")
		return fiber.NewError(fiber.StatusUnauthorized, "Missing authentication data")
	}

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

	// 创建设备消息通道（带缓冲，防止发送端阻塞）
	channel := make(chan *Instruction, 10)
	deviceChannels.Store(deviceId, channel)
	deviceChannelWG.Add(1)

	// 暂存上下文，避免在 goroutine 中引用 c 导致并发问题
	ctx := c.Context()

	// 启动后台 goroutine 检测客户端断开 (FIN/RST/网络中断)
	disconnectChan := make(chan string, 1) // ✅ 使用缓冲 channel，防止 goroutine 泄漏
	if conn := ctx.Conn(); conn != nil {
		go func() {
			defer close(disconnectChan)

			// 持续尝试读取，检测连接断开
			buf := make([]byte, 1)
			unexpectedDataCount := 0 // 记录异常数据次数

			for {
				// SetReadDeadline 防止永久阻塞
				// 每 5 分钟超时一次（降低系统调用频率）
				if err := conn.SetReadDeadline(time.Now().Add(5 * time.Minute)); err != nil {
					slog.Warn("Failed to set read deadline", "device", deviceId, "error", err)
					// 设置失败可能意味着连接已经有问题，尝试继续
				}
				_, err := conn.Read(buf)

				if err != nil {
					// 判断断开类型
					var reason string
					errStr := err.Error()

					if err == io.EOF {
						reason = "FIN (graceful close)"
					} else if strings.Contains(errStr, "reset") || strings.Contains(errStr, "RST") {
						reason = "RST (connection reset)"
					} else if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline") {
						// 超时是正常的（因为我们设置了 30 秒 ReadDeadline）
						// 继续循环，重新设置 deadline
						continue
					} else if strings.Contains(errStr, "broken pipe") {
						reason = "network error (broken pipe)"
					} else if strings.Contains(errStr, "use of closed") {
						reason = "connection closed"
					} else {
						reason = fmt.Sprintf("unknown (%v)", err)
					}

					slog.Debug("Client connection terminated",
						"device", deviceId,
						"reason", reason,
						"error", err)

					disconnectChan <- reason
					return
				}

				// 如果客户端意外发送了数据（SSE 不应该有上行数据）
				unexpectedDataCount++
				slog.Warn("Unexpected data from SSE client, ignoring",
					"device", deviceId,
					"count", unexpectedDataCount)

				// 防止恶意客户端持续发送数据导致 CPU 占用
				if unexpectedDataCount > 10 {
					slog.Error("Client sending too much unexpected data, closing connection",
						"device", deviceId,
						"count", unexpectedDataCount)
					disconnectChan <- "protocol violation (unexpected upstream data)"
					return
				}

				// 短暂延迟，避免紧密循环
				time.Sleep(100 * time.Millisecond)
			}
		}()
	}

	// 使用SetBodyStreamWriter保持连接不断开, 流式写入
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ticker := time.NewTicker(cfg.GlobalConfig().GetHeartbeatIntervalDuration())
		defer ticker.Stop()

		// 发送连接成功事件
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", EVT_SYS_CONNECTED, address); err != nil {
			slog.Warn("Failed to send connected event", "device", deviceId, "error", err)
			return
		}
		_ = w.Flush()

		// 发送缓存的消息帧
		if lastEventId > 0 {
			frames := device.getCachedFrames(lastEventId)
			for _, frame := range frames {
				var err error
				if frame.Event == "" {
					_, err = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", frame.ID, frame.Data)
				} else {
					_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", frame.ID, frame.Event, frame.Data)
				}
				if err != nil {
					slog.Warn("Failed to send cached frame", "device", deviceId, "frame_id", frame.ID, "error", err)
					return
				}
			}
			if len(frames) > 0 {
				if err := w.Flush(); err != nil {
					slog.Warn("Failed to flush cached frames", "device", deviceId, "error", err)
					return
				}
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
			case <-ticker.C: // 心跳检测（同时作为连接检测）
				_, err := fmt.Fprintf(w, "%s\n\n", PAYLOAD_HEARTBEAT)
				if err != nil {
					device.offline(DCR_HEARTBEAT_FAIL, "heartbeat write failed")
					user.handleDeviceOffline(device)
					deviceChannelWG.Done()
					slog.Warn("Heartbeat write failed, client disconnected",
						"device", deviceId,
						"error", err)
					return
				} else {
					_ = w.Flush()
					device.touch()
					user.touch()
				}
			case reason := <-disconnectChan: // 检测到客户端断开 (FIN/RST/网络异常)
				slog.Info("Client connection terminated",
					"device", deviceId,
					"client", address,
					"reason", reason)
				device.offline(DCR_DEVICE_DISCONNECT, reason)
				user.handleDeviceOffline(device)
				deviceChannelWG.Done()
				return
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
