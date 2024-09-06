package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sse_broker/funcs"

	"github.com/gin-gonic/gin"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Server struct {
		Port          int    `toml:"port"`
		StaticDir     string `toml:"static_dir"`
		AccessLogPath string `toml:"access_log_path"`
		ErrorLogPath  string `toml:"error_log_path"`
		AppLogPath    string `toml:"app_log_path"`
	} `toml:"server"`
	JWT struct {
		Secret string `toml:"secret"`
		Expire int    `toml:"expire"`
	} `toml:"jwt"`
	Redis struct {
		Addrs    []string `toml:"addrs"`
		Password string   `toml:"password"`
		DB       int      `toml:"db"`
		PoolSize int      `toml:"pool_size"`
	} `toml:"redis"`
	SSE struct {
		HeartbeatInterval    int `toml:"heartbeat_interval"`
		DeviceFrameCacheSize int `toml:"device_frame_cache_size"`
		DeviceFrameExpire    int `toml:"device_frame_cache_expire"`
	} `toml:"sse"`
}

func loadConfig(baseDir string) (*Config, error) {
	config := Config{}
	file, err := os.Open(filepath.Join(baseDir, "config.toml"))
	if err != nil {
		fmt.Printf("Failed to open config file: %v", err)
	}
	defer file.Close()
	decoder := toml.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		fmt.Printf("Failed to decode config file: %v", err)
	}
	if config.SSE.HeartbeatInterval <= 0 {
		config.SSE.HeartbeatInterval = 30
	}
	return &config, nil
}

func touchStaticDir(baseDir string, config *Config) {
	// static dir
	if !filepath.IsAbs(config.Server.StaticDir) {
		config.Server.StaticDir = filepath.Join(baseDir, config.Server.StaticDir)
	}
	funcs.TouchDir(config.Server.StaticDir)
}

func initLogger(baseDir string, config *Config) (accessLogFile, errorLogFile, appLogFile *os.File) {
	// access log path
	if !filepath.IsAbs(config.Server.AccessLogPath) {
		config.Server.AccessLogPath = filepath.Join(baseDir, config.Server.AccessLogPath)
	}
	funcs.TouchDir(filepath.Dir(config.Server.AccessLogPath))
	accessLogFile, err := os.OpenFile(config.Server.AccessLogPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	gin.DefaultWriter = io.MultiWriter(accessLogFile, os.Stdout)

	// error log path
	if !filepath.IsAbs(config.Server.ErrorLogPath) {
		config.Server.ErrorLogPath = filepath.Join(baseDir, config.Server.ErrorLogPath)
	}
	funcs.TouchDir(filepath.Dir(config.Server.ErrorLogPath))
	errorLogFile, err = os.OpenFile(config.Server.ErrorLogPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	gin.DefaultErrorWriter = io.MultiWriter(errorLogFile, os.Stderr)

	// app log path
	if !filepath.IsAbs(config.Server.AppLogPath) {
		config.Server.AppLogPath = filepath.Join(baseDir, config.Server.AppLogPath)
	}
	appLogFile = funcs.InitializeLogFile(config.Server.AppLogPath, true)
	return accessLogFile, errorLogFile, appLogFile
}
