/**
 * 基于 EventSource 的 SSE 客户端守护器（daemon）TypeScript 版本。
 *
 * 该实现的核心职责有三个：
 * 1. 统一管理 SSE 连接的建立、保持、关闭和重连流程；
 * 2. 每次建立新连接前都重新读取设备 ID 和访问令牌，避免使用过期凭据；
 * 3. 对服务端推送的系统事件和普通消息进行统一分发，便于上层业务按需监听。
 *
 * 适用于浏览器端或前端脚本环境中的实时消息订阅场景。
 */

/**
 * 创建守护器时传入的配置项。
 * getDevice / getToken 用于动态生成当前连接所需的认证信息。
 */
export interface SSEClientDaemonOptions {
	getDevice: () => string | Promise<string>   // 获取当前设备 ID 的函数，返回值可以是字符串或 Promise<string>
	getToken: () => string | Promise<string>   // 获取当前访问令牌的函数，返回值可以是字符串或 Promise<string>
	url: string     // SSE 服务端地址，必须是支持 EventSource 的 URL
	initialRetryDelay?: number // 首次重连延迟（毫秒），默认 1000
	maxRetryDelay?: number  // 重连延迟上限（毫秒），默认 30000
	retryFactor?: number    // 指数退避倍率（第 N 次重连大致为 initialRetryDelay * retryFactor^N），默认 2
	jitter?: number     // 抖动系数，避免大量客户端在同一时间点同时重连，默认 0.2
	autoStart?: boolean // 创建实例后是否自动启动连接，默认 true
	lastEventIdStorageKey?: string // 存储 lastEventId 的 localStorage 键名，默认 sseClientDaemon:lastEventId
	onOpen?: (event: Event) => void // 连接成功时的回调函数
	onError?: (event: Event | Error) => void // 连接错误时的回调函数
}

/**
 * 对外暴露的守护器 API。
 * 通过这些方法，可以控制连接生命周期，并注册不同类型的消息监听器。
 */
export interface SSEClientDaemon {
    // 启动守护器：将状态切换为活跃，并尝试建立连接。
	start: () => void
    // 停止守护器：关闭当前连接、清理重连任务，并重置内部状态。
	stop: () => void
    // 立即重连：在当前没有活跃连接时先激活守护器，再发起一次连接尝试。
	reconnect: () => void
    // 注册普通消息监听器，收到默认 onmessage 消息时触发。
	addMessageListener: (listener: (data: string) => void) => void
    // 移除普通消息监听器。
	removeMessageListener: (listener: (data: string) => void) => void
    // 注册指定事件名的监听器，收到对应事件时触发。
	addEventListener: (eventName: string, listener: (data: string) => void) => void
    // 移除指定事件名的监听器。
	removeEventListener: (eventName: string, listener: (data: string) => void) => void
    // 当前连接状态：CONNECTING / OPEN / CLOSED。
	readonly readyState: number
    // 当前守护器是否处于活跃状态。
	readonly isActive: boolean
}

type DataListener = (data: string) => void

type QueryValue = string | number | boolean | null | undefined

type QueryMap = Record<string, QueryValue>

/**
 * 由服务端主动推送的系统事件名称。
 * 外部调用者可以直接引用这些常量，避免在业务代码中手写字符串。
 */
export enum SSESystemEventName {
	// 服务端通知客户端实例已成功连接，客户端无需任何操作。
	Connected = 'sys_connected',
	// 服务端通知客户端实例被踢下线，客户端应当停止守护器并清理相关状态，业务上可能触发自动登出或提示用户。
	KickOffline = 'sys_kick_offline',
	// 服务端通知客户端实例被挤下线，客户端应当停止守护器并清理相关状态，业务上可能触发自动登出或提示用户。
	ExtrudeOffline = 'sys_extrude_offline',
	// 服务端通知客户端实例，当前连接的服务器即将关闭，一般客户端无需任何操作，自动重连(连到其他服务器实例)即可。
	InstanceClose = 'sys_instance_close'
}

// 默认重连策略配置。
// 这些值决定连接失败后多久再次尝试建立连接，以及重试频率的增长方式。
const DEFAULTS: Required<Pick<SSEClientDaemonOptions, 'initialRetryDelay' | 'maxRetryDelay' | 'retryFactor' | 'jitter' | 'autoStart' | 'lastEventIdStorageKey'>> = {
	// 首次重连延迟（毫秒）
	initialRetryDelay: 1000,
	// 重连延迟上限（毫秒）
	maxRetryDelay: 30000,
	// 指数退避倍率（第 N 次重连大致为 initialRetryDelay * retryFactor^N）
	retryFactor: 2,
	// 抖动系数，避免大量客户端在同一时间点同时重连
	jitter: 0.2,
	// 创建实例后是否自动启动连接
	autoStart: true,
	// 存储 lastEventId 的 localStorage 键名
	lastEventIdStorageKey: 'sseClientDaemon:lastEventId'
}

// 需要默认绑定的系统级事件名称。
// 这些事件通常由服务端主动推送，用于通知客户端连接成功、被踢下线、实例关闭等状态。
const DEFAULT_SYS_EVENT_NAMES = [
	SSESystemEventName.Connected,
	SSESystemEventName.KickOffline,
	SSESystemEventName.ExtrudeOffline,
	SSESystemEventName.InstanceClose
]

// 简单的函数类型判断，便于对传入的回调进行统一校验。
function isFunction(fn: unknown): fn is (...args: any[]) => any {
	return typeof fn === 'function'
}

// 将对象中的键值对拼接成 URL 查询字符串。
// 这里会自动过滤空值、undefined 和 null，并根据当前 URL 是否已有 query 参数决定使用 ? 或 &。
function joinUrlWithQuery(url: string, query: QueryMap): string {
	const parts: string[] = []
	Object.keys(query).forEach(key => {
		const value = query[key]
		if (value === undefined || value === null || value === '') {
			return
		}
		parts.push(`${encodeURIComponent(key)}=${encodeURIComponent(String(value))}`)
	})

	if (parts.length === 0) {
		return url
	}

	return `${url}${url.indexOf('?') === -1 ? '?' : '&'}${parts.join('&')}`
}

function readLastEventIdFromStorage(storageKey: string): string | null {
	try {
		if (typeof window === 'undefined' || !window.localStorage) {
			return null
		}
		return window.localStorage.getItem(storageKey)
	} catch (error) {
		console.warn('[sseClientDaemon] read lastEventId from localStorage failed:', error)
		return null
	}
}

function writeLastEventIdToStorage(storageKey: string, value: string): void {
	try {
		if (typeof window === 'undefined' || !window.localStorage) {
			return
		}
		window.localStorage.setItem(storageKey, value)
	} catch (error) {
		console.warn('[sseClientDaemon] write lastEventId to localStorage failed:', error)
	}
}

export default function createSSEClientDaemon(options: Partial<SSEClientDaemonOptions> = {}): SSEClientDaemon {
	const resolvedOptions = {
		...DEFAULTS,
		...options
	}

	const {
		initialRetryDelay,
		maxRetryDelay,
		retryFactor,
		jitter,
		autoStart,
		lastEventIdStorageKey,
		onOpen,
		onError
	} = resolvedOptions

	const url = resolvedOptions.url
	const getDevice = resolvedOptions.getDevice
	const getToken = resolvedOptions.getToken

	if (!url) {
		throw new Error('[sseClientDaemon] options.url is required')
	}
	if (!isFunction(getDevice)) {
		throw new Error('[sseClientDaemon] options.getDevice must be a function')
	}
	if (!isFunction(getToken)) {
		throw new Error('[sseClientDaemon] options.getToken must be a function')
	}

	const requestUrl = url as string
	const getDeviceFn = getDevice as () => string | Promise<string>
	const getTokenFn = getToken as () => string | Promise<string>
	const storageKey = lastEventIdStorageKey || DEFAULTS.lastEventIdStorageKey
	let lastEventId = ''

	// 当前正在使用的 EventSource 实例。
	let eventSource: EventSource | null = null
	// 当前重试次数，连接成功后会重置为 0。
	let retryCount = 0
	// 重连定时器句柄，用于延迟重试。
	let retryTimer: ReturnType<typeof setTimeout> | null = null
	// 守护器是否处于“应当保持连接”的活跃状态。
	let active = false
	// 按事件名保存的监听器集合。
	const eventListeners = new Map<string, Set<DataListener>>()
	// 已绑定到原生 EventSource 的事件处理器，避免重复注册。
	const nativeEventHandlers = new Map<string, EventListener>()
	// 注册的普通消息监听器（对应默认的 onmessage 事件）。
	const messageListeners = new Set<DataListener>()

	// 清理当前未执行的重连定时器，避免多个重连任务叠加。
	function clearRetryTimer() {
		if (!retryTimer) {
			return
		}
		clearTimeout(retryTimer)
		retryTimer = null
	}

	function refreshLastEventId() {
		lastEventId = readLastEventIdFromStorage(storageKey) || ''
	}

	// 每次建立连接前都重新读取 device 和 token，并拼接为最终的 SSE 地址。
	// 这样可以避免使用已经失效的授权信息。
	async function buildSseUrl(): Promise<string> {
		refreshLastEventId()
		const device = await Promise.resolve(getDeviceFn())
		const token = await Promise.resolve(getTokenFn())
		const query: QueryMap = {
			device,
			token
		}
		// 如果存储中有 lastEventId，则在请求时带上，便于服务端从上次断开的位置继续推送消息。
		if (lastEventId) {
			query.id = lastEventId
		}
		return joinUrlWithQuery(requestUrl, query)
	}

	// 关闭当前已存在的 EventSource 实例。
	// 在重连前或停止守护器时需要调用，确保旧连接不再保留。
	function closeCurrentSource() {
		if (!eventSource) {
			return
		}
		eventSource.close()
		eventSource = null
	}

	// 向所有普通 message 监听器分发消息。
	function emitMessage(data: string) {
		messageListeners.forEach(listener => {
			try {
				listener(data)
			} catch (e) {
				console.error('[sseClientDaemon] message listener error:', e)
			}
		})
	}

	// 向指定事件名的监听器分发消息。
	function emitEvent(eventName: string, data: string) {
		const listeners = eventListeners.get(eventName)
		if (!listeners || listeners.size === 0) {
			return
		}

		listeners.forEach(listener => {
			try {
				listener(data)
			} catch (e) {
				console.error('[sseClientDaemon] event listener error:', e)
			}
		})
	}

	// 统一将 EventSource 的消息内容转换为字符串。
	// 浏览器原生 SSE 的 data 以字符串形式传递，因此这里做统一归一化，避免上层处理时出现类型差异。
	function parseEventData(messageEvent: MessageEvent): string {
		const rawData = messageEvent && messageEvent.data
		if (typeof rawData === 'string') {
			return rawData
		}
		return String(rawData ?? '')
	}

	// 解析消息对应的事件名。
	// 若浏览器事件对象本身携带事件名，则优先使用；否则返回空字符串，表示它是普通消息。
	function resolveEventName(messageEvent: MessageEvent, _parsedData: string): string {
		if (
			messageEvent &&
			typeof messageEvent.type === 'string' &&
			messageEvent.type &&
			messageEvent.type !== 'message'
		) {
			return messageEvent.type
		}

		return ''
	}

	// 根据解析出的事件名决定如何分发消息。
	// 对系统事件做特殊处理，例如连接成功时仅记录日志，踢下线/挤下线时则停止守护器。
	function handleResolvedEvent(eventName: string, data: string) {
		if (eventName) {
			switch (eventName) {
				case SSESystemEventName.Connected:
				case SSESystemEventName.InstanceClose:
					console.log('[sseClientDaemon] sys_connected event received:', data)
					emitEvent(eventName, data)
					break
				case SSESystemEventName.KickOffline:
				case SSESystemEventName.ExtrudeOffline:
					console.log(`[sseClientDaemon] ${eventName} event received, stopping daemon:`, data)
					emitEvent(eventName, data)
					daemon.stop()
					break
				default:
					console.log(`[sseClientDaemon] event received: ${eventName}`, data)
					emitEvent(eventName, data)
			}
			return
		}

		emitMessage(data)
	}

	function persistLastEventId(eventId: string) {
		if (!eventId) {
			return
		}
		lastEventId = eventId
		writeLastEventIdToStorage(storageKey, eventId)
	}

	function getEventIdFromMessageEvent(messageEvent: MessageEvent): string {
		const eventWithId = messageEvent as MessageEvent & { id?: string }
		if (typeof eventWithId.id === 'string' && eventWithId.id) {
			return eventWithId.id
		}
		return ''
	}

	// 统一入口：接收原生事件，解析数据并进行分发。
	function handleIncomingEvent(messageEvent: MessageEvent) {
		const eventId = getEventIdFromMessageEvent(messageEvent)
		if (eventId) {
			persistLastEventId(eventId)
		}
		const data = parseEventData(messageEvent)
		const eventName = resolveEventName(messageEvent, data)
		handleResolvedEvent(eventName, data)
	}

	// 为指定事件名绑定原生 EventSource 监听器。
	// 只有当当前连接实例存在且该事件尚未绑定过时才会注册。
	function bindNativeEventHandler(eventName: string) {
		if (!eventSource || !eventName || nativeEventHandlers.has(eventName)) {
			return
		}

		const handler: EventListener = (event: Event) => {
			handleIncomingEvent(event as MessageEvent)
		}

		eventSource.addEventListener(eventName, handler)
		nativeEventHandlers.set(eventName, handler)
	}

	// 解除对某个事件名的原生监听器绑定。
	function unbindNativeEventHandler(eventName: string) {
		if (!eventSource || !eventName || !nativeEventHandlers.has(eventName)) {
			return
		}

		const handler = nativeEventHandlers.get(eventName)
		if (handler) {
			eventSource.removeEventListener(eventName, handler)
		}
		nativeEventHandlers.delete(eventName)
	}

	// 将默认系统事件和当前已注册的自定义事件监听器全部绑定到当前连接实例上。
	function bindAllNativeEventHandlers() {
		DEFAULT_SYS_EVENT_NAMES.forEach(eventName => {
			bindNativeEventHandler(eventName)
		})

		eventListeners.forEach((listeners, eventName) => {
			if (listeners && listeners.size > 0) {
				bindNativeEventHandler(eventName)
			}
		})
	}

	// 根据指数退避策略计算下一次重连的等待时长。
	// 同时引入抖动系数，避免大量客户端在同一时间点同时重连造成雪崩效应。
	function calcRetryDelay(): number {
		const baseDelay = Math.min(
			initialRetryDelay * Math.pow(retryFactor, retryCount),
			maxRetryDelay
		)
		const randomFactor = 1 + (Math.random() * 2 - 1) * jitter
		return Math.max(0, Math.floor(baseDelay * randomFactor))
	}

	// 在满足活跃状态且当前没有正在执行的重连任务时，安排下一次重连。
	function scheduleReconnect() {
		if (!active || retryTimer) {
			return
		}

		const delay = calcRetryDelay()
		retryCount += 1

		retryTimer = setTimeout(() => {
			retryTimer = null
			connect()
		}, delay)
	}

	// 建立 SSE 连接，并为 onopen / onmessage / onerror 绑定统一处理逻辑。
	async function connect() {
		if (!active) {
			return
		}

		clearRetryTimer()
		closeCurrentSource()

		try {
			const sseUrl = await buildSseUrl()
			if (!active) {
				return
			}
			console.log('[sseClientDaemon] connecting to', sseUrl)
			eventSource = new EventSource(sseUrl)
			bindAllNativeEventHandlers()
		} catch (error) {
			if (isFunction(onError)) {
				onError(error as Error)
			} else {
				console.error('[sseClientDaemon] connect error:', error)
			}
			scheduleReconnect()
			return
		}

		eventSource.onopen = event => {
			retryCount = 0
			if (isFunction(onOpen)) {
				onOpen(event)
			}
		}

		eventSource.onmessage = event => {
			const data = parseEventData(event)
			emitMessage(data)
			handleResolvedEvent(resolveEventName(event, data), data)
		}

		eventSource.onerror = event => {
			if (isFunction(onError)) {
				onError(event)
			}

			closeCurrentSource()
			scheduleReconnect()
		}
	}

	// 对外暴露的守护器对象。
	const daemon: SSEClientDaemon = {
		// 启动守护器：将状态切换为活跃，并尝试建立连接。
		start() {
			if (active) {
				return
			}
			active = true
			retryCount = 0
			connect()
		},

		// 停止守护器：关闭当前连接、清理重连任务，并重置内部状态。
		stop() {
			active = false
			clearRetryTimer()
			closeCurrentSource()
			retryCount = 0
		},

		// 立即重连：在当前没有活跃连接时先激活守护器，再发起一次连接尝试。
		reconnect() {
			if (!active) {
				active = true
			}
			retryCount = 0
			connect()
		},

		// 注册普通消息监听器，收到默认 onmessage 消息时触发。
		addMessageListener(listener) {
			if (!isFunction(listener)) {
				throw new Error('[sseClientDaemon] listener must be a function')
			}
			messageListeners.add(listener)
		},

		// 移除普通消息监听器。
		removeMessageListener(listener) {
			if (!isFunction(listener)) {
				return
			}
			messageListeners.delete(listener)
		},

		// 注册指定事件名的监听器，收到对应事件时触发。
		addEventListener(eventName, listener) {
			if (typeof eventName !== 'string' || !eventName) {
				throw new Error('[sseClientDaemon] eventName must be a non-empty string')
			}
			if (!isFunction(listener)) {
				throw new Error('[sseClientDaemon] listener must be a function')
			}

			if (!eventListeners.has(eventName)) {
				eventListeners.set(eventName, new Set())
			}

			eventListeners.get(eventName)!.add(listener)
			bindNativeEventHandler(eventName)
		},

		// 移除指定事件名的监听器。
		removeEventListener(eventName, listener) {
			if (typeof eventName !== 'string' || !eventName) {
				return
			}
			if (!isFunction(listener)) {
				return
			}

			const listeners = eventListeners.get(eventName)
			if (!listeners) {
				return
			}

			listeners.delete(listener)
			if (listeners.size === 0) {
				eventListeners.delete(eventName)
				unbindNativeEventHandler(eventName)
			}
		},

		// 当前连接状态：CONNECTING / OPEN / CLOSED。
		get readyState() {
			return eventSource ? eventSource.readyState : EventSource.CLOSED
		},

		// 当前守护器是否处于活跃状态。
		get isActive() {
			return active
		}
	}

	if (autoStart) {
		daemon.start()
	}

	return daemon
}
