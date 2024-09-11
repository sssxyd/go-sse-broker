package sse

import (
	"fmt"
	"log"
	"net/http"
	"sse-broker/funcs"
	"time"

	"github.com/gin-gonic/gin"
)

type InfoParams struct {
	UID    string `json:"uid" form:"uid"`
	Device string `json:"device" form:"device"`
	IP     string `json:"ip" form:"ip"`
}

func (p *InfoParams) getTarget() (string, string) {
	if p.Device != "" {
		return "device", p.Device
	} else if p.UID != "" {
		return "user", p.UID
	} else if p.IP != "" {
		return "instance", p.IP
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
			Online: true,
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

func getInstanceInfo(ip string) InstanceInfo {
	online, err := globalRedis.SIsMember(fmt.Sprintf("%s%s", KEY_INSTANCE_SET, globalInstance.Address), ip)
	if err != nil {
		log.Println("Failed to check instance online status:", err)
		online = false
	}
	deviceCount := 0
	if online {
		cnt, err := globalRedis.SCard(fmt.Sprintf("%s%s", KEY_INSTANCE_DEVICE_SET_PREFIX, ip))
		if err != nil {
			log.Println("Failed to get device count:", err)
			deviceCount = 0
		} else {
			deviceCount = int(cnt)
		}
	}
	return InstanceInfo{
		Online:      online,
		Address:     ip,
		DeviceCount: deviceCount,
	}
}

func getClusterInfo() ClusterInfo {
	instanceAddresses, err := globalRedis.SMembers(KEY_INSTANCE_SET)
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
