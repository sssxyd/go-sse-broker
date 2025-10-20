package main

import (
	"embed"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sse-broker/funcs"
	"sse-broker/sse"
	"strings"
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
	config       *Config
	accessLog    *lumberjack.Logger
	accessLogger io.Writer
	errorLog     *lumberjack.Logger
	errorLogger  io.Writer
)

//go:embed static/**
var staticFiles embed.FS

const version = "1.0.7"

func is_windows() bool {
	return strings.Contains(strings.ToLower(os.Getenv("OS")), "windows")
}

func create_logger(config *Config) {
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
	brokerLogger := funcs.InitializeLumberjackLogger(config.BrokerLog.Path, config.BrokerLog.MaxMegaBytes, config.BrokerLog.MaxAgeDays, config.BrokerLog.MaxBackups, config.BrokerLog.Compress)

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
	if configPath == "" {
		if is_windows() {
			configPath = "config.toml"
		} else {
			configPath = "/etc/sse-broker/config.toml"
		}
	}
	baseDir := funcs.GetAppRootPath()
	cfg, err := loadConfig(baseDir, configPath)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		panic(fmt.Sprintf("Failed to load config: %v\n", err))
	}
	config = cfg

	create_logger(config)

	sse.Start(sse.Config{
		Server: struct {
			Version string
			Port    int
		}{Version: version, Port: config.Server.Port},
		JWT: struct {
			Secret string
			Expire int
		}{Secret: config.JWT.Secret, Expire: config.JWT.Expire},
		Redis: struct {
			Addrs    []string
			Password string
			DB       int
			PoolSize int
		}{Addrs: config.Redis.Addrs, Password: config.Redis.Password, DB: config.Redis.DB, PoolSize: config.Redis.PoolSize},
		SSE: struct {
			HeartbeatDuration         time.Duration
			DeviceUserExistDuration   time.Duration
			DeviceFrameExpireDuration time.Duration
			DeviceFrameCacheSize      int
		}{
			HeartbeatDuration:         time.Duration(config.SSE.HeartbeatInterval) * time.Second,
			DeviceUserExistDuration:   time.Duration(config.SSE.HeartbeatInterval+5) * time.Second,
			DeviceFrameExpireDuration: time.Duration(config.SSE.DeviceFrameExpire) * time.Second,
			DeviceFrameCacheSize:      config.SSE.DeviceFrameCacheSize,
		},
		Callback: map[string]string{
			sse.TOPIC_USER_ONLINE:    config.Callback.UserOnline,
			sse.TOPIC_USER_OFFLINE:   config.Callback.UserOffline,
			sse.TOPIC_DEVICE_ONLINE:  config.Callback.DeviceOnline,
			sse.TOPIC_DEVICE_OFFLINE: config.Callback.DeviceOffline,
		},
	})
}

func main() {
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
		StackTraceHandler: func(c *fiber.Ctx, e interface{}) {
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

	instanceIP := sse.GetIP()
	instancePort := config.Server.Port
	slog.Info("SSE-Broker Started", "port", instancePort)
	slog.Info("Instance IP", "ip", instanceIP, "version", version)
	slog.Info("API  Page", "url", fmt.Sprintf("http://%s:%d/", instanceIP, instancePort))
	slog.Info("Demo Page", "url", fmt.Sprintf("http://%s:%d/static/demo.html", instanceIP, instancePort))

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

	// 启动服务（阻塞直到关闭）
	if err := app.Listen(fmt.Sprintf(":%d", instancePort)); err != nil {
		slog.Error("Server stopped", "error", err.Error())
	}

	// 释放资源
	sse.Dispose()
	if accessLog != nil {
		accessLog.Close()
	}
	if errorLog != nil {
		errorLog.Close()
	}
}
