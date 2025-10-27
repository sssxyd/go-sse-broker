package cfg

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/creasty/defaults"
	"github.com/pelletier/go-toml/v2"
)

var (
	globalConfig *Config
)

type Config struct {
	Server struct {
		IPV4    string `toml:"ipv4" defaults:"0.0.0.0"`
		IPV6    string `toml:"ipv6" defaults:""`
		Port    int    `toml:"port" defaults:"8080"`
		Version string `toml:"version" defaults:"1.0.0"`
	} `toml:"server"`
	AccessLog struct {
		Path         string `toml:"path" defaults:"./logs/access.log"`
		MaxMegaBytes int    `toml:"max_mega_bytes" defaults:"100"`
		MaxAgeDays   int    `toml:"max_age_days" defaults:"7"`
		MaxBackups   int    `toml:"max_backups" defaults:"3"`
		Compress     bool   `toml:"compress" defaults:"true"`
	} `toml:"access_log"`
	ErrorLog struct {
		Path         string `toml:"path" defaults:"./logs/error.log"`
		MaxMegaBytes int    `toml:"max_mega_bytes" defaults:"100"`
		MaxAgeDays   int    `toml:"max_age_days" defaults:"7"`
		MaxBackups   int    `toml:"max_backups" defaults:"3"`
		Compress     bool   `toml:"compress" defaults:"true"`
	} `toml:"error_log"`
	BrokerLog struct {
		Level        string `toml:"level" defaults:"info"`
		Path         string `toml:"path" defaults:"./logs/broker.log"`
		MaxMegaBytes int    `toml:"max_mega_bytes" defaults:"100"`
		MaxAgeDays   int    `toml:"max_age_days" defaults:"7"`
		MaxBackups   int    `toml:"max_backups" defaults:"3"`
		Compress     bool   `toml:"compress" defaults:"true"`
	} `toml:"broker_log"`
	JWT struct {
		Secret string `toml:"secret" defaults:""`
		Expire int    `toml:"expire" defaults:"86400"`
	} `toml:"jwt"`
	Redis struct {
		Addrs    []string `toml:"addrs" defaults:"[]"`
		Password string   `toml:"password" defaults:""`
		DB       int      `toml:"db" defaults:"0"`
		PoolSize int      `toml:"pool_size" defaults:"0"`
	} `toml:"redis"`
	SSE struct {
		HeartbeatInterval    int `toml:"heartbeat_interval" defaults:"30"`
		DeviceFrameCacheSize int `toml:"device_frame_cache_size" defaults:"100"`
		DeviceFrameExpire    int `toml:"device_frame_cache_expire" defaults:"86400"`
	} `toml:"sse"`
	Callback struct {
		UserOnline    string `toml:"user_online" defaults:""`
		UserOffline   string `toml:"user_offline" defaults:""`
		DeviceOnline  string `toml:"device_online" defaults:""`
		DeviceOffline string `toml:"device_offline" defaults:""`
	} `toml:"callback"`
}

func (c *Config) GetCallbackURL(topic string) string {
	topic = strings.ToLower(strings.TrimSpace(topic))
	if topic == "" {
		return ""
	}
	switch topic {
	case "user_online":
		return c.Callback.UserOnline
	case "user_offline":
		return c.Callback.UserOffline
	case "device_online":
		return c.Callback.DeviceOnline
	case "device_offline":
		return c.Callback.DeviceOffline
	}
	return ""
}

func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}
	if c.JWT.Secret == "" {
		return fmt.Errorf("JWT secret cannot be empty")
	}
	if len(c.Redis.Addrs) == 0 {
		return fmt.Errorf("Redis addresses cannot be empty")
	}
	return nil
}

func (c *Config) GetHeartbeatIntervalDuration() time.Duration {
	return time.Duration(c.SSE.HeartbeatInterval) * time.Second
}

func (c *Config) GetDeviceFrameExpireDuration() time.Duration {
	return time.Duration(c.SSE.DeviceFrameExpire) * time.Second
}

func (c *Config) GetDeviceUserExistDuration() time.Duration {
	return time.Duration(c.SSE.HeartbeatInterval+5) * time.Second
}

func LoadConfig(configPath string) (*Config, error) {
	config := &Config{}
	defaults.Set(config)

	file, err := os.Open(configPath)
	if err != nil {
		fmt.Printf("Failed to open config file %s: %v", configPath, err)
		return nil, err
	}
	defer file.Close()
	decoder := toml.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		fmt.Printf("Failed to decode config file: %v", err)
		return nil, err
	}
	if config.SSE.HeartbeatInterval <= 0 {
		config.SSE.HeartbeatInterval = 30
	}
	if err := config.Validate(); err != nil {
		fmt.Printf("Config validation error: %v", err)
		return nil, err
	}

	globalConfig = config

	return globalConfig, nil
}

func GlobalConfig() *Config {
	return globalConfig
}
