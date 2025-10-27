package sse

const KEY_CLUSTER_INSTANCE_SET = "sse_cluster_instance_set"
const KEY_INSTANCE_PREFIX = "sse_instance_"
const KEY_INSTANCE_DEVICE_SET_PREFIX = "sse_instance_device_set_"
const KEY_DEVICE_PREFIX = "sse_device_"
const KEY_USER_DEVICE_SET_PREFIX = "sse_user_device_set_"
const KEY_FRAME_CACHE_PREFIX = "sse_frame_cache_"
const KEY_ONLINE_USER_SET = "sse_online_user_set"

const PAYLOAD_HEARTBEAT = ":heartbeat"

const TOPIC_USER_ONLINE = "sse_topic_user_online"
const TOPIC_USER_OFFLINE = "sse_topic_user_offline"
const TOPIC_DEVICE_ONLINE = "sse_topic_device_online"
const TOPIC_DEVICE_OFFLINE = "sse_topic_device_offline"
const TOPIC_INSTANCE_CLOSE = "sse_topic_instance_close"
const TOPIC_INSTANCE_START = "sse_topic_instance_start"
const TOPIC_INSTANCE_PREFIX = "sse_topic_instance_"

type SSECommand string

func (ci SSECommand) String() string {
	return string(ci)
}

const (
	CMD_SEND_FRAME      SSECommand = "send_frame"
	CMD_EXTRUDE_OFFLINE SSECommand = "extrude_offline"
	CMD_KICK_OFFLINE    SSECommand = "kick_offline"
	CMD_INSTANCE_CLOSE  SSECommand = "instance_close"
)

type SSESystemEvent string

func (sen SSESystemEvent) String() string {
	return string(sen)
}

const (
	EVT_SYS_CONNECTED       SSESystemEvent = "sys_connected"
	EVT_SYS_KICK_OFFLINE    SSESystemEvent = "sys_kick_offline"
	EVT_SYS_EXTRUDE_OFFLINE SSESystemEvent = "sys_extrude_offline"
	EVT_SYS_INSTANCE_CLOSE  SSESystemEvent = "sys_instance_close"
)

type DeviceCloseReason string

func (dcr DeviceCloseReason) String() string {
	return string(dcr)
}

const (
	DCR_EXTRUDE_OFFLINE   DeviceCloseReason = "extrude_offline"   // reason for extruding a device offline
	DCR_KICK_OFFLINE      DeviceCloseReason = "kick_offline"      // reason for kicking a device offline
	DCR_INSTANCE_CLOSE    DeviceCloseReason = "instance_close"    // reason for closing an instance
	DCR_INSTANCE_CLEAR    DeviceCloseReason = "instance_clear"    // reason for clearing an instance
	DCR_HEARTBEAT_FAIL    DeviceCloseReason = "heartbeat_fail"    // reason for heartbeat failure
	DCR_DEVICE_CONNECTED  DeviceCloseReason = "device_connected"  // reason for device connection
	DCR_DEVICE_DISCONNECT DeviceCloseReason = "device_disconnect" // reason for device disconnection
	DCR_ZOMBIE_CLEANUP    DeviceCloseReason = "zombie_cleanup"    // reason for zombie cleanup
)
