package sse

import (
	"context"
	"fmt"
	"log"
)

func dispatchInstructionBatch(channel string, instructions []Instruction) {
	ctx := context.Background()
	pipe := globalRedis.Pipeline()
	for _, instruction := range instructions {
		pipe.Publish(ctx, channel, instruction.String())
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		log.Printf("Failed to dispatch instruction batch: %v\n", err)
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
	globalRedis.Publish(TOPIC_DEVICE_ONLINE, change.String())
}

func DispatchDeviceOffline(change StateChange) {
	globalRedis.Publish(TOPIC_DEVICE_OFFLINE, change.String())
}

func DispatchUserOnline(change StateChange) {
	globalRedis.Publish(TOPIC_USER_ONLINE, change.String())
}

func DispatchUserOffline(change StateChange) {
	globalRedis.Publish(TOPIC_USER_OFFLINE, change.String())
}
