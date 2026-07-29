package local

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type SSEConnectedHandler func()
type SSEDisConnectHandler func(reason string, err error)
type SSEOfflineHandler func(reason string)
type SSEEventHandler func(event, data string)
type SSEMessageHandler func(data string)
type SSEGetTokenHandler func() string
type SSESaveIDHandler func(id string) error
type SSEGetIDHandler func() string

type SSEOptions struct {
	SSEEventsURL        string               // SSE 事件流的 URL
	ClientDeviceID      string               // 客户端设备 ID
	InactivityThreshold time.Duration        // SSE 连接的非活动阈值
	DialTimeout         time.Duration        // SSE 连接的拨号超时时间
	KeepAlive           time.Duration        // SSE 连接的心跳间隔时间
	HeartbeatInterval   time.Duration        // SSE 连接的心跳间隔时间
	ScannerBufferSize   int                  // Scanner 缓冲区大小
	ScannerMaxTokenSize int                  // Scanner 最大 token 大小
	OnConnected         SSEConnectedHandler  // 连接成功回调
	OnDisconnect        SSEDisConnectHandler // 连接断开回调
	OnKickOffline       SSEOfflineHandler    // 被踢下线回调
	OnExtrudeOffline    SSEOfflineHandler    // 被挤下线回调
	OnMessage           SSEMessageHandler    // 无Event字段的Message消息回调
	OnEvent             SSEEventHandler      // 通用事件回调
	GetAccessToken      SSEGetTokenHandler   // 获取访问令牌回调
	SaveLastEventID     SSESaveIDHandler     // 保存最后事件 ID 回调
	GetLastEventID      SSEGetIDHandler      // 获取最后事件 ID 回调
}

// 连接关闭器包装（确保只关闭一次）
type connectionCloser struct {
	closeOnce sync.Once
	closeFunc func()
}

func newConnectionCloser(fn func()) *connectionCloser {
	return &connectionCloser{
		closeFunc: fn,
	}
}

func (cc *connectionCloser) Close() {
	cc.closeOnce.Do(cc.closeFunc)
}

// 可强制关闭的连接包装
type forceCloseConn struct {
	net.Conn
	closed atomic.Bool
	mu     sync.Mutex
}

func (fc *forceCloseConn) Read(b []byte) (n int, err error) {
	if fc.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	return fc.Conn.Read(b)
}

func (fc *forceCloseConn) Write(b []byte) (n int, err error) {
	if fc.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	return fc.Conn.Write(b)
}

func (fc *forceCloseConn) Close() error {
	if !fc.closed.CompareAndSwap(false, true) {
		return nil
	}
	return fc.Conn.Close()
}

// ForceClose 强制关闭连接(终极方案)
func (fc *forceCloseConn) ForceClose() error {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if fc.closed.Load() {
		return nil
	}

	// 1. 先尝试优雅关闭 (TCP FIN)
	if tcpConn, ok := fc.Conn.(*net.TCPConn); ok {
		_ = tcpConn.CloseWrite()          // 发送 FIN
		time.Sleep(50 * time.Millisecond) // 等待服务端响应
	}

	// 2. 设置过期 deadline (触发 Read/Write 立即返回)
	past := time.Now().Add(-time.Hour)
	_ = fc.Conn.SetDeadline(past)
	_ = fc.Conn.SetReadDeadline(past)
	_ = fc.Conn.SetWriteDeadline(past)

	// 3. 只有在需要时才使用 RST 关闭
	if tcpConn, ok := fc.Conn.(*net.TCPConn); ok {
		// 移除 SetLinger(0)，使用正常关闭
		// _ = tcpConn.SetLinger(0)  // 删除这行
		_ = tcpConn.CloseRead()
	}

	fc.closed.Store(true)

	// 4. 关闭连接
	err := fc.Conn.Close()
	if err != nil && !strings.Contains(err.Error(), "use of closed") {
		slog.Warn("ForceClose error", "error", err)
	}
	return nil
}

// 改进 contextAwareScanner - 防止 goroutine 泄漏
type contextAwareScanner struct {
	scanner *bufio.Scanner
	ctx     context.Context
	body    io.ReadCloser
	closed  atomic.Bool
	mu      sync.Mutex
}

func newContextAwareScanner(ctx context.Context, body io.ReadCloser) *contextAwareScanner {
	return &contextAwareScanner{
		scanner: bufio.NewScanner(body),
		ctx:     ctx,
		body:    body,
	}
}

func (cs *contextAwareScanner) Buffer(buf []byte, max int) {
	cs.scanner.Buffer(buf, max)
}

func (cs *contextAwareScanner) Text() string {
	return cs.scanner.Text()
}

func (cs *contextAwareScanner) Err() error {
	return cs.scanner.Err()
}

// 简化 Scan - 直接调用,依赖连接关闭来中断
func (cs *contextAwareScanner) Scan() bool {
	if cs.closed.Load() {
		return false
	}

	// 检查 context
	select {
	case <-cs.ctx.Done():
		return false
	default:
	}

	// 直接调用 scanner.Scan()
	// 当底层连接被 ForceClose 关闭后,Read() 会立即返回错误
	return cs.scanner.Scan()
}

func (cs *contextAwareScanner) Close() error {
	if !cs.closed.CompareAndSwap(false, true) {
		return nil
	}

	// 先关闭 body,中断阻塞的 Read
	err := cs.body.Close()

	slog.Debug("contextAwareScanner closed")
	return err
}
