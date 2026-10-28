package io.marvia.android

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.SystemClock
import java.util.concurrent.ConcurrentHashMap

/**
 * Uplink — есть ли у телефона сеть, кроме самого туннеля.
 *
 * Без него уведомление честно писало «Подключено» на телефоне, у которого
 * обе SIM-карты показывали «только экстренные вызовы», а Wi-Fi был выключен:
 * туннель жив в том смысле, что ядро работает, но ехать трафику некуда.
 * Человек смотрит на «Подключено», открывает сайт, ждёт — и решает, что
 * сломан VPN.
 *
 * Слушаем сети с интернетом, кроме VPN: NetworkRequest по умолчанию просит
 * NOT_VPN сам. Проверенность сети (VALIDATED) не требуем: Android проверяет
 * её запросом к Google, и в сети, где этот адрес режут, исправный интернет
 * выглядел бы пропавшим.
 */
class Uplink(context: Context) {
    private val manager = context.getSystemService(ConnectivityManager::class.java)
    private val alive: MutableSet<Network> = ConcurrentHashMap.newKeySet()

    @Volatile
    private var since = 0L

    private val callback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) {
            alive.add(network)
        }

        override fun onLost(network: Network) {
            alive.remove(network)
        }
    }

    fun start() {
        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .build()
        runCatching {
            manager?.registerNetworkCallback(request, callback)
            since = SystemClock.elapsedRealtime()
        }
    }

    fun stop() {
        if (since == 0L) return
        runCatching { manager?.unregisterNetworkCallback(callback) }
        since = 0L
        alive.clear()
    }

    /** Сети нет совсем. Пока система не ответила, считаем, что есть. */
    val offline: Boolean
        get() = isOffline(since, alive.size, SystemClock.elapsedRealtime())

    companion object {
        /**
         * Сколько ждать первого ответа системы. Сети, которые уже есть,
         * приходят в onAvailable сразу после регистрации, но не мгновенно, —
         * и без паузы каждое подключение начиналось бы с мигнувшего «Нет сети».
         */
        const val SETTLE_MS = 2_000L

        fun isOffline(since: Long, networks: Int, now: Long): Boolean =
            since != 0L && networks == 0 && now - since >= SETTLE_MS
    }
}
