package io.marvia.android

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.Build
import android.os.Handler
import android.os.Looper

/** Первое сообщение — исходная сеть; обновления её свойств не рвут VPN. */
internal class NetworkChangeState {
    private var known = false
    private var current: Long? = null
    var generation = 0L
        private set
    val available: Boolean get() = current != null
    @Synchronized fun selectedId() = current

    @Synchronized fun available(id: Long): Boolean {
        if (known && current == id) return false
        val changed = known
        known = true
        current = id
        if (changed) generation++
        return changed
    }

    @Synchronized fun lost(id: Long): Boolean {
        if (!known || current != id) return false
        current = null
        generation++
        return true
    }

    @Synchronized fun snapshot() = generation to available
}

/** Исключаем виртуальную сеть из запроса: она переживает смену Wi-Fi/LTE. */
internal class NetworkMonitor(context: Context, private val changed: (Boolean) -> Unit) {
    private val manager = context.getSystemService(ConnectivityManager::class.java)
    private val state = NetworkChangeState()
    @Volatile private var closed = false
    private var registered = false
    private val candidates = linkedMapOf<Long, Int>()
    private val callback = object : ConnectivityManager.NetworkCallback() {
        override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
            // VALIDATED не требуем: системная проверка интернета бывает
            // заблокирована, даже когда сервер VPN доступен.
            if (!closed && caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN) &&
                caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)) {
                if (Build.VERSION.SDK_INT >= 31) {
                    if (state.available(network.networkHandle)) changed(true)
                } else {
                    // На Android 7–11 нет best-matching callback. Сохраняем
                    // рабочую сеть при появлении/потере запасной мобильной.
                    candidates[network.networkHandle] =
                        (if (caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)) 100 else 0) +
                        (if (caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET)) 20
                        else if (caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)) 10 else 0)
                    selectLegacy()
                }
            }
        }

        override fun onLost(network: Network) {
            if (closed) return
            if (Build.VERSION.SDK_INT >= 31) {
                if (state.lost(network.networkHandle)) changed(false)
            } else {
                candidates.remove(network.networkHandle)
                selectLegacy()
            }
        }
    }

    fun start() {
        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN).build()
        if (Build.VERSION.SDK_INT >= 31) {
            manager.registerBestMatchingNetworkCallback(request, callback, Handler(Looper.getMainLooper()))
        } else {
            manager.registerNetworkCallback(request, callback)
        }
        registered = true
    }
    fun snapshot() = state.snapshot()
    private fun selectLegacy() {
        val previous = state.selectedId()
        val best = candidates.entries.maxWithOrNull(compareBy<Map.Entry<Long, Int>> { it.value }.thenBy { it.key == previous })?.key
        if (best != null) {
            if (state.available(best)) changed(true)
        } else if (previous != null && state.lost(previous)) changed(false)
    }
    fun close() {
        closed = true
        if (registered) manager.unregisterNetworkCallback(callback)
        registered = false
    }
}
