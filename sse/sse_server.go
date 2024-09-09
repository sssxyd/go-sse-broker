package sse

import (
	"fmt"
	"log"
	"sse-broker/funcs"
	"strings"
	"sync"
)

var (
	globalConfig    *Config
	globalInstance  *ServiceInstance
	globalRedis     *funcs.RedisClient
	deviceChannels  = &sync.Map{}
	deviceChannelWG sync.WaitGroup // 用于等待所有goroutines完成的WaitGroup
)

func Start(config Config) {
	globalConfig = &config
	jwtInit(config.JWT.Secret, config.JWT.Expire)
	reidsClient, localIP, _, err := funcs.NewRedisClient(config.Redis.Addrs, config.Redis.Password, config.Redis.DB, config.Redis.PoolSize)
	globalRedis = reidsClient
	if err != nil {
		fmt.Printf("Failed to create redis client: %v\n", err)
		panic(fmt.Sprintf("Failed to create redis client: %v\n", err))
	}
	globalInstance = NewServiceInstance(localIP)
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
	log.Println(globalInstance.Address)
	index := strings.LastIndex(globalInstance.Address, ":")
	if index == -1 {
		return globalInstance.Address
	}
	return globalInstance.Address[:index]
}
