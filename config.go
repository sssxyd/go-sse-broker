package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Server struct {
		Port int `toml:"port"`
	} `toml:"server"`
	AccessLog struct {
		Path         string `toml:"path"`
		MaxMegaBytes int    `toml:"max_mega_bytes"`
		MaxAgeDay    int    `toml:"max_age_day"`
		MaxBackups   int    `toml:"max_backups"`
		Compress     bool   `toml:"compress"`
	}
	ErrorLog struct {
		Path         string `toml:"path"`
		MaxMegaBytes int    `toml:"max_mega_bytes"`
		MaxAgeDay    int    `toml:"max_age_day"`
		MaxBackups   int    `toml:"max_backups"`
		Compress     bool   `toml:"compress"`
	}
	BrokerLog struct {
		Level        string `toml:"level"`
		Path         string `toml:"path"`
		MaxMegaBytes int    `toml:"max_mega_bytes"`
		MaxAgeDay    int    `toml:"max_age_day"`
		MaxBackups   int    `toml:"max_backups"`
		Compress     bool   `toml:"compress"`
	}
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
	Callback struct {
		UserOnline    string `toml:"user_online"`
		UserOffline   string `toml:"user_offline"`
		DeviceOnline  string `toml:"device_online"`
		DeviceOffline string `toml:"device_offline"`
	} `toml:"callback"`
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
