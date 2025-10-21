package main

import (
	"embed"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sse-broker/config"
	"sse-broker/funcs"
	"sse-broker/sse"
	"strings"
	"sync"
	"syscall"
	"time"

	"runtime/debug"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	configObj    *config.Config
	accessLog    *lumberjack.Logger
	accessLogger io.Writer
	errorLog     *lumberjack.Logger
	errorLogger  io.Writer
	brokerLogger *lumberjack.Logger
)

//go:embed static/**
var staticFiles embed.FS

const version = "1.0.7"

func is_windows() bool {
	return strings.Contains(strings.ToLower(os.Getenv("OS")), "windows")
}

func parse_addrs(ipv4, ipv6 string, port int) []string {
	var addrs []string
	if ipv4 != "" {
		ips := strings.Split(ipv4, ",")
		for _, ip := range ips {
			trimmedIP := strings.TrimSpace(ip)
			if trimmedIP != "" {
				addrs = append(addrs, fmt.Sprintf("%s:%d", trimmedIP, port))
			}
		}
	}
	if ipv6 != "" {
		ips := strings.Split(ipv6, ",")
		for _, ip := range ips {
			trimmedIP := strings.TrimSpace(ip)
			if trimmedIP != "" {
				addrs = append(addrs, fmt.Sprintf("[%s]:%d", trimmedIP, port))
			}
		}
	}
	return addrs
}

func get_config_path(baseDir string, configPath string) (string, error) {
	if configPath != "" {
		if !filepath.IsAbs(configPath) {
			configPath = filepath.Join(baseDir, configPath)
		}
		if funcs.PathExists(configPath) {
			return configPath, nil
		}
	}
	if is_windows() {
		configPath = filepath.Join(baseDir, "config.toml")
		if !funcs.PathExists(configPath) {
			configPath = filepath.Join(baseDir, "config.windows.toml")
		}
		if !funcs.PathExists(configPath) {
			return "", fmt.Errorf("config file not found")
		} else {
			return configPath, nil
		}
	}
	configPath = "/etc/sse-broker/config.toml"
	if !funcs.PathExists(configPath) {
		configPath = filepath.Join(baseDir, "config.toml")
		if !funcs.PathExists(configPath) {
			return "", fmt.Errorf("config file not found")
		}
	}
	return configPath, nil
}

func create_logger(config *config.Config) {
	// 解析日志级别
	var level slog.Level
	switch config.BrokerLog.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	accessLog = funcs.InitializeLumberjackLogger(config.AccessLog.Path, config.AccessLog.MaxMegaBytes, config.AccessLog.MaxAgeDays, config.AccessLog.MaxBackups, config.AccessLog.Compress)
	errorLog = funcs.InitializeLumberjackLogger(config.ErrorLog.Path, config.ErrorLog.MaxMegaBytes, config.ErrorLog.MaxAgeDays, config.ErrorLog.MaxBackups, config.ErrorLog.Compress)
	brokerLogger = funcs.InitializeLumberjackLogger(config.BrokerLog.Path, config.BrokerLog.MaxMegaBytes, config.BrokerLog.MaxAgeDays, config.BrokerLog.MaxBackups, config.BrokerLog.Compress)

	var writer io.Writer
	if is_windows() {
		// windows环境下，同时输出到控制台和文件
		accessLogger = io.MultiWriter(os.Stdout, accessLog)
		errorLogger = io.MultiWriter(os.Stderr, errorLog)
		writer = io.MultiWriter(os.Stdout, brokerLogger)

	} else {
		accessLogger = accessLog
		errorLogger = errorLog
		writer = io.MultiWriter(brokerLogger)
	}
	// 创建文本格式的 handler，包含源码位置信息
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
	}
	// 使用 JSON 格式（时间格式更标准）
	handler := slog.NewJSONHandler(writer, opts)
	// 设置为默认 logger
	slog.SetDefault(slog.New(handler))
}

// 判断是否为通用探测/调试/健康检查等路径
func is_common_probe_path(path string) bool {
	if strings.HasPrefix(path, "/.well-known/") {
		return true
	}
	if path == "/robots.txt" || path == "/sitemap.xml" {
		return true
	}
	if path == "/health" || path == "/healthz" || path == "/status" || path == "/ping" {
		return true
	}
	if strings.HasPrefix(path, "/__webpack_hmr") || strings.HasPrefix(path, "/__vite_") || strings.HasPrefix(path, "/__browser_sync__") {
		return true
	}
	if path == "/.env" || path == "/.git" || path == "/.gitignore" || path == "/.DS_Store" {
		return true
	}
	if strings.HasPrefix(path, "/browser-sync/") {
		return true
	}
	if strings.HasPrefix(path, "/debug") {
		return true
	}
	if strings.HasPrefix(path, "/api-docs") || strings.HasPrefix(path, "/swagger") || strings.HasPrefix(path, "/openapi.json") {
		return true
	}
	return false
}

func init() {
	// 设置Windows控制台为UTF-8编码
	// if os.Getenv("OS") == "Windows_NT" {
	// 	handle := windows.Handle(os.Stdout.Fd())
	// 	var mode uint32
	// 	windows.GetConsoleMode(handle, &mode)
	// 	mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	// 	windows.SetConsoleMode(handle, mode)
	// }

	var shortConfig string
	var configPath string
	shortVersion := flag.Bool("v", false, "show version")
	versionFlag := flag.Bool("version", false, "show version")
	flag.StringVar(&shortConfig, "c", "", "config file path")
	flag.StringVar(&configPath, "config", "", "config file path")
	flag.Parse()

	if *versionFlag || *shortVersion {
		fmt.Printf("Version: %s\n", version)
		os.Exit(0)
	}

	if configPath == "" {
		configPath = shortConfig
	}

	configPath, err := get_config_path(funcs.GetAppRootPath(), configPath)
	if err != nil {
		fmt.Printf("Failed to get config path: %v\n", err)
		panic(fmt.Sprintf("Failed to get config path: %v\n", err))
	}

	configObj, err = config.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		panic(fmt.Sprintf("Failed to load config: %v\n", err))
	}

	create_logger(configObj)

	sse.Start(sse.Config{
		Server: struct {
			Version string
			Port    int
		}{Version: version, Port: configObj.Server.Port},
		JWT: struct {
			Secret string
			Expire int
		}{Secret: configObj.JWT.Secret, Expire: configObj.JWT.Expire},
		Redis: struct {
			Addrs    []string
			Password string
			DB       int
			PoolSize int
		}{Addrs: configObj.Redis.Addrs, Password: configObj.Redis.Password, DB: configObj.Redis.DB, PoolSize: configObj.Redis.PoolSize},
		SSE: struct {
			HeartbeatDuration         time.Duration
			DeviceUserExistDuration   time.Duration
			DeviceFrameExpireDuration time.Duration
			DeviceFrameCacheSize      int
		}{
			HeartbeatDuration:         time.Duration(configObj.SSE.HeartbeatInterval) * time.Second,
			DeviceUserExistDuration:   time.Duration(configObj.SSE.HeartbeatInterval+5) * time.Second,
			DeviceFrameExpireDuration: time.Duration(configObj.SSE.DeviceFrameExpire) * time.Second,
			DeviceFrameCacheSize:      configObj.SSE.DeviceFrameCacheSize,
		},
		Callback: map[string]string{
			sse.TOPIC_USER_ONLINE:    configObj.Callback.UserOnline,
			sse.TOPIC_USER_OFFLINE:   configObj.Callback.UserOffline,
			sse.TOPIC_DEVICE_ONLINE:  configObj.Callback.DeviceOnline,
			sse.TOPIC_DEVICE_OFFLINE: configObj.Callback.DeviceOffline,
		},
	})
}

func main() {
	addrs := parse_addrs(configObj.Server.IPV4, configObj.Server.IPV6, configObj.Server.Port)
	if len(addrs) == 0 {
		slog.Error("No valid server addresses to listen on", "message", "please check the configuration")
		return
	}

	// 创建 Fiber 应用，并定制错误处理（写入 error.log）
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if fe, ok := err.(*fiber.Error); ok {
				code = fe.Code
			}
			// 时间 | 级别 | 状态码 | 方法 路径 | IP | 错误
			fmt.Fprintf(errorLogger, "%s | ERROR %d | %s %s | ip=%s | %s\n",
				time.Now().Format("2006-01-02 15:04:05"),
				code, c.Method(), c.OriginalURL(), c.IP(), err.Error(),
			)
			return c.Status(code).JSON(fiber.Map{
				"code":   code,
				"msg":    err.Error(),
				"result": "",
				"micro":  0,
			})
		},
	})

	// Panic 恢复并写栈到 error.log
	app.Use(recover.New(recover.Config{
		EnableStackTrace: true,
		StackTraceHandler: func(c *fiber.Ctx, e any) {
			if errorLogger != nil {
				fmt.Fprintf(errorLogger, "%s | PANIC | %s %s | ip=%s | %v\n%s\n",
					time.Now().Format("2006-01-02 15:04:05"),
					c.Method(), c.OriginalURL(), c.IP(), e, string(debug.Stack()),
				)
			}
		},
	}))

	// 访问日志写入 access.log
	app.Use(logger.New(logger.Config{
		Output:     accessLogger,
		TimeFormat: "2006-01-02 15:04:05",
		TimeZone:   "Local",
		// ${latency} 为处理耗时，${bytesSent} 等可按需添加
		Format: "${time} | ${ip} | ${status} | ${latency} | ${method} ${path}\n",
	}))

	// 静态文件（使用 embed FS）
	// 将 embed FS 子目录 "static" 作为根目录挂载到 /static
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		slog.Error("failed to sub fs", "error", err.Error())
	}
	app.Use("/static", filesystem.New(filesystem.Config{
		Root:   http.FS(sub),
		Browse: false,
		Index:  "index.html",
	}))

	app.Use(func(c *fiber.Ctx) error {
		// 如果是 devtools 探测路径，直接返回 204
		if is_common_probe_path(c.Path()) {
			return c.SendStatus(fiber.StatusNoContent)
		}
		return c.Next()
	})

	// 根路径重定向
	app.Get("/", func(c *fiber.Ctx) error {
		return c.Redirect("/static/index.html", http.StatusMovedPermanently)
	})

	// favicon（保持与原有路径兼容）
	app.Get("/favicon.ico", func(c *fiber.Ctx) error {
		favicon, err := staticFiles.ReadFile("static/favicon.ico")
		if err != nil {
			return c.Status(http.StatusNotFound).SendString("Favicon not found")
		}
		c.Set("Content-Type", "image/x-icon")
		return c.Send(favicon)
	})

	// API 路由
	app.Get("/events", sse.TokenCheck(), sse.HandleEvents)
	app.All("/token", sse.HandleToken)
	app.All("/send", sse.HandleSend)
	app.All("/info", sse.HandleInfo)
	app.All("/kick", sse.HandleKick)

	instancePort := configObj.Server.Port
	slog.Info("SSE Server Start On " + strings.Join(addrs, ","))
	slog.Info("API  Page", "url", fmt.Sprintf("http://%s/", addrs[0]))
	slog.Info("Demo Page", "url", fmt.Sprintf("http://%s/static/demo.html", addrs[0]))

	// 优雅关闭：监听信号，触发 Fiber 关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-quit
		slog.Info("Received signal", "signal", sig.String(), "message", "shutting down")
		// 先停止内部调度
		sse.Stop()
		if err := app.Shutdown(); err != nil {
			slog.Error("Fiber shutdown error", "error", err.Error())
		}
	}()

	if len(addrs) == 1 {
		// 启动服务（阻塞直到关闭）
		if err := app.Listen(fmt.Sprintf(":%d", instancePort)); err != nil {
			slog.Error("Server stopped", "error", err.Error())
		}
	} else {
		// 多地址监听
		var wg sync.WaitGroup
		for _, addr := range addrs {
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				slog.Error("Failed to listen on address", "address", addr, "error", err.Error())
				continue
			}
			wg.Add(1)
			go func(l net.Listener, a string) {
				defer wg.Done()
				slog.Info("Listening on address", "address", a)
				if err := app.Listener(l); err != nil {
					slog.Error("Server stopped on address", "address", a, "error", err.Error())
				}
			}(ln, addr)
		}
		wg.Wait()
	}

	// 释放资源
	sse.Dispose()
	if accessLog != nil {
		accessLog.Close()
	}
	if errorLog != nil {
		errorLog.Close()
	}
	if brokerLogger != nil {
		brokerLogger.Close()
	}
}
