package sse

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt"
)

type TokenParams struct {
	UID    string `json:"uid" form:"uid"`
	Device string `json:"device" form:"device"`
	TTL    int    `json:"ttl" form:"ttl"`
}

// CreateToken 生成JWT
func HandleToken(c *fiber.Ctx) error {
	startRequest(c)
	var params TokenParams
	if err := fillParams(c, &params); err != nil {
		slog.Error("Failed to fill params", "error", err)
		return nil
	}
	if params.TTL <= 0 {
		params.TTL = jwtExpire
	}

	duration := time.Duration(params.TTL) * time.Second
	claims := &Claims{
		UID:        params.UID,
		DeviceName: params.Device,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: time.Now().Add(duration).Unix(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"code":   http.StatusInternalServerError,
			"msg":    fmt.Sprintf("Failed to sign token: %s", err.Error()),
			"result": "",
			"micros": endRequest(c),
		})
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"code":   0,
		"msg":    "success",
		"result": tokenString,
		"micros": endRequest(c),
	})
}
