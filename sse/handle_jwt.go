package sse

import (
	"sse-broker/funcs"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt"
)

type Claims struct {
	UID        string `json:"uid"`
	DeviceName string `json:"device_name"`
	jwt.StandardClaims
}

var jwtSecret []byte
var jwtExpire int

// Init 初始化JWT
func jwtInit(secret string, expire int) {
	jwtSecret = []byte(secret)
	jwtExpire = expire
}

// Middleware 处理JWT鉴权
func TokenCheck() fiber.Handler {
	return func(c *fiber.Ctx) error {
		tokenString := c.Query("token", "")
		if tokenString == "" {
			tokenString = c.Get("X-SSE-Token")
		}
		deviceName := c.Query("device", "")
		if deviceName == "" {
			deviceName = c.Get("X-SSE-Device")
		}
		lastEventID := c.Query("id", "")
		if lastEventID == "" {
			lastEventID = c.Get("X-SSE-ID")
		}
		lastId, err := strconv.Atoi(lastEventID)
		if err != nil {
			lastId = 0
		}
		if tokenString == "" || deviceName == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "token and device is required"})
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})

		if deviceName != "" && claims.DeviceName != "" && claims.DeviceName != deviceName {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"code":   401,
				"msg":    "Invalid device",
				"result": "",
			})
		}

		if err != nil || !token.Valid {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid token"})
		}

		// Fiber上下文存储
		c.Locals("_uid", claims.UID)
		c.Locals("_device_name", deviceName)
		c.Locals("_device_id", funcs.MD5(deviceName))
		c.Locals("_last_event_id", lastId)
		return c.Next()
	}
}
