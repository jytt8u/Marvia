package io.marvia.android

import android.app.Application
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import java.io.File

/**
 * MarviaApp — точка входа процесса: ловит падения.
 *
 * Падение на чужом телефоне — это «закрыто из-за ошибки» и ни строчки о
 * том, где. Поэтому необработанное исключение сначала записывается в файл,
 * и только потом уходит системе. При следующем запуске первая строка
 * стека попадает в журнал, а весь файл — в отчёт продавцу: так человек
 * присылает причину одним нажатием, а не пересказывает, что видел.
 */
class MarviaApp : Application() {
    override fun onCreate() {
        super.onCreate()
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { thread, error ->
            runCatching {
                crashFile(this).writeText(
                    java.text.SimpleDateFormat("yyyy-MM-dd HH:mm:ss", java.util.Locale.US).format(java.util.Date()) +
                        " · " + thread.name + "\n" + android.util.Log.getStackTraceString(error),
                )
            }
            previous?.uncaughtException(thread, error) ?: kotlin.system.exitProcess(2)
        }
        // Прошлое падение — в журнал, чтобы оно было видно и до отчёта.
        val last = runCatching { crashFile(this).takeIf { it.exists() }?.readText() }.getOrNull()
        if (!last.isNullOrBlank()) {
            Journal.add(getString(R.string.log_crashed, last.lineSequence().drop(1).firstOrNull().orEmpty().trim()), Journal.Level.ERROR)
        }
        SubscriptionRefresh.sync(this, Store(this).subscriptions.isNotEmpty())

        // Виджет на рабочем столе узнаёт о туннеле отсюда: по расписанию он
        // не будится (widget_marvia_info.xml), а служба и окно меняют
        // состояние в этом же процессе.
        kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.SupervisorJob() + kotlinx.coroutines.Dispatchers.Main).launch {
            MarviaState.state
                .map { MarviaWidget.key(it) }
                .distinctUntilChanged()
                .collect { MarviaWidget.paintAll(this@MarviaApp) }
        }
    }

    companion object {
        fun crashFile(context: android.content.Context): File = File(context.filesDir, "crash.txt")
    }
}
