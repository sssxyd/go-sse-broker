package sse

import (
	"encoding/json"
	"time"
)

type Config struct {
	JWT struct {
		Secret string
		Expire int
	}
	Redis struct {
		Addrs    []string
		Password string
		DB       int
		PoolSize int
	}
	SSE struct {
		HeartbeatDuration         time.Duration
		DeviceUserExistDuration   time.Duration
		DeviceFrameExpireDuration time.Duration
		DeviceFrameCacheSize      int
	}
}

type Instruction struct {
	DeviceID string `json:"device_id"`
	Command  string `json:"command"`
	Event    string `json:"event"`
	Data     string `json:"data"`
}

func (i *Instruction) String() string {
	json, err := json.Marshal(i)
	if err != nil {
		json = []byte("{}")
	}
	return string(json)
}

type Frame struct {
	ID    int64  `json:"id"`
	Event string `json:"event"`
	Data  string `json:"data"`
}

func (f *Frame) String() string {
	json, err := json.Marshal(f)
	if err != nil {
		json = []byte("{}")
	}
	return string(json)
}

type StateChange struct {
	UID         string `json:"uid"`
	DeviceID    string `json:"device_id"`
	TriggerTime string `json:"trigger_time"`
	Reason      string `json:"reason"`
	Payload     string `json:"payload"`
}

func (s *StateChange) String() string {
	json, err := json.Marshal(s)
	if err != nil {
		json = []byte("{}")
	}
	return string(json)
}
