package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sse-broker/funcs"

	"github.com/gin-gonic/gin"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Server struct {
		Port          int    `toml:"port"`
		AccessLogPath string `toml:"access_log_path"`
		ErrorLogPath  string `toml:"error_log_path"`
		BrokerLogPath string `toml:"broker_log_path"`
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

func loadConfig(baseDir string, configPath string) (*Config, error) {
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(baseDir, configPath)
	}
	config := Config{}
	file, err := os.Open(configPath)
	if err != nil {
		fmt.Printf("Failed to open config file %s: %v", configPath, err)
		return nil, err
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
	if !filepath.IsAbs(config.Server.BrokerLogPath) {
		config.Server.BrokerLogPath = filepath.Join(baseDir, config.Server.BrokerLogPath)
	}
	appLogFile = funcs.InitializeLogFile(config.Server.BrokerLogPath, true)
	return accessLogFile, errorLogFile, appLogFile
}
