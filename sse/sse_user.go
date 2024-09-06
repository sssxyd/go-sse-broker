package sse

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

type User struct {
	UID string `json:"uid"`
}

func NewUser(uid string) *User {
	return &User{
		UID: uid,
	}
}

func (u *User) touch() {
	globalRedis.Expire(fmt.Sprintf("%s%s", KEY_USER_DEVICE_SET_PREFIX, u.UID), globalConfig.SSE.DeviceUserExistDuration)
}

func (u *User) validateDeviceSet(deviceIds []string) []string {
	if len(deviceIds) == 0 {
		return []string{}
	}
	var deviceKeys []string
	for _, deviceId := range deviceIds {
		if deviceId == "" {
			continue
		}
		deviceKeys = append(deviceKeys, fmt.Sprintf("%s%s", KEY_DEVICE_PREFIX, deviceId))
	}
	if len(deviceKeys) == 0 {
		return []string{}
	}
	ctx := context.Background()
	pipe := globalRedis.Pipeline()
	cmds := make([]*redis.IntCmd, len(deviceKeys))
	for i, key := range deviceKeys {
		cmds[i] = pipe.Exists(ctx, key)
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		log.Println("Pipeline error:", err)
		return []string{}
	}
	var validDeviceIds []string
	var invalidDeviceIds []string
	for i, cmd := range cmds {
		exists, err := cmd.Result()
		if err != nil {
			log.Printf("Error checking device existence: %v\n", err)
			continue
		}
		if exists == 1 {
			validDeviceIds = append(validDeviceIds, deviceIds[i])
		} else {
			invalidDeviceIds = append(invalidDeviceIds, deviceIds[i])
		}
	}
	if len(invalidDeviceIds) > 0 {
		// 清理无效设备
		globalRedis.SRem(fmt.Sprintf("%s%s", KEY_USER_DEVICE_SET_PREFIX, u.UID), invalidDeviceIds)
	}
	return validDeviceIds
}

func (u *User) handleDeviceOnline(device *Device) {
	userDeviceSetKey := fmt.Sprintf("%s%s", KEY_USER_DEVICE_SET_PREFIX, u.UID)

	ctx := context.Background()
	_cmds, err := globalRedis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.SMembers(ctx, userDeviceSetKey)
		pipe.SAdd(ctx, userDeviceSetKey, device.DeviceID)
		pipe.Expire(ctx, userDeviceSetKey, globalConfig.SSE.DeviceUserExistDuration)
		return nil
	})
	if err != nil {
		log.Printf("Failed to handle user device online: %v\n", err)
		return
	}
	deviceIds, err := _cmds[0].(*redis.StringSliceCmd).Result()
	if err != nil {
		log.Printf("Failed to get user device set: %v\n", err)
		return
	}
	deviceIds = u.validateDeviceSet(deviceIds)
	if len(deviceIds) == 0 {
		// 第一个设备上线
		u.online(device.DeviceID, DCR_CONNECTED, device.DeviceAddress)
	}
}

func (u *User) online(deviceId string, reason string, payload string) {
	ctx := context.Background()
	userDeviceSetKey := fmt.Sprintf("%s%s", KEY_USER_DEVICE_SET_PREFIX, u.UID)
	globalRedis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.SAdd(ctx, userDeviceSetKey, deviceId)
		pipe.Expire(ctx, userDeviceSetKey, globalConfig.SSE.DeviceUserExistDuration)
		pipe.SAdd(ctx, KEY_ONLINE_USER_SET, u.UID)
		return nil
	})
	DispatchUserOnline(StateChange{
		UID:         u.UID,
		DeviceID:    deviceId,
		TriggerTime: time.Now().Format("2006-01-02 15:04:05"),
		Reason:      reason,
		Payload:     payload,
	})
}

func (u *User) handleDeviceOffline(device *Device) {
	userDeviceSetKey := fmt.Sprintf("%s%s", KEY_USER_DEVICE_SET_PREFIX, u.UID)

	ctx := context.Background()
	_cmds, err := globalRedis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.SRem(ctx, userDeviceSetKey, device.DeviceID)
		pipe.SMembers(ctx, userDeviceSetKey)
		pipe.Expire(ctx, userDeviceSetKey, globalConfig.SSE.DeviceUserExistDuration)
		return nil
	})
	if err != nil {
		log.Printf("Failed to handle user device offline: %v\n", err)
		return
	}
	deviceIds, err := _cmds[1].(*redis.StringSliceCmd).Result()
	if err != nil {
		log.Printf("Failed to get user device set: %v\n", err)
		return
	}
	deviceIds = u.validateDeviceSet(deviceIds)
	if len(deviceIds) == 0 {
		// 最后一个设备下线
		u.offline(device.DeviceID, DCR_DISCONNECT, device.DeviceAddress)
	}
}

func (u *User) offline(deviceId string, reason string, payload string) {
	ctx := context.Background()
	_, err := globalRedis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, fmt.Sprintf("%s%s", KEY_USER_DEVICE_SET_PREFIX, u.UID))
		pipe.SRem(ctx, KEY_ONLINE_USER_SET, u.UID)
		return nil
	})
	if err != nil {
		log.Printf("Failed to offline user: %v\n", err)
	}
	DispatchUserOffline(StateChange{
		UID:         u.UID,
		DeviceID:    deviceId,
		TriggerTime: time.Now().Format("2006-01-02 15:04:05"),
		Reason:      reason,
		Payload:     payload,
	})
}
