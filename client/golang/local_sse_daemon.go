package local

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type SSEDaemon struct {
	sseClient              *SSEClient           // SSE 客户端实例
	maxRetryCount          int                  // 最大重试次数
	retryInterval          time.Duration        // 连续失败的重试间隔时间
	businessConnected      SSEConnectedHandler  // 业务连接成功回调
	businessDisconnected   SSEDisConnectHandler // 业务连接断开回调
	businessKickOffline    SSEOfflineHandler    // 业务被踢下线回调
	businessExtrudeOffline SSEOfflineHandler    // 业务被挤下线回调

	// ====== 内部状态管理 ======
	mu                sync.Mutex         // 保护状态字段的互斥锁
	running           bool               // 守护进程是否运行中
	reconnecting      bool               // 是否处于重连流程中
	hasEverConnected  bool               // 是否曾经成功连接过一次
	currentRetryCount int                // 当前连续失败的重试计数
	stopReason        string             // 停止原因
	stopErr           error              // 停止关联错误
	disconnectCalled  bool               // 业务断开回调是否已触发
	ctx               context.Context    // 守护进程的 context，用于优雅退出
	cancel            context.CancelFunc // 守护进程的 cancel 函数
	wg                sync.WaitGroup     // 等待重连 goroutine 完成
}

func NewSSEDaemon(options *SSEOptions, maxRetryCount int, retryInterval time.Duration) (*SSEDaemon, error) {
	daemon := &SSEDaemon{
		maxRetryCount: maxRetryCount,
		retryInterval: retryInterval,
	}
	daemon.businessConnected = options.OnConnected
	daemon.businessDisconnected = options.OnDisconnect
	daemon.businessKickOffline = options.OnKickOffline
	daemon.businessExtrudeOffline = options.OnExtrudeOffline

	// 使用 daemon 的回调包装原始回调
	options.OnConnected = daemon.daemonOnConnected
	options.OnDisconnect = daemon.daemonOnDisconnected
	options.OnKickOffline = daemon.daemonOnKickOffline
	options.OnExtrudeOffline = daemon.daemonOnExtrudeOffline

	client, err := NewSSEClient(options)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSE client: %w", err)
	}
	daemon.sseClient = client
	return daemon, nil
}

func (d *SSEDaemon) daemonOnConnected() {
	d.mu.Lock()
	isFirstConnected := !d.hasEverConnected
	// 标记已成功连接过一次
	d.hasEverConnected = true
	// 连接成功后结束重连状态
	d.reconnecting = false
	// 重置重试计数（连接成功后清零）
	d.currentRetryCount = 0
	d.mu.Unlock()

	slog.Info("SSE daemon connected successfully, retry count reset")

	// 仅首次连接成功时触发业务回调，重连成功不触发
	if isFirstConnected && d.businessConnected != nil {
		d.businessConnected()
	}
}

func (d *SSEDaemon) daemonOnDisconnected(reason string, err error) {
	d.mu.Lock()
	running := d.running
	hasEverConnected := d.hasEverConnected
	retryCount := d.currentRetryCount
	d.mu.Unlock()

	slog.Info("SSE daemon disconnected", "reason", reason, "has_ever_connected", hasEverConnected, "retry_count", retryCount)

	// 如果守护进程已停止，不做任何处理
	if !running {
		slog.Debug("Daemon is stopped, skip reconnection logic")
		return
	}

	// 检查是否是系统事件导致的断开（这些事件应该停止守护进程，不再重连）
	if strings.HasPrefix(reason, "kick offline:") ||
		strings.HasPrefix(reason, "extrude offline:") ||
		strings.HasPrefix(reason, "instance close:") {
		slog.Info("Connection disconnected due to system event, stopping daemon", "reason", reason)
		if stopErr := d.stopWithReason(reason, err); stopErr != nil {
			slog.Error("Failed to stop daemon on system event", "error", stopErr)
		}
		return
	}

	// 决定是否需要重连
	if hasEverConnected {
		// 曾经连接成功过，现在断开了（非系统事件），立即触发重连
		slog.Info("Connection was established before and disconnected abnormally, triggering reconnection")
		d.triggerReconnect()
	} else {
		// 首次连接失败，检查重试次数
		d.mu.Lock()
		d.currentRetryCount++
		newRetryCount := d.currentRetryCount
		d.mu.Unlock()

		if newRetryCount >= d.maxRetryCount {
			// 超过重试次数，停止守护进程
			slog.Error("Exceeded max retry count", "retry_count", newRetryCount, "max_count", d.maxRetryCount)
			if stopErr := d.stopWithReason(reason, err); stopErr != nil {
				slog.Error("Failed to stop daemon after exceeding retry count", "error", stopErr)
			}
		} else {
			// 还有重试次数，继续重连
			slog.Info("Initial connection failed, will retry", "retry_count", newRetryCount, "max_count", d.maxRetryCount)
			d.triggerReconnect()
		}
	}
}

// triggerReconnect 触发重连逻辑（延迟重连）
func (d *SSEDaemon) triggerReconnect() {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		slog.Debug("Daemon is stopped, skip scheduling reconnection")
		return
	}
	d.reconnecting = true
	d.mu.Unlock()

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()

		// 等待重试间隔时间
		select {
		case <-d.ctx.Done():
			d.mu.Lock()
			d.reconnecting = false
			d.mu.Unlock()
			slog.Debug("Reconnection cancelled due to context done")
			return
		case <-time.After(d.retryInterval):
		}

		// 再次检查是否还在运行
		d.mu.Lock()
		if !d.running {
			d.reconnecting = false
			d.mu.Unlock()
			slog.Debug("Daemon is stopped, skip reconnection")
			return
		}
		d.mu.Unlock()

		slog.Info("Attempting to reconnect")
		err := d.sseClient.Connect()
		if err != nil {
			slog.Error("Reconnection failed", "error", err)
		}
	}()
}

func (d *SSEDaemon) daemonOnKickOffline(reason string) {
	// 被踢下线，调用业务回调
	if d.businessKickOffline != nil {
		d.businessKickOffline(reason)
	}
	// 不在这里调用 Stop，因为此时我们还在 run goroutine 中
	// closeInternal(false) 会被调用，然后 daemonOnDisconnected 会处理重连逻辑
	// daemonOnDisconnected 会识别这是系统事件，自动阻止重连
	slog.Info("SSE daemon received kick offline event", "reason", reason)
}

func (d *SSEDaemon) daemonOnExtrudeOffline(reason string) {
	// 被挤下线，调用业务回调
	if d.businessExtrudeOffline != nil {
		d.businessExtrudeOffline(reason)
	}
	// 不在这里调用 Stop，因为此时我们还在 run goroutine 中
	// closeInternal(false) 会被调用，然后 daemonOnDisconnected 会处理重连逻辑
	// daemonOnDisconnected 会识别这是系统事件，自动阻止重连
	slog.Info("SSE daemon received extrude offline event", "reason", reason)
}

func (d *SSEDaemon) Start() error {
	d.mu.Lock()
	// 检查是否已在运行
	if d.running {
		d.mu.Unlock()
		return nil
	}

	// 初始化状态
	d.running = true
	d.reconnecting = false
	d.hasEverConnected = false
	d.currentRetryCount = 0
	d.stopReason = ""
	d.stopErr = nil
	d.disconnectCalled = false
	d.ctx, d.cancel = context.WithCancel(context.Background())
	d.mu.Unlock()

	slog.Info("SSE daemon starting", "max_retry_count", d.maxRetryCount, "retry_interval", d.retryInterval)

	// 首次连接
	err := d.sseClient.Connect()
	if err != nil {
		slog.Error("Initial connection failed", "error", err)
		// 首次连接失败后的重试/停止逻辑由 daemonOnDisconnected 统一处理
		// 这里直接返回，避免与回调路径产生重复计数或重复调度。
		return nil
	}

	slog.Info("SSE daemon started successfully")
	return nil
}

func (d *SSEDaemon) Stop() error {
	return d.stopWithReason("daemon stopped", nil)
}

func (d *SSEDaemon) stopWithReason(reason string, err error) error {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return nil
	}

	if reason == "" {
		reason = "daemon stopped"
	}
	if d.stopReason == "" {
		d.stopReason = reason
	}
	if d.stopErr == nil {
		d.stopErr = err
	}

	d.running = false
	d.reconnecting = false

	callback := d.businessDisconnected
	callbackReason := d.stopReason
	callbackErr := d.stopErr
	shouldCallDisconnect := callback != nil && !d.disconnectCalled
	if shouldCallDisconnect {
		d.disconnectCalled = true
	}
	cancel := d.cancel
	d.mu.Unlock()

	slog.Info("SSE daemon stopping", "reason", callbackReason)

	// 取消 context，停止所有重连 goroutine
	cancel()

	// 关闭 SSE 客户端
	d.sseClient.Close()

	// 等待所有重连 goroutine 完成（最多等待 2 秒）
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("SSE daemon stopped successfully")
	case <-time.After(2 * time.Second):
		slog.Warn("SSE daemon stop timeout waiting for goroutines")
	}

	if shouldCallDisconnect {
		callback(callbackReason, callbackErr)
	}

	return nil
}
