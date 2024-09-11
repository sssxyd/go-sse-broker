package sse

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type KickParams struct {
	UID    string `json:"uid" form:"uid"`
	Device string `json:"device" form:"device"`
	Data   string `json:"data" form:"data"`
}

func HandleKick(c *gin.Context) {
	startRequest(c)
	var params KickParams
	err := fillParams(c, &params)
	if err != nil {
		log.Fatalln(err)
		return
	}
	if params.UID == "" && params.Device == "" {
		c.JSON(http.StatusOK, gin.H{
			"code":   http.StatusBadRequest,
			"msg":    "uid and device cannot be empty at the same time",
			"result": "",
			"micro":  endRequest(c),
		})
		return
	}
	count := 0
	deviceIds := collectDeviceIds(params.UID, params.Device)
	for _, deviceId := range deviceIds {
		if deviceId == "" {
			continue
		}
		device := globalInstance.getDevice(deviceId)
		if device != nil {
			globalInstance.handleInstruction(&Instruction{
				DeviceID: deviceId,
				Command:  CMD_KICK_OFFLINE,
				Data:     params.Data,
			})
			count++
		} else {
			device = getRedisDevice(deviceId)
			if device != nil {
				DispatchInstruction(device.InstanceAddress, Instruction{
					DeviceID: deviceId,
					Command:  CMD_KICK_OFFLINE,
					Data:     params.Data,
				})
				count++
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":   1,
		"msg":    "success",
		"result": count,
		"micro":  endRequest(c),
	})
}
