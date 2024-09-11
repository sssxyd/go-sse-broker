package sse

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sse-broker/funcs"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type SendFrameParams struct {
	UID    string `json:"uid" form:"uid"`
	Device string `json:"device" form:"device"`
	Event  string `json:"event" form:"event"`
	Data   string `json:"data" form:"data"`
}

func collectDeviceIds(uid_str string, device_name_str string) []string {
	uids := strings.Split(uid_str, ",")
	deviceNames := strings.Split(device_name_str, ",")
	targetDeviceSet := make(map[string]bool)
	for _, deviceName := range deviceNames {
		if deviceName == "" {
			continue
		}
		// 对设备ID进行MD5哈希
		deviceId := funcs.MD5(deviceName)
		targetDeviceSet[deviceId] = true
	}
	var userDeviceSetKeys []string
	for _, uid := range uids {
		if uid == "" {
			continue
		}
		userDeviceSetKeys = append(userDeviceSetKeys, fmt.Sprintf("%s%s", KEY_USER_DEVICE_SET_PREFIX, uid))
	}
	if len(userDeviceSetKeys) > 0 {
		ctx := context.Background()
		pipe := globalRedis.Pipeline()
		cmds := make([]*redis.StringSliceCmd, len(userDeviceSetKeys))
		for i, key := range userDeviceSetKeys {
			cmds[i] = pipe.SMembers(ctx, key)
		}
		_, err := pipe.Exec(ctx)
		if err != nil && err != redis.Nil {
			log.Println("Pipeline error:", err)
		} else {
			for i, cmd := range cmds {
				members, err := cmd.Result()
				if err == redis.Nil {
					log.Printf("User %s has no device\n", uids[i])
					continue
				} else if err != nil {
					log.Printf("Error retrieving members from set %s: %v\n", userDeviceSetKeys[i], err)
					continue
				}
				for _, deviceId := range members {
					targetDeviceSet[deviceId] = true
				}
			}
		}
	}
	var keys []string

	for key := range targetDeviceSet {
		keys = append(keys, key)
	}
	return keys
}

func getAllDeviceIds() []string {
	instance_addresses, err := globalRedis.SMembers(KEY_INSTANCE_SET)
	if err != nil || len(instance_addresses) == 0 {
		return []string{}
	}
	ctx := context.Background()
	pipe := globalRedis.Pipeline()

	cmds := make([]*redis.StringSliceCmd, len(instance_addresses))
	for i, address := range instance_addresses {
		cmds[i] = pipe.SMembers(ctx, fmt.Sprintf("%s%s", KEY_INSTANCE_DEVICE_SET_PREFIX, address))
	}
	_, err = pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		log.Println("Pipeline error:", err)
		return []string{}
	}
	var deviceIds []string
	for i, cmd := range cmds {
		members, err := cmd.Result()
		if err == redis.Nil {
			log.Printf("Instance %s has no device\n", instance_addresses[i])
			continue
		} else if err != nil {
			log.Printf("Error retrieving members from set: %v\n", err)
			continue
		}
		deviceIds = append(deviceIds, members...)
	}
	return deviceIds
}

// 分段处理设备ID，批量获取实例地址
func splitDeviceWithInstanceAddressBatch(deviceIds []string, instanceDeviceMap *sync.Map, wg *sync.WaitGroup) {
	defer wg.Done() // 标志当前 goroutine 结束

	ctx := context.Background()
	pipe := globalRedis.Pipeline()

	cmds := make([]*redis.StringCmd, len(deviceIds))
	for i, deviceId := range deviceIds {
		cmds[i] = pipe.HGet(ctx, fmt.Sprintf("%s%s", KEY_DEVICE_PREFIX, deviceId), "instance_address")
	}

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		log.Println("Pipeline error:", err)
		return
	}

	for i, cmd := range cmds {
		address, err := cmd.Result()
		if err == redis.Nil {
			log.Printf("Device %s not found\n", deviceIds[i])
			continue
		} else if err != nil {
			log.Printf("Error retrieving instance address for device %s: %v\n", deviceIds[i], err)
			continue
		}

		if address != "" {
			// 使用 sync.Map 保证并发写安全
			val, _ := instanceDeviceMap.LoadOrStore(address, []string{})
			// 这里的 val 是 interface{} 类型，强制转换为 []string
			instanceDeviceMap.Store(address, append(val.([]string), deviceIds[i]))
		}
	}
}

// 分段并发处理设备ID
func splitDeviceWithInstanceAddress(deviceIds []string) map[string][]string {
	instanceDeviceMap := &sync.Map{}
	batchSize := 250      // 每批处理 250 个设备ID
	var wg sync.WaitGroup // 用于等待所有 goroutines 完成

	// 遍历并分批处理
	for i := 0; i < len(deviceIds); i += batchSize {
		end := i + batchSize
		if end > len(deviceIds) {
			end = len(deviceIds)
		}

		wg.Add(1) // 增加等待计数
		go splitDeviceWithInstanceAddressBatch(deviceIds[i:end], instanceDeviceMap, &wg)
	}

	wg.Wait() // 等待所有的 goroutines 完成

	resultMap := make(map[string][]string)
	instanceDeviceMap.Range(func(key, value interface{}) bool {
		// 强制类型转换，假设 key 是 string，value 是 []string
		strKey, ok1 := key.(string)
		strValue, ok2 := value.([]string)
		if ok1 && ok2 {
			resultMap[strKey] = strValue
		}
		return true
	})

	return resultMap
}

func HandleSend(c *gin.Context) {
	startRequest(c)
	var params SendFrameParams
	if err := fillParams(c, &params); err != nil {
		log.Fatalln(err)
		return
	}
	if params.Data == "" {
		c.JSON(http.StatusOK, gin.H{
			"code":   http.StatusBadRequest,
			"msg":    "data cannot be empty",
			"result": "",
			"micro":  endRequest(c),
		})
		return
	}
	sendAll := params.UID == "" && params.Device == ""
	var deviceIds []string
	if sendAll {
		deviceIds = getAllDeviceIds()
	} else {
		deviceIds = collectDeviceIds(params.UID, params.Device)
	}

	total := len(deviceIds)

	var remoteDeviceIds []string
	for _, deviceId := range deviceIds {
		if deviceId == "" {
			continue
		}
		device := globalInstance.getDevice(deviceId)
		if device != nil {
			globalInstance.handleInstruction(&Instruction{
				DeviceID: deviceId,
				Command:  CMD_SEND_FRAME,
				Data:     params.Data,
				Event:    params.Event,
			})
		} else {
			remoteDeviceIds = append(remoteDeviceIds, deviceId)
		}
	}
	if len(remoteDeviceIds) > 0 {
		instanceDeviceMap := splitDeviceWithInstanceAddress(remoteDeviceIds)
		for address, ids := range instanceDeviceMap {
			go func() {
				instractions := make([]Instruction, len(ids))
				for _, deviceId := range ids {
					instractions = append(instractions, Instruction{
						DeviceID: deviceId,
						Command:  CMD_SEND_FRAME,
						Data:     params.Data,
						Event:    params.Event,
					})
				}
				DispatchInstructions(address, instractions)
			}()
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":   1,
		"msg":    "success",
		"result": total,
		"micro":  endRequest(c),
	})
}
