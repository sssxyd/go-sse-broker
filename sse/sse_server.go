package sse

import (
	"fmt"
	"sse-broker/cfg"
	"sse-broker/funcs"
	"strings"
	"sync"
)

var (
	globalInstance  *ServiceInstance
	globalRedis     *funcs.RedisClient
	deviceChannels  = &sync.Map{}
	deviceChannelWG = &sync.WaitGroup{} // 用于等待所有goroutines完成的WaitGroup
)

func Start() {
	config := cfg.GlobalConfig()
	jwtInit(config.JWT.Secret, config.JWT.Expire)
	reidsClient, localIP, _, err := funcs.NewRedisClient(config.Redis.Addrs, config.Redis.Password, config.Redis.DB, config.Redis.PoolSize)
	globalRedis = reidsClient
	if err != nil {
		fmt.Printf("Failed to create redis client: %v\n", err)
		panic(fmt.Sprintf("Failed to create redis client: %v\n", err))
	}
	globalInstance = NewServiceInstance(config.Server.Version, fmt.Sprintf("%s:%d", localIP, config.Server.Port))
	globalInstance.clear()
	globalInstance.start()
}

func Stop() {
	globalInstance.stop()
}

func Dispose() {
	// 容错，防止有的channel没有关闭
	deviceChannels.Range(func(key, value interface{}) bool {
		close(value.(chan *Instruction))
		return true
	})
	globalInstance.dispose()
	globalRedis.Close()
}

func GetIP() string {
	index := strings.LastIndex(globalInstance.Address, ":")
	if index == -1 {
		return globalInstance.Address
	}
	return globalInstance.Address[:index]
}

func GetPort() int {
	return cfg.GlobalConfig().Server.Port
}

func GetVersion() string {
	return cfg.GlobalConfig().Server.Version
}
