# go-sse-broker
go-sse-broker is a powerful and flexible SSE (Server-Sent Events) server designed to offer a complete SSE service solution. Built with scalability in mind, it connects to a Redis instance to automatically form a cluster, ensuring efficient message distribution across multiple nodes.

**Key features include:**

- **Multi-device support for a single user**: Users can connect from multiple devices simultaneously, with seamless synchronization across all connected devices.
- **Targeted message delivery**: Provides APIs to send messages and events to a specific user, a specific device, or broadcast to all connected clients.
- **Redis-based clustering**: Multiple go-sse-broker instances connected to the same Redis instance automatically form a cluster, enabling horizontal scaling and improved fault tolerance.
This makes go-sse-broker an ideal solution for real-time applications that require reliable event delivery across distributed systems.
