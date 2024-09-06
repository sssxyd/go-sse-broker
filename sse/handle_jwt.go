package sse

import (
	"net/http"
	"sse_broker/funcs"
	"strconv"

	"github.com/gin-gonic/gin"
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
func TokenCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := c.DefaultQuery("token", "")
		if tokenString == "" {
			tokenString = c.GetHeader("X-SSE-Token")
		}
		deviceName := c.DefaultQuery("device", "")
		if deviceName == "" {
			deviceName = c.GetHeader("X-SSE-Device")
		}
		lastEventID := c.DefaultQuery("id", "")
		if lastEventID == "" {
			lastEventID = c.GetHeader("X-SSE-ID")
		}
		lastId, err := strconv.Atoi(lastEventID)
		if err != nil {
			lastId = 0
		}
		if tokenString == "" || deviceName == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "token and device is required"})
			c.Abort()
			return
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})

		if deviceName != "" && claims.DeviceName != "" && claims.DeviceName != deviceName {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":   401,
				"msg":    "Invalid device",
				"result": "",
			})
			c.Abort()
			return
		}

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		// 将userId保存到上下文中
		c.Set("_uid", claims.UID)
		// 对deviceId进行MD5，防止乱写
		c.Set("_device_name", deviceName)
		c.Set("_device_id", funcs.MD5(deviceName))
		// 将lastEventID保存到上下文中
		c.Set("_last_event_id", lastId)
		c.Next()
	}
}
