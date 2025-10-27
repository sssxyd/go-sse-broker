package sse

import (
	"log/slog"
	"net/http"

	"github.com/gofiber/fiber/v2"
)

type KickParams struct {
	UID    string `json:"uid" form:"uid"`
	Device string `json:"device" form:"device"`
	Data   string `json:"data" form:"data"`
}

func HandleKick(c *fiber.Ctx) error {
	startRequest(c)
	var params KickParams
	err := fillParams(c, &params)
	if err != nil {
		slog.Error(err.Error())
		return nil
	}
	if params.UID == "" && params.Device == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"code":   http.StatusBadRequest,
			"msg":    "uid and device cannot be empty at the same time",
			"result": "",
			"micros": endRequest(c),
		})
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
	return c.Status(http.StatusOK).JSON(fiber.Map{
		"code":   0,
		"msg":    "success",
		"result": count,
		"micros": endRequest(c),
	})
}
