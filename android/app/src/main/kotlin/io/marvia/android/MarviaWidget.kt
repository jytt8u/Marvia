package io.marvia.android

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.widget.RemoteViews

/**
 * MarviaWidget — виджет на рабочем столе: состояние туннеля и включение
 * одним нажатием, не открывая приложения.
 *
 * Нажатие ведёт туда, где его можно выполнить. Когда туннель можно поднять
 * без окна — в TunnelSwitch; когда нельзя (нет ключа, нет разрешения на
 * VPN) — сразу в приложение. Решаем это заранее, при отрисовке: из фона
 * Android окно уже не откроет, а нажатие, которое ничего не делает, хуже
 * нажатия, открывшего приложение.
 */
class MarviaWidget : AppWidgetProvider() {

    override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) {
        paint(context, manager, ids)
    }

    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != ACTION_TOGGLE) {
            super.onReceive(context, intent)
            return
        }
        if (!TunnelSwitch.toggle(context)) {
            // Разрешение отозвали уже после отрисовки: пробуем открыть окно.
            runCatching { context.startActivity(openApp(context)) }
        }
        paintAll(context)
    }

    companion object {
        private const val ACTION_TOGGLE = "io.marvia.android.WIDGET_TOGGLE"

        /**
         * key — то, что виджет показывает. Перерисовываем, только когда оно
         * сменилось: отклик и скорость меняются каждые пару секунд, а виджет
         * их не показывает, и будить рабочий стол ради них незачем.
         */
        fun key(state: TunnelState): String = when (state) {
            is TunnelState.On -> "on:" + state.node
            is TunnelState.Connecting -> "connecting"
            is TunnelState.Failed -> "failed"
            else -> "off"
        }

        /** paintAll перерисовывает все виджеты Marvia на рабочих столах. */
        fun paintAll(context: Context) {
            val manager = AppWidgetManager.getInstance(context) ?: return
            val ids = manager.getAppWidgetIds(ComponentName(context, MarviaWidget::class.java))
            if (ids.isNotEmpty()) paint(context, manager, ids)
        }

        private fun paint(context: Context, manager: AppWidgetManager, ids: IntArray) {
            val state = MarviaState.state.value
            val on = state is TunnelState.On || state is TunnelState.Connecting
            val (title, detail) = when (state) {
                is TunnelState.On -> R.string.status_on to state.node.substringBefore('·').trim()
                is TunnelState.Connecting -> R.string.status_connecting to ""
                is TunnelState.Failed -> R.string.status_failed to context.getString(R.string.widget_tap)
                else -> R.string.status_off to context.getString(R.string.widget_tap)
            }
            val views = RemoteViews(context.packageName, R.layout.widget_marvia)
            views.setTextViewText(R.id.widgetStatus, context.getString(title))
            views.setTextViewText(R.id.widgetDetail, detail)
            views.setImageViewResource(R.id.widgetDot, if (on) R.drawable.widget_dot_on else R.drawable.widget_dot_off)
            val tap = if (on || TunnelSwitch.ready(context)) {
                PendingIntent.getBroadcast(
                    context, 0, Intent(context, MarviaWidget::class.java).setAction(ACTION_TOGGLE),
                    PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
                )
            } else {
                PendingIntent.getActivity(
                    context, 0, openApp(context),
                    PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
                )
            }
            views.setOnClickPendingIntent(R.id.widgetRoot, tap)
            manager.updateAppWidget(ids, views)
        }

        private fun openApp(context: Context) =
            Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    }
}
