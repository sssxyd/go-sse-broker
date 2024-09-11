package sse

const KEY_INSTANCE_SET = "sse_instance_set"
const KEY_INSTANCE_PREFIX = "sse_instance_"
const KEY_INSTANCE_DEVICE_SET_PREFIX = "sse_instance_device_set_"
const KEY_DEVICE_PREFIX = "sse_device_"
const KEY_USER_DEVICE_SET_PREFIX = "sse_user_device_set_"
const KEY_FRAME_CACHE_PREFIX = "sse_frame_cache_"
const KEY_ONLINE_USER_SET = "sse_online_user_set"

const CMD_SEND_FRAME = "send_frame"
const CMD_EXTRUDE_OFFLINE = "extrude_offline"
const CMD_KICK_OFFLINE = "kick_offline"
const CMD_INSTANCE_CLOSE = "instance_close"

const DCR_EXTRUDE_OFFLINE = "extrude_offline"
const DCR_KICK_OFFLINE = "kick_offline"
const DCR_INSTANCE_CLOSE = "instance_close"
const DCR_INSTANCE_CLEAR = "instance_clear"
const DCR_HEARTBEAT_FAIL = "heartbeat_fail"
const DCR_DISCONNECT = "disconnect"
const DCR_CONNECTED = "connected"
const DCR_DEVICE_ONLINE = "device_online"

const EVT_SYS_CONNECTED = "sys_connected"
const EVT_SYS_KICK_OFFLINE = "sys_kick_offline"
const EVT_SYS_EXTRUDE_OFFLINE = "sys_extrude_offline"
const EVT_SYS_INSTANCE_CLOSE = "sys_instance_close"

const PAYLOAD_HEARTBEAT = ":heartbeat"

const TOPIC_USER_ONLINE = "sse_topic_user_online"
const TOPIC_USER_OFFLINE = "sse_topic_user_offline"
const TOPIC_DEVICE_ONLINE = "sse_topic_device_online"
const TOPIC_DEVICE_OFFLINE = "sse_topic_device_offline"
const TOPIC_INSTANCE_CLOSE = "sse_topic_instance_close"
const TOPIC_INSTANCE_START = "sse_topic_instance_start"
const TOPIC_INSTANCE_PREFIX = "sse_topic_instance_"
