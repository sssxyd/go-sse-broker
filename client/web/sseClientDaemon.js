/**
 * 基于 EventSource 的 SSE 客户端守护器（daemon）。
 *
 * 该实现的核心职责有三个：
 * 1. 统一管理 SSE 连接的建立、保持、关闭和重连流程；
 * 2. 每次建立新连接前都重新读取设备 ID 和访问令牌，避免使用过期凭据；
 * 3. 对服务端推送的系统事件和普通消息进行统一分发，便于上层业务按需监听。
 *
 * 适用于浏览器端或前端脚本环境中的实时消息订阅场景。
 *
 * 必填 options：
 * - getDevice: () => string，返回当前设备 ID。
 * - getToken: () => string，返回 SSE 服务 accessToken。
 * - url: string，SSE 地址，例如 /sse/events。
 */

import { remove } from "nprogress"

// 默认重连策略配置。
// 这些值决定连接失败后多久再次尝试建立连接，以及重试频率的增长方式。
const DEFAULTS = {
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
    'sys_connected',            // 连接成功
    'sys_kick_offline',         // 被踢下线
    'sys_extrude_offline',      // 被挤下线
    'sys_instance_close'        // 服务端实例关闭
]

// 简单的函数类型判断，便于对传入的回调进行统一校验。
function isFunction(fn) {
	return typeof fn === 'function'
}

/**
 * 拼接 URL 查询参数。
 * - 自动跳过 undefined / null / ''。
 * - 自动处理编码。
 * - 自动判断使用 ? 还是 & 追加参数。
 */
function joinUrlWithQuery(url, query) {
	const parts = []
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

function readLastEventIdFromStorage(storageKey) {
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

function writeLastEventIdToStorage(storageKey, value) {
	try {
		if (typeof window === 'undefined' || !window.localStorage) {
			return
		}
		window.localStorage.setItem(storageKey, value)
	} catch (error) {
		console.warn('[sseClientDaemon] write lastEventId to localStorage failed:', error)
	}
}

/**
 * 创建 SSE 客户端守护器。
 *
 * 可选 options：
 * - initialRetryDelay: number，默认 1000。
 * - maxRetryDelay: number，默认 30000。
 * - retryFactor: number，默认 2。
 * - jitter: number，默认 0.2。
 * - autoStart: boolean，默认 true。
 * - onOpen: (event) => void，连接建立成功回调。
 * - onError: (event) => void，连接异常回调。
 */
// 创建并返回一个 SSE 客户端守护器实例。
// 该实例会在创建后自动尝试建立连接，除非调用方显式关闭 autoStart。
export default function createSSEClientDaemon(options = {}) {
	const {
		getDevice,
		getToken,
		url,
		initialRetryDelay,
		maxRetryDelay,
		retryFactor,
		jitter,
		autoStart,
		lastEventIdStorageKey,
		onOpen,
		onError
	} = {
		...DEFAULTS,
		...options
	}

	if (!url) {
		throw new Error('[sseClientDaemon] options.url is required')
	}
	if (!isFunction(getDevice)) {
		throw new Error('[sseClientDaemon] options.getDevice must be a function')
	}
	if (!isFunction(getToken)) {
		throw new Error('[sseClientDaemon] options.getToken must be a function')
	}

	const storageKey = lastEventIdStorageKey || DEFAULTS.lastEventIdStorageKey
	let lastEventId = ''

	// 当前正在使用的 EventSource 实例。
	let eventSource = null
	// 当前重试次数，连接成功后会重置为 0。
	let retryCount = 0
	// 重连定时器句柄，用于延迟重试。
	let retryTimer = null
	// 守护器是否处于“应当保持连接”的活跃状态。
	let active = false
	// 按事件名保存的监听器集合。
	const eventListeners = new Map()
	// 已绑定到原生 EventSource 的事件处理器，避免重复注册。
	const nativeEventHandlers = new Map()
	// 注册的普通消息监听器（对应默认的 onmessage 事件）。
	const messageListeners = new Set()

	// 清理当前未执行的重连定时器，避免多个重连任务叠加。
	function clearRetryTimer() {
		if (!retryTimer) {
			return
		}
		clearTimeout(retryTimer)
		retryTimer = null
	}

	// 每次建立连接前都重新读取 device 和 token，并拼接为最终的 SSE 地址。
	// 这样可以避免使用已经失效的授权信息。
	function refreshLastEventId() {
		lastEventId = readLastEventIdFromStorage(storageKey) || ''
	}

	async function buildSseUrl() {
		refreshLastEventId()
		const device = await Promise.resolve(getDevice())
		const token = await Promise.resolve(getToken())
		const query = {
			device,
			token
		}
		// 如果存储中有 lastEventId，则在请求时带上，便于服务端从上次断开的位置继续推送消息。
		if (lastEventId) {
			query.id = lastEventId
		}
		return joinUrlWithQuery(url, query)
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
	function emitMessage(data) {
		messageListeners.forEach(listener => {
			try {
				listener(data)
			} catch (e) {
				console.error('[sseClientDaemon] message listener error:', e)
			}
		})
	}

	// 向指定事件名的监听器分发消息。
	function emitEvent(eventName, data) {
		const listeners = eventListeners.get(eventName)
		if (!listeners || listeners.size === 0) {
			return
		}

		listeners.forEach(listener => {
			try {
				listener(data)
			} catch (e) {
				// 保证单个监听器异常不影响其他监听器
				console.error('[sseClientDaemon] event listener error:', e)
			}
		})
	}

	// 统一将 EventSource 的消息内容转换为字符串。
	// 这里会尽量将 JSON 字符串解析为对象，但如果解析失败，则保留原始字符串。
	function parseEventData(messageEvent) {
		const rawData = messageEvent && messageEvent.data
		if (typeof rawData !== 'string') {
			return rawData
		}

		try {
			return JSON.parse(rawData)
		} catch (e) {
			return rawData
		}
	}

	// 解析消息对应的事件名。
	// 若浏览器事件对象本身携带事件名，则优先使用；否则回退到 data.event 字段。
	function resolveEventName(messageEvent, parsedData) {
		if (
			messageEvent &&
			typeof messageEvent.type === 'string' &&
			messageEvent.type &&
			messageEvent.type !== 'message'
		) {
			return messageEvent.type
		}

		if (
			parsedData &&
			typeof parsedData === 'object' &&
			!Array.isArray(parsedData) &&
			typeof parsedData.event === 'string' &&
			parsedData.event
		) {
			return parsedData.event
		}

		return ''
	}

	// 根据解析出的事件名决定如何分发消息。
	// 对系统事件做特殊处理，例如连接成功时仅记录日志，踢下线/挤下线时则停止守护器。
	function handleResolvedEvent(eventName, data) {
		if (eventName) {
			switch (eventName) {
				case 'sys_connected':
				case 'sys_instance_close':
					console.log('[sseClientDaemon] sys_connected event received:', data)
					emitEvent(eventName, data)
					break
				case 'sys_kick_offline':
				case 'sys_extrude_offline':
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

	function persistLastEventId(eventId) {
		if (!eventId) {
			return
		}
		lastEventId = eventId
		writeLastEventIdToStorage(storageKey, eventId)
	}

	function getEventIdFromMessageEvent(messageEvent) {
		if (messageEvent && typeof messageEvent.id === 'string' && messageEvent.id) {
			return messageEvent.id
		}
		return ''
	}

	// 统一入口：接收原生事件，解析数据并进行分发。
	function handleIncomingEvent(messageEvent) {
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
	function bindNativeEventHandler(eventName) {
		if (!eventSource || !eventName || nativeEventHandlers.has(eventName)) {
			return
		}

		const handler = event => {
			handleIncomingEvent(event)
		}

		eventSource.addEventListener(eventName, handler)
		nativeEventHandlers.set(eventName, handler)
	}

	// 解除对某个事件名的原生监听器绑定。
	function unbindNativeEventHandler(eventName) {
		if (!eventSource || !eventName || !nativeEventHandlers.has(eventName)) {
			return
		}

		const handler = nativeEventHandlers.get(eventName)
		eventSource.removeEventListener(eventName, handler)
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
	function calcRetryDelay() {
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
			// 定时触发时再次尝试建立连接
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
				onError(error)
			} else {
				console.error('[sseClientDaemon] connect error:', error)
			}
			scheduleReconnect()
			return
		}

		eventSource.onopen = event => {
			// 连接成功后清空重试次数，后续异常将从初始延迟重新计算
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

			// 统一异常处理：先关闭旧连接，再进入重连调度
			closeCurrentSource()
			scheduleReconnect()
		}
	}

	// 对外暴露的守护器对象。
	const daemon = {
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

			eventListeners.get(eventName).add(listener)
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

	// 默认自动启动，便于直接创建即用
	if (autoStart) {
		daemon.start()
	}

	return daemon
}
