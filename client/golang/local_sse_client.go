package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type sse_sys_event string

var (
	EVT_SYS_CONNECTED       sse_sys_event = "sys_connected"
	EVT_SYS_KICK_OFFLINE    sse_sys_event = "sys_kick_offline"
	EVT_SYS_EXTRUDE_OFFLINE sse_sys_event = "sys_extrude_offline"
	EVT_SYS_INSTANCE_CLOSE  sse_sys_event = "sys_instance_close"
)

// SSEEvent 表示 SSE 事件
type sse_message struct {
	ID    string `db:"id" json:"id"`
	Event string `db:"event" json:"event"`
	Data  string `db:"data" json:"data"`
	// Retry int    `db:"retry" json:"retry"`
}

// 分类错误
var (
	ErrNoContent = errors.New("sse: server returned 204 no content")
	ErrGone      = errors.New("sse: server returned 410 gone")
)

const (
	defaultInactivityThreshold = 65 * time.Second
	defaultDialTimeout         = 10 * time.Second
	defaultKeepAlive           = 30 * time.Second

	scannerBufferSize      = 64 * 1024
	scannerMaxTokenSize    = 1 * 1024 * 1024
	heartbeatCheckInterval = 5 * time.Second
)

// SSEClient SSE 客户端
type SSEClient struct {
	options      SSEOptions
	activityUnix atomic.Int64
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.RWMutex
	running      bool
	wg           sync.WaitGroup
	closeReason  string // 记录关闭原因

	// 改为保存 connectionCloser
	currentClose atomic.Pointer[connectionCloser]
}

// NewSSEOptions 创建带默认值的 SSE 选项
func NewSSEOptions(url, device string, getToken SSEGetTokenHandler) SSEOptions {
	return SSEOptions{
		SSEEventsURL:   url,
		ClientDeviceID: device,
		GetAccessToken: getToken,
	}
}

func normalizeSSEOptions(options *SSEOptions) {
	if options.InactivityThreshold <= 0 {
		options.InactivityThreshold = defaultInactivityThreshold
	}
	if options.DialTimeout <= 0 {
		options.DialTimeout = defaultDialTimeout
	}
	if options.KeepAlive <= 0 {
		options.KeepAlive = defaultKeepAlive
	}
	if options.HeartbeatInterval <= 0 {
		options.HeartbeatInterval = heartbeatCheckInterval
	}
	if options.ScannerBufferSize <= 0 {
		options.ScannerBufferSize = scannerBufferSize
	}
	if options.ScannerMaxTokenSize <= 0 {
		options.ScannerMaxTokenSize = scannerMaxTokenSize
	}
	if options.OnConnected == nil {
		options.OnConnected = func() {
			slog.Debug("SSE connected")
		}
	}
	if options.OnDisconnect == nil {
		options.OnDisconnect = func(reason string, err error) {
			if err != nil {
				slog.Error("SSE disconnected", "reason", reason, "error", err)
			} else {
				slog.Info("SSE disconnected", "reason", reason)
			}
		}
	}
	if options.OnKickOffline == nil {
		options.OnKickOffline = func(data string) {
			slog.Warn("SSE kick offline", "data", data)
		}
	}
	if options.OnExtrudeOffline == nil {
		options.OnExtrudeOffline = func(data string) {
			slog.Warn("SSE extrude offline", "data", data)
		}
	}
	if options.OnMessage == nil {
		options.OnMessage = func(data string) {
			slog.Debug("SSE message received", "data", data)
		}
	}
	if options.OnEvent == nil {
		options.OnEvent = func(event, data string) {
			slog.Debug("SSE event received", "event", event, "data", data)
		}
	}
}

// NewSSEClient 创建新的 SSE 客户端
func NewSSEClient(options *SSEOptions) (*SSEClient, error) {
	normalizeSSEOptions(options)

	if options.GetAccessToken == nil {
		return nil, fmt.Errorf("GetAccessToken handler cannot be nil")
	}
	if options.SSEEventsURL == "" {
		return nil, fmt.Errorf("SSEEventsURL cannot be empty")
	}
	if options.ClientDeviceID == "" {
		return nil, fmt.Errorf("ClientDeviceID cannot be empty")
	}

	ctx, cancel := context.WithCancel(context.Background())

	sc := &SSEClient{
		options: *options,
		ctx:     ctx,
		cancel:  cancel,
	}
	sc.activityUnix.Store(time.Now().UnixNano())

	return sc, nil
}

func (c *SSEClient) mark_activity() {
	c.activityUnix.Store(time.Now().UnixNano())
}

func (c *SSEClient) since_last_activity() time.Duration {
	t := c.activityUnix.Load()
	if t == 0 {
		// 返回 0 表示刚刚活动
		return 0
	}
	return time.Since(time.Unix(0, t)) // t 为纳秒放在第二参数
}

// Connect 连接到 SSE 服务器
func (c *SSEClient) Connect() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}

	// 如果之前 ctx 已取消，重建
	select {
	case <-c.ctx.Done():
		c.ctx, c.cancel = context.WithCancel(context.Background())
	default:
	}

	c.running = true
	c.wg.Add(1) // 添加到 WaitGroup
	c.mu.Unlock()

	started := make(chan error, 1)
	go c.run(started)

	return <-started
}

// Close 外部调用的关闭方法
func (c *SSEClient) Close() {
	c.closeInternal(true) // 等待退出
}

// closeInternal 内部关闭方法（优化版）
func (c *SSEClient) closeInternal(wait bool) {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.mu.Unlock()

	slog.Debug("Closing SSE client", "wait", wait)

	// 1. 先强制关闭底层连接
	if closer := c.currentClose.Load(); closer != nil {
		closer.Close()
	}

	// 2. 取消 context
	c.cancel()

	// 3. 等待退出(可选)
	if wait {
		done := make(chan struct{})
		go func() {
			c.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			slog.Info("SSE client closed successfully")
		case <-time.After(2 * time.Second): // 增加到 2 秒
			slog.Warn("Close timeout after 2s, forcing exit")
		}
	}
}

// IsConnected 检查是否已连接
func (c *SSEClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

// run 主运行循环
func (c *SSEClient) run(started chan<- error) {
	// ====== 保存初始连接状态 ======
	// 用途：在 defer 执行时，判断是否需要调用 OnDisconnect 回调
	//
	// 为什么需要保存？
	// 当系统事件（如 kick_offline、extrude_offline、instance_close）触发时，
	// 会调用 closeInternal(false)，这会立即设置 running = false。
	// 如果在 defer 中直接检查 running，会因为已被修改而无法准确判断连接是否曾经建立过。
	//
	// 三种场景的 OnDisconnect 处理：
	// 1. 首次连接失败：在 connectToServer 后直接调用 OnDisconnect(reason, err)，不走 defer
	// 2. 系统事件关闭：wasConnected=true，defer 通过保存的状态调用 OnDisconnect
	// 3. 外部 Close()：wasConnected=true，defer 通过保存的状态调用 OnDisconnect
	c.mu.Lock()
	wasConnected := c.running
	c.mu.Unlock()

	defer func() {
		c.wg.Done()

		c.mu.Lock()
		c.running = false
		reason := c.closeReason
		c.mu.Unlock()

		// 只有当初始时连接已建立时，才在 defer 中调用 OnDisconnect
		// 这保证了所有断开连接的路径都能准确地调用 OnDisconnect 回调，
		// 同时避免了首次连接失败时的重复调用
		if wasConnected {
			if reason == "" {
				reason = "closed by client"
			}
			c.options.OnDisconnect(reason, nil)
		}
	}()

	sendStarted := func(err error) {
		if started == nil {
			return
		}
		started <- err
		started = nil
	}

	select {
	case <-c.ctx.Done():
		slog.Info("SSE client context cancelled, stopping")
		sendStarted(context.Canceled)
		return
	default:
	}

	err := c.connectToServer(func() {
		sendStarted(nil)
	})
	if err != nil {
		sendStarted(err)
		slog.Error("SSE connection failed, exiting", "error", err)
		c.options.OnDisconnect("connection failed", err)
		return
	}

	sendStarted(nil)

	slog.Info("SSE connection closed, exiting")
}

// 关键优化：使用可取消的 HTTP 请求 context
func (c *SSEClient) connectToServer(onConnected func()) error {
	// 每次连接创建独立资源
	var currentConn net.Conn
	var connMu sync.Mutex
	var transport *http.Transport
	var connClosed atomic.Bool

	// 创建可独立取消的请求 context（这是关键！）
	reqCtx, reqCancel := context.WithCancel(c.ctx)
	defer reqCancel()

	dialer := &net.Dialer{
		Timeout:   c.options.DialTimeout,
		KeepAlive: c.options.KeepAlive,
	}

	transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}

			// 显式启用 TCP Keep-Alive（必须！）
			if tcpConn, ok := conn.(*net.TCPConn); ok {
				_ = tcpConn.SetKeepAlive(true)
				_ = tcpConn.SetKeepAlivePeriod(30 * time.Second)
			}

			// 包装连接，支持强制关闭
			wrappedConn := &forceCloseConn{Conn: conn}

			connMu.Lock()
			currentConn = wrappedConn
			connMu.Unlock()

			return wrappedConn, nil
		},
		ForceAttemptHTTP2:      false,
		MaxIdleConns:           0,
		MaxIdleConnsPerHost:    0,
		MaxConnsPerHost:        1,
		IdleConnTimeout:        1 * time.Second,
		TLSHandshakeTimeout:    10 * time.Second,
		ExpectContinueTimeout:  1 * time.Second,
		ResponseHeaderTimeout:  15 * time.Second,
		MaxResponseHeaderBytes: 1 << 20,
		DisableCompression:     true,
		DisableKeepAlives:      false, // 改为 false（允许 HTTP Keep-Alive）
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   0, // 不设置整体超时，使用 context 控制
	}

	// 读取配置
	token := c.options.GetAccessToken()
	device := c.options.ClientDeviceID
	lastEventId := c.options.GetLastEventID()
	eventsUrl := c.options.SSEEventsURL

	if token == "" {
		return fmt.Errorf("sse token is empty")
	}
	if device == "" {
		return fmt.Errorf("client device ID is empty")
	}

	u, err := url.Parse(eventsUrl)
	if err != nil {
		return fmt.Errorf("invalid URL: %s, %w", eventsUrl, err)
	}

	// 使用可取消的 context 创建请求
	req, err := http.NewRequestWithContext(reqCtx, "GET", u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive") // 改为 keep-alive
	req.Header.Set("X-SSE-Token", token)
	req.Header.Set("X-SSE-Device", device)
	if lastEventId != "" {
		req.Header.Set("X-SSE-ID", lastEventId)
	}

	slog.Info("SSE connecting", "url", u.String(), "last_event_id", lastEventId)

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	// 改进的关闭器 - 添加优雅关闭逻辑
	closer := newConnectionCloser(func() {
		if !connClosed.CompareAndSwap(false, true) {
			return
		}

		slog.Debug("Closing connection resources")

		// 第零步: 尝试通知服务端连接即将关闭
		if currentConn != nil {
			// 发送一个空的 HTTP GET 到关闭端点 (如果服务端支持)
			// 或者发送 TCP FIN (通过半关闭)
			connMu.Lock()
			if tcpConn, ok := currentConn.(*net.TCPConn); ok {
				// 先半关闭写端，让服务端知道客户端不再发送数据
				_ = tcpConn.CloseWrite()
				time.Sleep(100 * time.Millisecond) // 等待服务端响应
			}
			connMu.Unlock()
		}

		// 第一步: 强制关闭底层 TCP 连接
		connMu.Lock()
		if currentConn != nil {
			// 立即设置过期的 deadline
			past := time.Now().Add(-time.Hour)
			_ = currentConn.SetDeadline(past)
			_ = currentConn.SetReadDeadline(past)
			_ = currentConn.SetWriteDeadline(past)

			// 改进：使用正常关闭而非 RST
			if tcpConn, ok := currentConn.(*net.TCPConn); ok {
				// 使用正常关闭流程 (FIN)
				_ = tcpConn.CloseRead() // 关闭读端
				// CloseWrite 已在上面调用
			}

			// 强制关闭
			if fc, ok := currentConn.(*forceCloseConn); ok {
				fc.ForceClose()
			} else {
				_ = currentConn.Close()
			}
			currentConn = nil
		}
		connMu.Unlock()

		// 第二步: 取消请求 context
		reqCancel()

		// 第三步: 关闭 HTTP Response Body
		_ = resp.Body.Close()

		// 第四步: 清理 Transport
		if transport != nil {
			transport.CloseIdleConnections()
		}
	})

	// 保存关闭器
	c.currentClose.Store(closer)

	// defer 使用 closer.Close()
	defer func() {
		closer.Close()
		c.currentClose.CompareAndSwap(closer, nil)
	}()

	// 检查响应状态
	switch resp.StatusCode {
	case http.StatusNoContent:
		return ErrNoContent
	case http.StatusGone:
		return ErrGone
	case http.StatusUnauthorized:
		return fmt.Errorf("unauthorized: invalid token")
	case http.StatusForbidden:
		return fmt.Errorf("forbidden: access denied")
	case http.StatusOK:
		// 继续处理
	default:
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// 检查内容类型
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		return fmt.Errorf("unexpected content type: %s", contentType)
	}
	if onConnected != nil {
		onConnected()
	}
	c.mark_activity()
	slog.Debug("SSE connected successfully")
	c.options.OnConnected()

	// 使用原始的 resp.Body，不再包装
	return c.readEventStream(resp.Body)
}

// 优化 readEventStream - 确保资源清理
func (c *SSEClient) readEventStream(body io.Reader) error {
	bodyCloser, ok := body.(io.ReadCloser)
	if !ok {
		bodyCloser = io.NopCloser(body)
	}

	scanner := newContextAwareScanner(c.ctx, bodyCloser)
	defer func() {
		// 确保 scanner 被关闭
		scanner.Close()
		slog.Debug("readEventStream exited")
	}()

	buf := make([]byte, c.options.ScannerBufferSize)
	scanner.Buffer(buf, c.options.ScannerMaxTokenSize)

	var message sse_message
	stopCh := make(chan struct{})
	defer close(stopCh)

	var wg sync.WaitGroup
	wg.Add(1)

	// 心跳检测 goroutine
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(c.options.HeartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-c.ctx.Done():
				slog.Debug("Heartbeat: context cancelled")
				return
			case <-ticker.C:
				select {
				case <-c.ctx.Done():
					return
				case <-stopCh:
					return
				default:
				}

				threshold := c.options.InactivityThreshold
				if c.since_last_activity() > threshold {
					slog.Warn("SSE inactivity timeout", "threshold", threshold, "last_activity", time.Unix(0, c.activityUnix.Load()))
					// 记录关闭原因
					c.mu.Lock()
					c.closeReason = "heartbeat timeout"
					c.mu.Unlock()
					// 超时后主动关闭 scanner
					scanner.Close()
					return
				}
			}
		}
	}()

	// 主循环
	for {
		select {
		case <-c.ctx.Done():
			slog.Debug("Main loop: context cancelled")
			wg.Wait()
			return nil
		default:
		}

		// Scan() 会在连接关闭后立即返回 false
		if !scanner.Scan() {
			wg.Wait()
			err := scanner.Err()
			if err != nil {
				// 改进：更全面地识别"正常关闭"错误
				errStr := err.Error()
				if errors.Is(err, context.Canceled) ||
					errors.Is(err, io.EOF) ||
					errors.Is(err, io.ErrClosedPipe) ||
					strings.Contains(errStr, "use of closed") ||
					strings.Contains(errStr, "deadline") ||
					strings.Contains(errStr, "i/o timeout") || // 新增
					strings.Contains(errStr, "timeout") || // 新增
					strings.Contains(errStr, "connection reset") { // 新增
					slog.Debug("Connection closed normally", "error", err)
					return nil
				}
				return err
			}
			return nil
		}

		line := scanner.Text()

		if line == "" {
			if message.Data != "" || message.Event != "" {
				c.mark_activity()
				c.handleMessage(message.ID, message.Event, message.Data)
				message = sse_message{}
			}
			continue
		}

		if strings.HasPrefix(line, ":") {
			c.mark_activity()
			continue
		}

		if before, after, ok := strings.Cut(line, ":"); ok {
			field := before
			value := after
			if len(value) > 0 && value[0] == ' ' {
				value = value[1:]
			}

			switch field {
			case "id":
				message.ID = value
			case "event":
				message.Event = value
			case "data":
				if message.Data != "" {
					message.Data += "\n"
				}
				message.Data += value
			case "retry":
				slog.Debug("Received retry field, ignoring", "value", value)
			default:
				slog.Debug("Unknown field in SSE", "field", field, "value", value)
			}
		}
	}
}

func (c *SSEClient) handleMessage(id, event, data string) {
	// 添加 panic 恢复
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Handler panicked",
				"panic", r,
				"event", event,
				"id", id,
			)
		}
	}()

	id = strings.TrimSpace(id)
	event = strings.TrimSpace(event)
	data = strings.TrimSpace(data)

	// 保存 Last Event ID
	if id != "" && c.options.SaveLastEventID != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("SaveID panicked", "panic", r, "id", id)
				}
			}()
			c.options.SaveLastEventID(id)
		}()
	}

	// 无 event 字段时，走 OnMessage 处理
	if event == "" {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("Handler panicked", "panic", r, "id", id)
				}
			}()
			c.options.OnMessage(data)
		}()
		return
	}

	// 处理系统事件
	switch sse_sys_event(event) {
	case EVT_SYS_CONNECTED:
		slog.Info("SSE connected successfully")
		return

	case EVT_SYS_KICK_OFFLINE:
		slog.Warn("Received kick offline event, closing connection", "reason", data)
		c.mu.Lock()
		c.closeReason = "kick offline: " + data
		c.mu.Unlock()
		c.options.OnKickOffline(data)
		c.closeInternal(false) // 不等待，直接退出
		return

	case EVT_SYS_EXTRUDE_OFFLINE:
		slog.Warn("Received extrude offline event, closing connection", "reason", data)
		c.mu.Lock()
		c.closeReason = "extrude offline: " + data
		c.mu.Unlock()
		c.options.OnExtrudeOffline(data)
		c.closeInternal(false) // 不等待，直接退出
		return

	case EVT_SYS_INSTANCE_CLOSE:
		slog.Warn("Received instance close event", "reason", data)
		c.mu.Lock()
		c.closeReason = "instance close: " + data
		c.mu.Unlock()
		c.closeInternal(false) // 不等待，直接退出
		return

	default:
		// 其他事件，交给业务系统处理
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("Handler panicked", "panic", r, "event", event, "id", id)
				}
			}()
			c.options.OnEvent(event, data)
		}()
	}
}
