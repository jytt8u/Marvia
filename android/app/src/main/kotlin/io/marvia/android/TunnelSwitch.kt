package io.marvia.android

import android.content.Context
import android.content.Intent
import android.net.VpnService
import androidx.core.content.ContextCompat

/**
 * TunnelSwitch — «нажал — включилось, нажал — выключилось» без окна
 * приложения: для плитки в шторке и виджета на рабочем столе.
 *
 * Одна логика на всех, а не своя у каждой кнопки: иначе плитка и виджет
 * разошлись бы на первой же правке — например, одна научилась бы
 * останавливать подключение посреди попытки, а другая нет.
 */
object TunnelSwitch {
    /**
     * toggle переключает туннель. false — сделать это без окна нельзя: нет
     * ключа, система ещё не давала разрешения на VPN или не дала поднять
     * службу. Тогда вызывающий открывает приложение — спросить разрешение
     * умеет только экран.
     */
    fun toggle(context: Context): Boolean {
        val state = MarviaState.state.value
        if (state is TunnelState.On || state is TunnelState.Connecting) {
            context.startService(Intent(context, MarviaVpnService::class.java).setAction(MarviaVpnService.ACTION_STOP))
            return true
        }
        if (!ready(context)) return false
        return try {
            ContextCompat.startForegroundService(context, Intent(context, MarviaVpnService::class.java))
            true
        } catch (_: Throwable) {
            // Часть прошивок не даёт поднять службу из фона даже по нажатию.
            false
        }
    }

    /** ready — можно ли включить туннель без окна: есть ключ и разрешение на VPN. */
    fun ready(context: Context): Boolean =
        Store(context).accountLink.isNotBlank() && VpnService.prepare(context) == null
}
