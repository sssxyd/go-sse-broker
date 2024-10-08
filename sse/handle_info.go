package sse

import (
	"log"
	"net/http"
	"sse-broker/funcs"
	"time"

	"github.com/gin-gonic/gin"
)

type InfoParams struct {
	UID     string `json:"uid" form:"uid"`
	Device  string `json:"device" form:"device"`
	Address string `json:"address" form:"address"`
}

func (p *InfoParams) getTarget() (string, string) {
	if p.Device != "" {
		return "device", p.Device
	} else if p.UID != "" {
		return "user", p.UID
	} else if p.Address != "" {
		return "instance", p.Address
	} else {
		return "cluster", ""
	}
}

func getDeviceInfo(deviceId string, deviceName string) DeviceInfo {
	device := globalInstance.getDevice(deviceId)
	if device == nil {
		device = getRedisDevice(deviceId)
	}
	if device == nil {
		return DeviceInfo{
			Online: false,
			Device: Device{
				DeviceID:        deviceId,
				DeviceName:      deviceName,
				UID:             "",
				LoginTime:       "",
				InstanceAddress: "",
				DeviceAddress:   "",
				LastTouchTime:   "",
				LastFrameId:     0,
			},
		}
	} else {
		return DeviceInfo{
			Online: device.exist(),
			Device: *device,
		}
	}
}

func getUserInfo(uid string) UserInfo {
	user := NewUser(uid)
	deviceIds := user.getDeviceIds()
	if len(deviceIds) == 0 {
		return UserInfo{
			Online:        false,
			UID:           uid,
			LonginTime:    "",
			LastTouchTime: "",
			Devices:       []Device{},
		}
	}
	devices := make([]Device, 0, len(deviceIds))
	var firstLoginTime time.Time
	var lastTouchTime time.Time
	for _, deviceId := range deviceIds {
		device := globalInstance.getDevice(deviceId)
		if device == nil {
			device = getRedisDevice(deviceId)
		}
		if device != nil {
			devices = append(devices, *device)
			if device.LoginTime != "" {
				loginTime, _ := time.Parse("2006-01-02 15:04:05", device.LoginTime)
				if firstLoginTime.IsZero() || loginTime.Before(firstLoginTime) {
					firstLoginTime = loginTime
				}
			}
			if device.LastTouchTime != "" {
				touchTime, _ := time.Parse("2006-01-02 15:04:05", device.LastTouchTime)
				if lastTouchTime.IsZero() || touchTime.After(lastTouchTime) {
					lastTouchTime = touchTime
				}
			}
		}
	}
	return UserInfo{
		Online:        true,
		UID:           uid,
		LonginTime:    firstLoginTime.Format("2006-01-02 15:04:05"),
		LastTouchTime: lastTouchTime.Format("2006-01-02 15:04:05"),
		Devices:       devices,
	}
}

func getInstanceInfo(address string) InstanceInfo {
	instance := getRedisInstance(address)
	if instance == nil {
		return InstanceInfo{
			Online: false,
			AbstractInstance: AbstractInstance{
				Version:     "",
				Address:     address,
				StartTime:   "",
				DeviceCount: 0,
			},
		}
	}
	return InstanceInfo{
		Online:           instance.exist(),
		AbstractInstance: *instance,
	}
}

func getClusterInfo() ClusterInfo {
	instanceAddresses, err := globalRedis.SMembers(KEY_CLUSTER_INSTANCE_SET)
	if err != nil {
		log.Println("Failed to get instance addresses:", err)
	}
	if len(instanceAddresses) == 0 {
		return ClusterInfo{
			InstanceCount: 0,
			UserCount:     0,
			DeviceCount:   0,
			Instances:     []InstanceInfo{},
		}
	}
	deviceCount := 0
	instances := make([]InstanceInfo, 0, len(instanceAddresses))
	for _, address := range instanceAddresses {
		instance := getInstanceInfo(address)
		if instance.Online {
			deviceCount += instance.DeviceCount
		}
		instances = append(instances, instance)
	}
	userCount := 0
	cnt, err := globalRedis.SCard(KEY_ONLINE_USER_SET)
	if err != nil {
		log.Println("Failed to get online user count:", err)
		userCount = 0
	} else {
		userCount = int(cnt)
	}
	return ClusterInfo{
		InstanceCount: len(instanceAddresses),
		UserCount:     userCount,
		DeviceCount:   deviceCount,
		Instances:     instances,
	}
}

func HandleInfo(c *gin.Context) {
	startRequest(c)
	var params InfoParams
	err := fillParams(c, &params)
	if err != nil {
		log.Fatalln(err)
		return
	}
	var info interface{}
	switch target, id := params.getTarget(); target {
	case "device":
		info = getDeviceInfo(funcs.MD5(id), id)
	case "user":
		info = getUserInfo(id)
	case "instance":
		info = getInstanceInfo(id)
	case "cluster":
		info = getClusterInfo()
	default:
		info = gin.H{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":   1,
		"msg":    "success",
		"result": info,
		"micro":  endRequest(c),
	})
}
