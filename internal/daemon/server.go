package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Server 是「一个进程里那个 HTTP 服务」，**监听可以在运行中换掉**。
//
// 为什么需要它：设置页与首次向导都能改端口和绑定地址。这一版不再要求重启
// （docs/04 里“热重开排在 UI 这一刀”）。做法是先把新端口真的绑上，成功了再关旧的；
// 绑失败就什么也不动，用户看到的是一条能读懂的报错，而不是“服务没了”。
//
// 旧连接不会被打断：`http.Server` 只被关掉监听，已经建立的连接照常把响应写完。
// 所以 POST /api/settings 的响应能正常送回旧页面，页面再从响应里拿到新地址。
type Server struct {
	mu      sync.Mutex
	handler *Handler
	http    *http.Server
	ln      net.Listener
	port    int
	closed  bool
	done    chan struct{}
	// serveErr 只在监听真的出错时才收到东西；换代关掉的监听不算错。
	serveErr chan error
}

// NewServer 建服务。它不会开始监听，要再调 Start。
func NewServer(mgr *Manager, readToken, apiToken string, opts HandlerOptions) *Server {
	h := NewHandler(mgr, readToken, apiToken, opts)
	s := &Server{
		handler:  h,
		done:     make(chan struct{}),
		serveErr: make(chan error, 4),
		http: &http.Server{
			Handler:           h.Routes(),
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
	// 让 POST /api/settings 能直接调回来重开。
	s.handler.opts.Reload = s.Reload
	return s
}

// Handler 返回内部 Handler（测试与状态用）。
func (s *Server) Handler() *Handler { return s.handler }

// Port 返回当前实际监听端口。
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// Addr 返回能直接点开的地址（横幅与 /api/status 用）。
//
// 绑 0.0.0.0 时不能把它原样拼进地址：那是“监听所有网卡”，不是“可访问的主机”。
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return "http://" + net.JoinHostPort(hostForURL(s.handler.mgr.Config().Host), strconv.Itoa(s.port))
}

// hostForURL 把 0.0.0.0 / :: 换算成能点开的地址。
func hostForURL(host string) string {
	switch host {
	case "", "0.0.0.0":
		return "127.0.0.1"
	case "::", "[::]":
		return "::1"
	default:
		return host
	}
}

// Start 首次监听：从 port 往上找第一个能绑的（docs/04：被占时自动 +1）。
func (s *Server) Start(host string, port int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, errors.New("服务已经关了")
	}
	if s.ln != nil {
		return s.port, nil
	}

	ln, actual, err := listenLoop(host, port)
	if err != nil {
		return 0, err
	}
	s.ln = ln
	s.port = actual
	s.handler.SetBinding(actual, !IsLoopbackHost(host))
	s.serveLocked()
	return actual, nil
}

// Reload 换监听：改端口或绑定地址后调用，返回实际端口。
//
// 顺序是刻意的：**先绑新的，成功了才关旧的**。反过来的话，新端口一旦被占，
// 用户连“改回去”都做不到，因为旧地址也没了。
func (s *Server) Reload(host string, port int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, errors.New("服务已经关了")
	}

	ln, actual, err := listenLoop(host, port)
	if err != nil {
		return 0, err
	}

	old := s.ln
	s.ln = ln
	s.port = actual
	// 先更新再关旧监听：Host / Origin 校验立刻按新端口判。
	s.handler.SetBinding(actual, !IsLoopbackHost(host))
	s.serveLocked()

	if old != nil {
		// 关监听会让老的 Serve 立刻返回；那属于换代，不当故障。
		_ = old.Close()
	}
	return actual, nil
}

// serveLocked 为一个监听起一个 Serve 协程。调用方必须持有 s.mu。
func (s *Server) serveLocked() {
	ln := s.ln
	go func() {
		err := s.http.Serve(ln)
		if err == nil || isClosedConnErr(err) {
			return
		}
		select {
		case s.serveErr <- err:
		default:
		}
	}()
}

// ServeErr 把真正的监听错误抛给调用方。
func (s *Server) ServeErr() <-chan error { return s.serveErr }

// Shutdown 优雅退出：先停监听，再给在跑的请求 3 秒把话说完。
func (s *Server) Shutdown() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	ln := s.ln
	s.ln = nil
	close(s.done)
	s.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.http.Shutdown(ctx)
}

// Wait 阻塞到退出。监听中途真出错时返回那个错。
func (s *Server) Wait() error {
	select {
	case err := <-s.serveErr:
		return err
	case <-s.done:
		return nil
	}
}

// listenLoop 从 startPort 往上找第一个能绑的端口（docs/04：被占时自动 +1）。
//
// startPort 传 0 表示“随便给一个”，这时返回的是系统分配的那个端口（测试用）。
func listenLoop(host string, startPort int) (net.Listener, int, error) {
	var lastErr error
	end := startPort + 20
	if startPort == 0 {
		end = 1
	}
	for p := startPort; p < end; p++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(p)))
		if err == nil {
			actual := p
			if addr, ok := ln.Addr().(*net.TCPAddr); ok {
				actual = addr.Port
			}
			return ln, actual, nil
		}
		lastErr = err
	}
	return nil, 0, fmt.Errorf("端口 %d–%d 全被占用：%v", startPort, startPort+19, lastErr)
}

// isClosedConnErr 判断“监听被我关掉了”这类错误。换代会走到这里，不该当故障报。
func isClosedConnErr(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, http.ErrServerClosed)
}
