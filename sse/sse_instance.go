package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type ServiceInstance struct {
	Address     string `json:"address"`
	StartTime   string `json:"start_time"`
	DeviceCount int    `json:"device_count"`
	Devices     sync.Map
	TopicCancel context.CancelFunc
}

func subscribeInstanceTopic(ctx context.Context, topic string) {
	err := globalRedis.Subscribe(ctx, func(channel string, payload string) {
		var instruction Instruction
		err := json.Unmarshal([]byte(payload), &instruction)
		if err != nil {
			log.Printf("Failed to unmarshal instruction: %v\n", err)
			return
		}
		globalInstance.handleInstruction(&instruction)
	}, topic)
	if err != nil {
		log.Fatalf("Failed to subscribe instance topic: %v\n", err)
		panic(fmt.Sprintf("Failed to subscribe instance topic: %v\n", err))
	}
}

func NewServiceInstance(address string) *ServiceInstance {
	instacne := &ServiceInstance{
		Address:     address,
		StartTime:   time.Now().Format("2006-01-02 15:04:05"),
		DeviceCount: 0,
		Devices:     sync.Map{},
	}
	err := globalRedis.HSet(fmt.Sprintf("%s%s", KEY_INSTANCE_PREFIX, address),
		"address", instacne.Address,
		"start_time", instacne.StartTime,
		"device_count", 0)
	if err != nil {
		log.Fatalf("Failed to create instance: %v\n", err)
		return nil
	}
	return instacne
}

// 处理发给本实例的指令
func (s *ServiceInstance) handleInstruction(instruction *Instruction) {
	// log.Println("handleInstruction", instruction)
	if channel, ok := deviceChannels.Load(instruction.DeviceID); ok {
		inschannel, ok := channel.(chan *Instruction)
		if !ok {
			log.Printf("Device %s not found at %s\n", instruction.DeviceID, s.Address)
		} else {
			inschannel <- instruction
		}
	} else {
		log.Printf("Device %s not found at %s\n", instruction.DeviceID, s.Address)
	}
}

// 启动前清理本实例上次关机导致的残留及异常
func (s *ServiceInstance) clear() {
	ctx := context.Background()
	deviceSetKey := fmt.Sprintf("%s%s", KEY_INSTANCE_DEVICE_SET_PREFIX, s.Address)
	cmds, err := globalRedis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.SMembers(ctx, deviceSetKey)
		// clear from cluster instance set
		pipe.SRem(ctx, KEY_CLUSTER_INSTANCE_SET, s.Address)
		// delete instance info
		pipe.Del(ctx, fmt.Sprintf("%s%s", KEY_INSTANCE_PREFIX, s.Address))
		// delete device ids
		pipe.Del(ctx, deviceSetKey)
		return nil
	})
	if err != nil {
		log.Fatalf("Failed to clear instance: %v\n", err)
		return
	}
	deviceIDs, _ := cmds[0].(*redis.StringSliceCmd).Result()
	for _, deviceID := range deviceIDs {
		device := getRedisDevice(deviceID)
		if device != nil {
			device.offline(DCR_INSTANCE_CLEAR, s.Address)
			user := NewUser(device.UID)
			user.handleDeviceOffline(device)
		}
	}
}

func (s *ServiceInstance) start() bool {
	// 启动前清理本实例上次关机导致的残留及异常
	s.clear()

	// 本实例添加到实例集合
	_, err := globalRedis.SAdd(KEY_CLUSTER_INSTANCE_SET, s.Address)
	if err != nil {
		log.Fatalf("Failed to start instance: %v\n", err)
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.TopicCancel = cancel
	// 订阅实例Topic
	go subscribeInstanceTopic(ctx, fmt.Sprintf("%s%s", TOPIC_INSTANCE_PREFIX, s.Address))

	return true
}

func (s *ServiceInstance) stop() {
	// 停止订阅实例Topic
	s.TopicCancel()

	// 主动关闭本实例上连接的全部设备
	deviceChannels.Range(func(key, value interface{}) bool {
		deviceId := key.(string)
		channel, ok := value.(chan *Instruction)
		if ok {
			ins := &Instruction{
				DeviceID: deviceId,
				Command:  CMD_INSTANCE_CLOSE,
				Event:    "",
				Data:     s.Address,
			}
			channel <- ins
		}
		return true
	})
	// 等待所有设备连接关闭
	deviceChannelWG.Wait()
	log.Printf("Instance %s stopped\n", s.Address)
}

func (s *ServiceInstance) dispose() {
	ctx := context.Background()
	_, err := globalRedis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.SRem(ctx, KEY_CLUSTER_INSTANCE_SET, s.Address)
		pipe.Del(ctx, fmt.Sprintf("%s%s", KEY_INSTANCE_PREFIX, s.Address))
		pipe.Del(ctx, fmt.Sprintf("%s%s", KEY_INSTANCE_DEVICE_SET_PREFIX, s.Address))
		return nil
	})
	if err != nil {
		log.Fatalf("Failed to dispose instance: %v\n", err)
	}
}

func (s *ServiceInstance) getDevice(deviceID string) *Device {
	if deviceID == "" {
		return nil
	}
	device, ok := s.Devices.Load(deviceID)
	if ok {
		device, ok := device.(*Device)
		if ok {
			return device
		}
	}
	return nil
}

func (s *ServiceInstance) addDevice(device *Device) {
	s.Devices.Store(device.DeviceID, device)
	ctx := context.Background()
	_, err := globalRedis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.SAdd(ctx, fmt.Sprintf("%s%s", KEY_INSTANCE_DEVICE_SET_PREFIX, s.Address), device.DeviceID)
		pipe.HIncrBy(ctx, fmt.Sprintf("%s%s", KEY_INSTANCE_PREFIX, s.Address), "device_count", 1)
		return nil
	})
	if err != nil {
		log.Fatalf("Failed to add device: %v\n", err)
	}
}

func (s *ServiceInstance) delDevice(device *Device) {
	s.Devices.Delete(device.DeviceID)
	ctx := context.Background()
	_, err := globalRedis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.SRem(ctx, fmt.Sprintf("%s%s", KEY_INSTANCE_DEVICE_SET_PREFIX, s.Address), device.DeviceID)
		pipe.HIncrBy(ctx, fmt.Sprintf("%s%s", KEY_INSTANCE_PREFIX, s.Address), "device_count", -1)
		return nil
	})
	if err != nil {
		log.Fatalf("Failed to remove device: %v\n", err)
	}
}
