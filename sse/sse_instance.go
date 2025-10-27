package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type AbstractInstance struct {
	Version     string `json:"version"`
	Address     string `json:"address"`
	StartTime   string `json:"start_time"`
	DeviceCount int    `json:"device_count"`
}

func (s *AbstractInstance) String() string {
	json, err := json.Marshal(s)
	if err != nil {
		json = []byte("{}")
	}
	return string(json)
}

func (s *AbstractInstance) exist() bool {
	return s.Version != ""
}

func getRedisInstance(address string) *AbstractInstance {
	info, err := globalRedis.HGetAll(fmt.Sprintf("%s%s", KEY_INSTANCE_PREFIX, address))
	if err != nil {
		slog.Error("Failed to get instance", "error", err)
		return nil
	}
	if len(info) == 0 {
		return nil
	}
	instacne := &AbstractInstance{
		Version:   info["version"],
		Address:   address,
		StartTime: info["start_time"],
		DeviceCount: func() int {
			id, err := strconv.ParseInt(info["device_count"], 10, 64)
			if err != nil {
				slog.Error("Failed to parse last frame id", "error", err)
				return 0
			}
			return int(id)
		}(),
	}
	return instacne
}

type ServiceInstance struct {
	AbstractInstance
	Devices     sync.Map
	TopicCancel context.CancelFunc
}

func subscribeInstanceTopic(ctx context.Context, topic string) {
	err := globalRedis.Subscribe(ctx, func(channel string, payload string) {
		var instruction Instruction
		err := json.Unmarshal([]byte(payload), &instruction)
		if err != nil {
			slog.Error("Failed to unmarshal instruction", "error", err)
			return
		}
		globalInstance.handleInstruction(&instruction)
	}, topic)
	if err != nil {
		slog.Error("Failed to subscribe instance topic", "error", err)
		panic(fmt.Sprintf("Failed to subscribe instance topic: %v\n", err))
	}
}

func NewServiceInstance(version string, address string) *ServiceInstance {
	instacne := &ServiceInstance{
		AbstractInstance: AbstractInstance{
			Version:     version,
			Address:     address,
			StartTime:   time.Now().Format("2006-01-02 15:04:05"),
			DeviceCount: 0,
		},
		Devices: sync.Map{},
	}
	return instacne
}

// 处理发给本实例的指令
func (s *ServiceInstance) handleInstruction(instruction *Instruction) {
	if channel, ok := deviceChannels.Load(instruction.DeviceID); ok {
		inschannel, ok := channel.(chan *Instruction)
		if !ok {
			slog.Warn("Device not found", "device", instruction.DeviceID, "address", s.Address)
		} else {
			inschannel <- instruction
		}
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
		slog.Error("Failed to clear instance", "error", err)
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

	// 本实例上线，并添加到实例集合
	ctx := context.Background()
	_, err := globalRedis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, fmt.Sprintf("%s%s", KEY_INSTANCE_PREFIX, s.Address),
			"version", s.Version,
			"address", s.Address,
			"start_time", s.StartTime,
			"device_count", 0)
		pipe.SAdd(ctx, KEY_CLUSTER_INSTANCE_SET, s.Address)
		return nil
	})

	if err != nil {
		slog.Error("Failed to start instance", "error", err)
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.TopicCancel = cancel
	// 订阅实例Topic
	go subscribeInstanceTopic(ctx, fmt.Sprintf("%s%s", TOPIC_INSTANCE_PREFIX, s.Address))

	// 启动全局在线用户清理器（定期清理僵尸用户）
	go s.startOnlineUserCleaner(ctx)

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
	slog.Info("Instance stopped", "address", s.Address)
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
		slog.Error("Failed to dispose instance", "error", err)
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
		slog.Error("Failed to add device", "error", err)
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
		slog.Error("Failed to remove device", "error", err)
	}
}

// startOnlineUserCleaner 启动全局在线用户清理器
// 定期扫描 KEY_ONLINE_USER_SET，清理实际已下线但未正确移除的僵尸用户
func (s *ServiceInstance) startOnlineUserCleaner(ctx context.Context) {
	// 每 10 分钟执行一次清理（降低 Redis 压力）
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	slog.Info("Online user cleaner started", "interval", "10m")

	for {
		select {
		case <-ctx.Done():
			slog.Info("Online user cleaner stopped")
			return
		case <-ticker.C:
			s.cleanZombieUsers()
		}
	}
}

// cleanZombieUsers 清理僵尸用户
func (s *ServiceInstance) cleanZombieUsers() {

	// 使用 Redis 分布式锁，确保只有一个实例执行清理
	lockKey := "lock:online_user_cleaner"
	lockValue := s.Address     // 使用实例地址作为锁值
	lockTTL := 5 * time.Minute // 锁过期时间（防止死锁）

	// 尝试获取锁（SET NX EX）
	acquired, err := globalRedis.SetNX(lockKey, lockValue, lockTTL)
	if err != nil {
		slog.Error("Failed to acquire cleaner lock", "error", err)
		return
	}

	if !acquired {
		// 其他实例正在清理，跳过
		slog.Debug("Another instance is cleaning zombie users, skipping")
		return
	}

	// 确保释放锁
	defer func() {
		//只删除自己持有的锁（避免误删其他实例的锁）
		currentValue, _ := globalRedis.Get(lockKey)
		if currentValue == lockValue {
			globalRedis.Del(lockKey)
		}
	}()

	slog.Info("Starting zombie user cleanup", "instance", s.Address)

	// 获取所有在线用户
	onlineUsers, err := globalRedis.SMembers(KEY_ONLINE_USER_SET)
	if err != nil {
		slog.Error("Failed to get online users", "error", err)
		return
	}

	if len(onlineUsers) == 0 {
		return
	}

	slog.Debug("Checking online users", "count", len(onlineUsers))

	var zombieUsers []string
	var zombieUserKeys []string
	var validCount int

	for _, uid := range onlineUsers {
		if uid == "" {
			continue
		}

		// 检查用户的设备集合是否存在且非空
		userDeviceSetKey := fmt.Sprintf("%s%s", KEY_USER_DEVICE_SET_PREFIX, uid)

		// 获取用户的所有设备 ID
		deviceIds, err := globalRedis.SMembers(userDeviceSetKey)
		if err != nil {
			slog.Warn("Failed to get user device set", "uid", uid, "error", err)
			continue
		}

		// 验证设备是否真实存在
		user := NewUser(uid)
		validDeviceIds := user.validateDeviceSet(deviceIds)

		if len(validDeviceIds) == 0 {
			// 没有有效设备，标记为僵尸用户
			zombieUsers = append(zombieUsers, uid)
			zombieUserKeys = append(zombieUserKeys, userDeviceSetKey)

			// 发送用户离线事件
			DispatchUserOffline(StateChange{
				Event:       UserOffline,
				UID:         uid,
				Device:      "ZombieCleaner",
				EventTimeMs: time.Now().UnixMilli(),
				Reason:      DCR_ZOMBIE_CLEANUP,
				Payload:     s.Address,
			})
		} else {
			validCount++
		}
	}

	// 批量移除僵尸用户
	if len(zombieUsers) > 0 {
		removed, err := globalRedis.SRem(KEY_ONLINE_USER_SET, zombieUsers)
		if err != nil {
			slog.Error("Failed to remove zombie users", "error", err)
		} else {
			slog.Warn("Cleaned zombie users from online set",
				"zombie_count", removed,
				"valid_count", validCount,
				"total_checked", len(onlineUsers))
		}
		// 同时清理这些用户的设备集合
		err = globalRedis.Del(zombieUserKeys...)
		if err != nil {
			slog.Error("Failed to delete zombie user device sets", "error", err)
		}
	} else {
		slog.Debug("No zombie users found", "valid_count", validCount)
	}
}
