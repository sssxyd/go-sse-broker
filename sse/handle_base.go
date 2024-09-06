package sse

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func startRequest(c *gin.Context) {
	c.Set("_start", time.Now().UnixMicro())
}

func endRequest(c *gin.Context) int64 {
	start, _ := c.Get("_start")
	return time.Now().UnixMicro() - start.(int64)
}

func fillParams[T any](c *gin.Context, params *T) error {
	if c.Request.Method == "GET" {
		if err := c.BindQuery(params); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"code":   http.StatusBadRequest,
				"msg":    fmt.Sprintf("Failed to bind query: %s", err.Error()),
				"result": "",
				"micro":  endRequest(c),
			})
			return err
		}
	} else if c.Request.Method == "POST" {
		contentType := c.GetHeader("Content-Type")
		if contentType == "application/json" {
			if err := c.ShouldBindJSON(params); err != nil {
				c.JSON(http.StatusOK, gin.H{
					"code":   http.StatusBadRequest,
					"msg":    fmt.Sprintf("Failed to bind json: %s", err.Error()),
					"result": "",
					"micro":  endRequest(c),
				})
				return err
			}
		} else if contentType == "application/x-www-form-urlencoded" || contentType == "multipart/form-data" {
			if err := c.ShouldBind(params); err != nil {
				c.JSON(http.StatusOK, gin.H{
					"code":   http.StatusBadRequest,
					"msg":    fmt.Sprintf("Failed to bind form: %s", err.Error()),
					"result": "",
					"micro":  endRequest(c),
				})
				return err
			}
		} else {
			// 返回 415 不支持的媒体类型
			c.JSON(http.StatusOK, gin.H{
				"code":   http.StatusUnsupportedMediaType,
				"msg":    fmt.Sprintf("Unsupported media type: %s", contentType),
				"result": "",
				"micro":  endRequest(c),
			})
			return fmt.Errorf("unsupported media type")
		}
	} else {
		// 返回 405 不支持的请求方法
		c.JSON(http.StatusMethodNotAllowed, gin.H{
			"code":   http.StatusMethodNotAllowed,
			"msg":    fmt.Sprintf("Method not allowed: %s", c.Request.Method),
			"result": "",
			"micro":  endRequest(c),
		})
		return fmt.Errorf("method not allowed")
	}

	return nil
}
