package sse

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
)

func startRequest(c *fiber.Ctx) {
	c.Locals("_start", time.Now().UnixMicro())
}

func endRequest(c *fiber.Ctx) int64 {
	start := c.Locals("_start")
	if start == nil {
		return 0
	}
	return time.Now().UnixMicro() - start.(int64)
}

func fillParams[T any](c *fiber.Ctx, params *T) error {
	method := c.Method()
	switch method {
	case fiber.MethodGet:
		if err := c.QueryParser(params); err != nil {
			c.Status(http.StatusBadRequest).JSON(fiber.Map{
				"code":   http.StatusBadRequest,
				"msg":    fmt.Sprintf("Failed to bind query: %s", err.Error()),
				"result": "",
				"micros": endRequest(c),
			})
			return err
		}
	case fiber.MethodPost:
		if err := c.BodyParser(params); err != nil {
			c.Status(http.StatusBadRequest).JSON(fiber.Map{
				"code":   http.StatusBadRequest,
				"msg":    fmt.Sprintf("Failed to bind json: %s", err.Error()),
				"result": "",
				"micros": endRequest(c),
			})
			return err
		}
	default:
		c.Status(http.StatusMethodNotAllowed).JSON(fiber.Map{
			"code":   http.StatusMethodNotAllowed,
			"msg":    fmt.Sprintf("Method not allowed: %s", method),
			"result": "",
			"micros": endRequest(c),
		})
		return fmt.Errorf("method not allowed")
	}
	return nil
}
