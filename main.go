package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sse-broker/funcs"
	"sse-broker/sse"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	config        *Config
	accessLogFile *os.File
	errorLogFile  *os.File
	appLogFile    *os.File
)

//go:embed static/*
var staticFiles embed.FS

// 监测服务关闭信号
func handleShutdown() {
	// 创建一个 channel 来接收操作系统信号
	signalChan := make(chan os.Signal, 1)

	// 捕获 SIGINT (Ctrl+C) 和 SIGTERM (systemctl stop) 信号
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	// 等待信号
	sig := <-signalChan
	log.Printf("Received signal: %s. Shutting down...", sig)

	sse.Stop()

	sse.Dispose()

	// 关闭日志文件
	if accessLogFile != nil {
		accessLogFile.Close()
	}
	if errorLogFile != nil {
		errorLogFile.Close()
	}
	if appLogFile != nil {
		appLogFile.Close()
	}

	os.Exit(0)
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

	var configPath string
	flag.StringVar(&configPath, "config", "/etc/sse-broker/config.toml", "config file path")
	flag.Parse()

	baseDir := funcs.GetExecutionPath()
	cfg, err := loadConfig(baseDir, configPath)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		panic(fmt.Sprintf("Failed to load config: %v\n", err))
	}
	config = cfg
	a, e, p := initLogger(baseDir, config)
	accessLogFile = a
	errorLogFile = e
	appLogFile = p

	sse.Start(sse.Config{
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
	})
}

func main() {
	// 设置 Gin 运行模式为 release
	gin.SetMode(gin.ReleaseMode)

	go handleShutdown()

	// 创建Gin引擎
	engine := gin.Default()

	// 设置静态文件目录
	// 将嵌入的文件系统转换为 http.FileSystem
	staticFS := http.FS(staticFiles)
	engine.StaticFS("/static", staticFS)
	engine.GET("/", func(ctx *gin.Context) {
		ctx.FileFromFS("static/index.html", staticFS)
	})
	engine.GET("/index.html", func(ctx *gin.Context) {
		ctx.Redirect(http.StatusFound, "/static/index.html")
	})
	engine.GET("/favicon.ico", func(ctx *gin.Context) {
		ctx.Redirect(http.StatusFound, "/static/favicon.ico")
	})
	engine.GET("/demo", func(ctx *gin.Context) {
		ctx.Redirect(http.StatusFound, "/static/demo.html")
	})

	engine.GET("/events", sse.TokenCheck(), sse.HandleEvents)

	engine.Any("/token", sse.HandleToken)
	engine.Any("/send", sse.HandleSend)

	log.Printf("SSE-Broker Started, Listening Port: %d, Instance IP: %s\n", config.Server.Port, sse.GetIP())

	// 启动服务
	engine.Run(fmt.Sprintf(":%d", config.Server.Port))

}
