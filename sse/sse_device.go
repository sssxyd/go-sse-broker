package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type Device struct {
	DeviceID        string `json:"device_id"`
	UID             string `json:"uid"`
	LoginTime       string `json:"login_time"`
	InstanceAddress string `json:"instance_address"`
	DeviceAddress   string `json:"device_address"`
	LastTouchTime   string `json:"last_touch_time"`
	LastFrameId     int64  `json:"last_frame_id"`
}

func getRedisDevice(deviceID string) *Device {
	if deviceID == "" {
		return nil
	}
	deviceKey := fmt.Sprintf("%s%s", KEY_DEVICE_PREFIX, deviceID)
	info, err := globalRedis.HGetAll(deviceKey)
	if err != nil {
		return nil
	}
	device := &Device{
		DeviceID:        info["device_id"],
		UID:             info["uid"],
		LoginTime:       info["login_time"],
		InstanceAddress: info["instance_address"],
		DeviceAddress:   info["device_address"],
		LastTouchTime:   info["last_touch_time"],
		LastFrameId: func() int64 {
			id, err := strconv.ParseInt(info["last_frame_id"], 10, 64)
			if err != nil {
				return 0
			}
			return id
		}(),
	}
	return device
}

func NewDevice(deviceID, uid, instanceAddress, deviceAddress string) *Device {
	device := &Device{
		DeviceID:        deviceID,
		UID:             uid,
		LoginTime:       time.Now().Format("2006-01-02 15:04:05"),
		InstanceAddress: instanceAddress,
		DeviceAddress:   deviceAddress,
		LastTouchTime:   time.Now().Format("2006-01-02 15:04:05"),
	}
	ctx := context.Background()
	maxOne, err := globalRedis.Client().ZRevRangeWithScores(ctx, fmt.Sprintf("%s%s", KEY_DEVICE_CACHE_PREFIX, deviceID), 0, 0).Result()
	if err != nil || len(maxOne) == 0 {
		device.LastFrameId = 0
	} else {
		device.LastFrameId = int64(maxOne[0].Score)
	}

	_, err = globalRedis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, fmt.Sprintf("%s%s", KEY_DEVICE_PREFIX, deviceID),
			"uid", device.UID,
			"login_time", device.LoginTime,
			"instance_address", device.InstanceAddress,
			"device_address", device.DeviceAddress,
			"last_touch_time", device.LastTouchTime,
			"last_frame_id", device.LastFrameId,
		)
		pipe.Expire(ctx, fmt.Sprintf("%s%s", KEY_DEVICE_PREFIX, deviceID), globalConfig.SSE.DeviceUserExistDuration)
		return nil
	})
	if err != nil {
		log.Printf("Failed to create device: %v\n", err)
		return nil
	}
	return device
}

func (d *Device) isRemote() bool {
	return d.InstanceAddress != globalInstance.Address
}

func (d *Device) touch() {
	d.LastTouchTime = time.Now().Format("2006-01-02 15:04:05")
	ctx := context.Background()
	_, err := globalRedis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, fmt.Sprintf("%s%s", KEY_DEVICE_PREFIX, d.DeviceID), "last_touch_time", d.LastTouchTime)
		pipe.Expire(ctx, fmt.Sprintf("%s%s", KEY_DEVICE_PREFIX, d.DeviceID), globalConfig.SSE.DeviceUserExistDuration)
		return nil
	})
	if err != nil {
		log.Printf("Failed to touch device: %v\n", err)
	}
}

// 当前设备主动上线
func (d *Device) online() {
	globalInstance.addDevice(d)
	DispatchDeviceOnline(StateChange{
		DeviceID:    d.DeviceID,
		UID:         d.UID,
		TriggerTime: time.Now().Format("2006-01-02 15:04:05"),
		Reason:      DCR_DEVICE_ONLINE,
		Payload:     globalInstance.Address,
	})
}

func (d *Device) offline(reason string, payload string) {
	// 关闭设备的指令通道
	if channel, ok := deviceChannels.LoadAndDelete(d.DeviceID); ok {
		inschan, ok := channel.(chan Instruction)
		if ok {
			log.Printf("Close channel for device %s\n", d.DeviceID)
			close(inschan)
		}
	}
	// 删除本地实例中的设备
	globalInstance.delDevice(d)

	DispatchDeviceOffline(StateChange{
		DeviceID:    d.DeviceID,
		UID:         d.UID,
		TriggerTime: time.Now().Format("2006-01-02 15:04:05"),
		Reason:      reason,
		Payload:     payload,
	})

}

func (d *Device) delFrameCache() {
	globalRedis.Del(fmt.Sprintf("%s%s", KEY_DEVICE_CACHE_PREFIX, d.DeviceID))
}

func (d *Device) getCachedFrames(lastEventID int64) []Frame {
	var frames []Frame
	results, err := globalRedis.ZRangeByScore(fmt.Sprintf("%s%s", KEY_DEVICE_CACHE_PREFIX, d.DeviceID), fmt.Sprintf("%f", float64(lastEventID+1)), "+inf")
	if err != nil {
		return frames
	}
	for _, result := range results {
		frame := Frame{}
		json.Unmarshal([]byte(result), &frame)
		frames = append(frames, frame)
	}
	return frames
}

func (d *Device) addFrame(event string, data string) Frame {
	frameId, err := globalRedis.HIncrBy(fmt.Sprintf("%s%s", KEY_DEVICE_PREFIX, d.DeviceID), "last_frame_id", 1)
	if err != nil {
		log.Printf("Failed to get next frame id: %v\n", err)
		return Frame{
			ID:    d.LastFrameId + 1,
			Event: event,
			Data:  data,
		}
	}
	d.LastFrameId = frameId
	frame := Frame{
		ID:    frameId,
		Event: event,
		Data:  data,
	}
	ctx := context.Background()
	stop := -int64(globalConfig.SSE.DeviceFrameCacheSize + 1)
	cacheKey := fmt.Sprintf("%s%s", KEY_DEVICE_CACHE_PREFIX, d.DeviceID)
	_, err = globalRedis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.ZAdd(ctx, cacheKey, redis.Z{Score: float64(frame.ID), Member: frame.String()})
		pipe.ZRemRangeByRank(ctx, cacheKey, 0, stop)
		pipe.Expire(ctx, cacheKey, globalConfig.SSE.DeviceFrameExpireDuration)
		return nil
	})
	if err != nil {
		log.Printf("Failed to cache frame: %v\n", err)
	}
	return frame
}
