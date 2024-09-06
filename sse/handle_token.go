package sse

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt"
)

type TokenParams struct {
	UID      string `json:"uid" form:"uid"`
	DeviceID string `json:"device_id" form:"device_id"`
	TTL      int    `json:"ttl" form:"ttl"`
}

// CreateToken 生成JWT
func HandleToken(c *gin.Context) {
	startRequest(c)
	var params TokenParams
	if err := fillParams(c, &params); err != nil {
		log.Fatalln(err)
		return
	}
	if params.TTL <= 0 {
		params.TTL = jwtExpire
	}

	duration := time.Duration(params.TTL) * time.Second
	claims := &Claims{
		UID:      params.UID,
		DeviceID: params.DeviceID,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: time.Now().Add(duration).Unix(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code":   http.StatusInternalServerError,
			"msg":    fmt.Sprintf("Failed to sign token: %s", err.Error()),
			"result": "",
			"micro":  endRequest(c),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":   1,
		"msg":    "success",
		"result": tokenString,
		"micro":  endRequest(c),
	})
}
