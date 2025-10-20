package sse

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

var httpClient = &http.Client{
	Timeout: 10 * time.Second, // 设置请求超时时间
}

func dispatchInstructionBatch(channel string, instructions []Instruction) {
	ctx := context.Background()
	pipe := globalRedis.Pipeline()
	for _, instruction := range instructions {
		pipe.Publish(ctx, channel, instruction.String())
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		slog.Error("Failed to dispatch instruction batch", "error", err)
	}
}

func DispatchInstruction(instanceAddress string, instruction Instruction) {
	channel := fmt.Sprintf("%s%s", TOPIC_INSTANCE_PREFIX, instanceAddress)
	globalRedis.Publish(channel, instruction.String())
}

func DispatchInstructions(instanceAddress string, instructions []Instruction) {
	channel := fmt.Sprintf("%s%s", TOPIC_INSTANCE_PREFIX, instanceAddress)
	if len(instructions) == 1 {
		instruction := instructions[0]
		globalRedis.Publish(channel, instruction.String())
	} else {
		batchSize := 250 // 每批次发送250条指令
		for i := 0; i < len(instructions); i += batchSize {
			end := i + batchSize
			if end > len(instructions) {
				end = len(instructions)
			}
			dispatchInstructionBatch(channel, instructions[i:end])
		}
	}
}

func DispatchDeviceOnline(change StateChange) {
	payload := change.String()
	globalRedis.Publish(TOPIC_DEVICE_ONLINE, payload)
	on_state_change(TOPIC_DEVICE_ONLINE, payload)
}

func DispatchDeviceOffline(change StateChange) {
	payload := change.String()
	globalRedis.Publish(TOPIC_DEVICE_OFFLINE, payload)
	on_state_change(TOPIC_DEVICE_OFFLINE, payload)
}

func DispatchUserOnline(change StateChange) {
	payload := change.String()
	globalRedis.Publish(TOPIC_USER_ONLINE, payload)
	on_state_change(TOPIC_USER_ONLINE, payload)
}

func DispatchUserOffline(change StateChange) {
	payload := change.String()
	globalRedis.Publish(TOPIC_USER_OFFLINE, payload)
	on_state_change(TOPIC_USER_OFFLINE, payload)
}

func on_state_change(sse_topic string, payload string) {
	url, exist := globalConfig.Callback[sse_topic]
	if !exist || url == "" || url == "null" {
		return
	}
	url = strings.TrimSpace(url)
	if strings.HasPrefix(url, "http") || strings.HasPrefix(url, "https") {
		go post_json_with_retry(url, payload, 3)
	}
}

func post_json_with_retry(url string, payload string, retry int) {
	delaySeconds := 10
	for range retry {
		resp, err := httpClient.Post(url, "application/json", strings.NewReader(payload))
		if err == nil {
			if resp != nil {
				resp.Body.Close()
			}
			break
		}
		slog.Error("Failed to dispatch http event", "error", err)
		time.Sleep(time.Duration(delaySeconds) * time.Second)
		delaySeconds *= 6
	}
}
