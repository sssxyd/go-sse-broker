# Feature
go-sse-broker is a powerful and flexible SSE (Server-Sent Events) server designed to offer a complete SSE service solution. Built with scalability in mind, it connects to a Redis instance to automatically form a cluster, ensuring efficient message distribution across multiple nodes.

**Key features include:**

- **Multi-device support for a single user**: Users can connect from multiple devices simultaneously, with seamless synchronization across all connected devices.
- **Targeted message delivery**: Provides APIs to send messages and events to a specific user, a specific device, or broadcast to all connected clients.
- **Redis-based clustering**: Multiple go-sse-broker instances connected to the same Redis instance automatically form a cluster, enabling horizontal scaling and improved fault tolerance. This makes go-sse-broker an ideal solution for real-time applications that require reliable event delivery across distributed systems.

# Usage
## Windows
- Download zip from the [release page](https://github.com/sssxyd/go-sse-broker/releases/).
- Edit `config.toml`.
- Double click `sse-broker.exe`.
- Visit the API&Demo Page: [http://localhost:8080/](http://localhost:8080/)

## Linux
- Download rpm from the [release page](https://github.com/sssxyd/go-sse-broker/releases/).
- Install `rpm -Uvh sse-broker-1.0.1-1.el7.x86_64`
- Edit configuration: `vim /etc/sse-broker/config.toml`
- Start the service: `systemctl start sse-broker`
- Visit the API&Demo Page: [http://127.0.0.1:8080/](http://127.0.0.1:8080/)

## Docker
```shell
docker build -t sse-broker:1.0.2 .
```

## Edit config.toml 
```toml
[jwt]
secret = "please_modify"

[redis]
addrs = ["please_modify_1:6379", "please_modify_2:6379"]
password = "please_modify"
```

# Api
## Create Token
- EndPoint: /token
- HTTP Method: GET/POST
- Parameters:  
  | name | type | required | desc |
  |------|------|----------|------|
  | uid  | string | true | unique user id |
  | device | string | false | unique client device id |

- Request Example
  - Get  
  `/token?uid=1965`
  - Post JSON
  ```json
  {
    "uid": "1986",
    "device": "my computer 1"
  }
  ```  
- Response Example (code 1:success, others:failure)
  ```json
  {
    "code": 1,            
    "msg": "success",
    "micro": 192,
    "result": "jwt token string"
  }
  ```
  
## Send Message/Event
- EndPoint: /send
- HTTP Method: GET/POST
- Parameters:  
  | name | type | required | desc |
  |------|------|----------|------|
  | data | string | true | message data |
  | event | string | false | event name |
  | uid  | string | false | multiple uids separated by commas |
  | device | string | false | multiple devices separated by commas |
- Request Example  
  - Get  
    `/send?uid=1935&data=hello`
  - Post Json
   ```json
   {
    "device": "ax001,ax002",
    "event": "custom-event",
    "data": "hello"
   }
   ```
- Response Example  (code 1:success, others:failure)
  ```json
  {
    "code": 1,         
    "msg": "success",
    "micro": 225,
    "result": 2       
  }
  ```

## SSE Connection
- EndPoint: /events
- HTTP Method: Get
- Parameters: Either use parameters in the header or in the query
  - HTTP Headers (Optional):  
    | name | type | required | desc |
    |------|------|----------|------|
    | X-SSE-TOKEN | string | true | token |
    | X-SSE-DEVICE | string | true | device |
    | X-SSE-ID  | string | false | last event id |
  - Query (Optional):  
    | name | type | required | desc |
    |------|------|----------|------|
    | token | string | true | token |
    | device | string | true | device |
    | id  | string | false | id |
- JS Example (JS)
  ```js
    const token = "jwt_token"
    const device = "my computer 1"
    const lastEventId = 1098

    // connect to sse-broker
    const eventSource = new EventSource('/events?token=' + token + '&device=' + device + '&id=' + lastEventId)

    // message
    eventSource.onmessage = function(event) {
        appendMessage('message', 'Message', event.data);
    };

    // event: 'custom-event'
    eventSource.addEventListener('custom-event', function(event) {
        appendMessage('event', 'Custom Event', event.data);
    });

    // close
    eventSource.onerror = function() {
        appendMessage('error', 'Error', 'Connection was closed');
        eventSource.close();
    };
  ```

## Info/Status
- EndPoint: /info
- HTTP Method: GET/POST
- Parameters:  
  | name | type | required | desc |
  |------|------|----------|------|
  | uid  | string | false | one user id |
  | device | string | false | one device |
  | ip | string | false | instance ip address |  
- Request Example  
  - Get: Custer Info  
    `/info`
  - Post: User Info
   ```json
   {
    "uid": "sssxyd"
   }
   ```
- Response Example  (code 1:success, others:failure)
  ```json
  {
    "code": 1,         
    "msg": "success",
    "micro": 225,
    "result": {
        "online": true,
        "uid": "sssxyd",
        "login_time": "2024-09-12 09:57:59",
        "last_touch_time": "2024-09-12 10:13:59",
        "devices": [
            {
                "device_id": "c7ea097beaf447e97f41af1c4651983c",
                "device_name": "xuyd",
                "uid": "sssxyd",
                "login_time": "2024-09-12 09:57:59",
                "instance_address": "192.168.2.22",
                "device_address": "192.168.2.22:64321",
                "last_touch_time": "2024-09-12 10:13:59",
                "last_frame_id": 12
            }
        ]
    }      
  }
  ```

## Kick Offline
- EndPoint: /kick
- HTTP Method: GET/POST
- Parameters:  
  | name | type | required | desc |
  |------|------|----------|------|
  | uid  | string | false | multiple uids separated by commas |
  | device | string | false | multiple devices separated by commas |
- Request Example  
  - Get  
    `/kick?uid=1935,1936`
  - Post Json
   ```json
   {
    "device": "ax001,ax002",
    "uid": "1937",
   }
   ```
- Response Example  (code 1:success, others:failure)
  ```json
  {
    "code": 1,         
    "msg": "success",
    "micro": 225,
    "result": 6       
  }

# Callback
**Please Subscribe Redis Channel**  
- Redis Channels
  | Redis Channel | Trigger |
  |---|---|
  |sse_topic_user_online| uid first connected |
  |sse_topic_user_offline| last device of uid disconnected |
  |sse_topic_device_online| device connected |
  |sse_topic_device_offline| device disconnected |

- Message JSON Structure (Go)
  ```go
  type StateChange struct {
	UID         string `json:"uid"`
	DeviceID    string `json:"device_id"`
	TriggerTime string `json:"trigger_time"`
	Reason      string `json:"reason"`
	Payload     string `json:"payload"`
  }
  ```
- Reasons (Go)
  ```go
    const DCR_EXTRUDE_OFFLINE = "extrude_offline"
    const DCR_KICK_OFFLINE = "kick_offline"
    const DCR_INSTANCE_CLOSE = "instance_close"
    const DCR_INSTANCE_CLEAR = "instance_clear"
    const DCR_HEARTBEAT_FAIL = "heartbeat_fail"
    const DCR_DISCONNECT = "disconnect"
    const DCR_CONNECTED = "connected"
    const DCR_DEVICE_ONLINE = "device_online"
  ```


