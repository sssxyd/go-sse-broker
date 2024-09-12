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

//go:embed static/**
var staticFiles embed.FS

const version = "1.0.2"

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
		if os.Getenv("OS") == "Windows_NT" {
			configPath = "config.toml"
		} else {
			configPath = "/etc/sse-broker/config.toml"
		}
	}
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

	// 设置静态文件路由
	engine.GET("/static/*filepath", func(ctx *gin.Context) {
		staticServer := http.FileServer(http.FS(staticFiles))
		staticServer.ServeHTTP(ctx.Writer, ctx.Request)
	})
	engine.GET("/", func(ctx *gin.Context) {
		ctx.Redirect(http.StatusMovedPermanently, "/static/index.html")
	})
	engine.GET("/favicon.ico", func(ctx *gin.Context) {
		favicon, err := staticFiles.ReadFile("static/favicon.ico")
		if err != nil {
			ctx.String(http.StatusNotFound, "Favicon not found")
			return
		}
		ctx.Data(http.StatusOK, "image/x-icon", favicon)
	})

	// 设置API路由
	engine.GET("/events", sse.TokenCheck(), sse.HandleEvents)
	engine.Any("/token", sse.HandleToken)
	engine.Any("/send", sse.HandleSend)
	engine.Any("/info", sse.HandleInfo)
	engine.Any("/kikc", sse.HandleKick)

	log.Printf("SSE-Broker Started, Listening Port: %d, Instance IP: %s\n", config.Server.Port, sse.GetIP())

	// 启动服务
	engine.Run(fmt.Sprintf(":%d", config.Server.Port))

}
