/**
 * 基于 EventSource 的 SSE 客户端守护器。
 *
 * @typedef {Object} SSEClientDaemonOptions
 * @property {Function} getDevice 返回当前设备 ID。
 * @property {Function} getToken 返回 SSE 服务 accessToken。
 * @property {string} url SSE 地址，例如 `/sse/events`。
 * @property {number} [initialRetryDelay=1000] 首次重连延迟，单位为毫秒。
 * @property {number} [maxRetryDelay=30000] 重连延迟上限，单位为毫秒。
 * @property {number} [retryFactor=2] 指数退避倍率。
 * @property {number} [jitter=0.2] 抖动系数。
 * @property {boolean} [autoStart=true] 创建实例后是否自动启动连接。
 * @property {string} [lastEventIdStorageKey] localStorage 中保存 lastEventId 的键名。
 * @property {Function} [onOpen] 连接建立成功回调。
 * @property {Function} [onError] 连接异常回调。
 * 
 * @typedef {Object} SSEClientDaemon
 * @property {Function} start 启动守护器并尝试建立连接。
 * @property {Function} stop 停止守护器并重置内部状态。
 * @property {Function} reconnect 激活守护器并立即发起一次连接尝试。
 * @property {Function} addMessageListener 注册普通消息监听器。
 * @property {Function} removeMessageListener 移除普通消息监听器。
 * @property {Function} addEventListener 注册指定事件名的监听器。
 * @property {Function} removeEventListener 移除指定事件名的监听器。
 * @property {number} readyState 当前连接状态：CONNECTING / OPEN / CLOSED。
 * @property {boolean} isActive 当前守护器是否处于活跃状态。
 */

/** 默认重连策略配置。 */
const DEFAULTS = {
	/** 首次重连延迟（毫秒）。 */
	initialRetryDelay: 1000,
	/** 重连延迟上限（毫秒）。 */
	maxRetryDelay: 30000,
	/** 指数退避倍率。 */
	retryFactor: 2,
	/** 抖动系数，避免大量客户端同时重连。 */
	jitter: 0.2,
	/** 创建实例后是否自动启动连接。 */
	autoStart: true,
	/** 存储 lastEventId 的 localStorage 键名。 */
	lastEventIdStorageKey: 'sseClientDaemon:lastEventId'
}

/** 需要默认绑定的系统级事件名称。 */
const DEFAULT_SYS_EVENT_NAMES = [
	/** 服务端通知客户端实例已成功连接。 */
    'sys_connected', 
	/** 服务端通知客户端实例被踢下线。 */
    'sys_kick_offline', 
	/** 服务端通知客户端实例被挤下线。 */
    'sys_extrude_offline',      
	/** 服务端通知当前连接的服务器即将关闭。 */
    'sys_instance_close'     
]

/**
 * 判断值是否为函数。
 *
 * @param {*} fn 待检查的值。
 * @returns {boolean} 值是否为函数。
 */
function isFunction(fn) {
	return typeof fn === 'function'
}

/**
 * 拼接 URL 查询参数。
 *
 * @param {string} url 基础 URL。
 * @param {Object} query 查询参数对象。
 * @returns {string} 拼接后的 URL。
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

/**
 * 从 localStorage 读取上次的事件 ID。
 *
 * @param {string} storageKey 存储键名。
 * @returns {string|null} 已保存的事件 ID。
 */
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

/**
 * 将事件 ID 写入 localStorage。
 *
 * @param {string} storageKey 存储键名。
 * @param {string} value 要保存的事件 ID。
 * @returns {void}
 */
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
 * 创建 SSE 客户端守护器实例。
 *
 * @param {SSEClientDaemonOptions} [options={}] 守护器配置。
 * @returns {SSEClientDaemon} SSE 客户端守护器实例。
 */
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

	/** 当前正在使用的 EventSource 实例。 */
	let eventSource = null
	/** 当前重试次数，连接成功后会重置为 0。 */
	let retryCount = 0
	/** 重连定时器句柄，用于延迟重试。 */
	let retryTimer = null
	/** 守护器是否处于应当保持连接的活跃状态。 */
	let active = false
	/** 按事件名保存的监听器集合。 */
	const eventListeners = new Map()
	/** 已绑定到原生 EventSource 的事件处理器。 */
	const nativeEventHandlers = new Map()
	/** 注册的普通消息监听器，对应默认的 onmessage 事件。 */
	const messageListeners = new Set()

	/** 清理当前未执行的重连定时器。 */
	function clearRetryTimer() {
		if (!retryTimer) {
			return
		}
		clearTimeout(retryTimer)
		retryTimer = null
	}

	/** 每次建立连接前重新读取 device 和 token，并拼接最终的 SSE 地址。 */
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
		/** 如果存在 lastEventId，则带在请求中以便服务端续传。 */
		if (lastEventId) {
			query.id = lastEventId
		}
		return joinUrlWithQuery(url, query)
	}

	/** 关闭当前 EventSource 实例。 */
	function closeCurrentSource() {
		if (!eventSource) {
			return
		}
		eventSource.close()
		eventSource = null
	}

	/** 向所有普通 message 监听器分发消息。 */
	function emitMessage(data) {
		messageListeners.forEach(listener => {
			try {
				listener(data)
			} catch (e) {
				console.error('[sseClientDaemon] message listener error:', e)
			}
		})
	}

	/** 向指定事件名的监听器分发消息。 */
	function emitEvent(eventName, data) {
		const listeners = eventListeners.get(eventName)
		if (!listeners || listeners.size === 0) {
			return
		}

		listeners.forEach(listener => {
			try {
				listener(data)
			} catch (e) {
				/** 保证单个监听器异常不影响其他监听器。 */
				console.error('[sseClientDaemon] event listener error:', e)
			}
		})
	}

	/** 将 EventSource 的消息内容解析为对象或原始字符串。 */
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

	/** 解析消息对应的事件名，优先使用原生事件类型。 */
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

	/** 根据事件名分发消息，并处理系统事件。 */
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

	/** 接收原生事件，保存事件 ID、解析数据并进行分发。 */
	function handleIncomingEvent(messageEvent) {
		const eventId = getEventIdFromMessageEvent(messageEvent)
		if (eventId) {
			persistLastEventId(eventId)
		}
		const data = parseEventData(messageEvent)
		const eventName = resolveEventName(messageEvent, data)
		handleResolvedEvent(eventName, data)
	}

	/** 为指定事件名绑定原生 EventSource 监听器。 */
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

	/** 解除某个事件名的原生监听器绑定。 */
	function unbindNativeEventHandler(eventName) {
		if (!eventSource || !eventName || !nativeEventHandlers.has(eventName)) {
			return
		}

		const handler = nativeEventHandlers.get(eventName)
		eventSource.removeEventListener(eventName, handler)
		nativeEventHandlers.delete(eventName)
	}

	/** 将默认系统事件和自定义事件监听器绑定到当前连接实例。 */
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

	/** 根据指数退避策略和抖动系数计算下一次重连的等待时长。 */
	function calcRetryDelay() {
		const baseDelay = Math.min(
			initialRetryDelay * Math.pow(retryFactor, retryCount),
			maxRetryDelay
		)
		const randomFactor = 1 + (Math.random() * 2 - 1) * jitter
		return Math.max(0, Math.floor(baseDelay * randomFactor))
	}

	/** 在活跃且没有重连任务时安排下一次重连。 */
	function scheduleReconnect() {
		if (!active || retryTimer) {
			return
		}

		const delay = calcRetryDelay()
		retryCount += 1

		retryTimer = setTimeout(() => {
			retryTimer = null
			/** 定时触发时再次尝试建立连接。 */
			connect()
		}, delay)
	}

	/** 建立 SSE 连接，并绑定统一的连接事件处理逻辑。 */
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
			/** 连接成功后从初始延迟重新计算后续重试。 */
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

			/** 先关闭旧连接，再进入重连调度。 */
			closeCurrentSource()
			scheduleReconnect()
		}
	}

	/** 对外暴露的守护器对象。 */
	const daemon = {
		/** 启动守护器并尝试建立连接。 */
		start() {
			if (active) {
				return
			}
			active = true
			retryCount = 0
			connect()
		},

		/** 停止守护器并重置内部状态。 */
		stop() {
			active = false
			clearRetryTimer()
			closeCurrentSource()
			retryCount = 0
		},

		/** 激活守护器并立即发起一次连接尝试。 */
		reconnect() {
			if (!active) {
				active = true
			}
			retryCount = 0
			connect()
		},

		/** 注册普通消息监听器。 */
		addMessageListener(listener) {
			if (!isFunction(listener)) {
				throw new Error('[sseClientDaemon] listener must be a function')
			}
			messageListeners.add(listener)
		},

		/** 移除普通消息监听器。 */
		removeMessageListener(listener) {
			if (!isFunction(listener)) {
				return
			}
			messageListeners.delete(listener)
		},

		/** 注册指定事件名的监听器。 */
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

		/** 移除指定事件名的监听器。 */
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

		/** 当前连接状态：CONNECTING / OPEN / CLOSED。 */
		get readyState() {
			return eventSource ? eventSource.readyState : EventSource.CLOSED
		},

		/** 当前守护器是否处于活跃状态。 */
		get isActive() {
			return active
		}
	}

	/** 根据配置决定是否自动启动。 */
	if (autoStart) {
		daemon.start()
	}

	return daemon
}
