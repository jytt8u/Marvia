package io.marvia.android

import android.net.IpPrefix
import android.os.Build
import androidx.annotation.RequiresApi
import java.net.Inet4Address
import java.net.InetAddress
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/**
 * CustomRoutes — свои адреса мимо туннеля.
 *
 * Российских подсетей бывает мало: рабочий сервер, роутер провайдера, игра,
 * у которой сервер в Москве. Человек вписывает адрес, подсеть или имя сайта, и
 * этот трафик идёт напрямую — тем же исключением маршрутов, что и российские
 * подсети, то есть с Android 13.
 *
 * Имя сайта превращается в адреса при подключении. Это честно не «сайт», а
 * его адреса на ту минуту: у больших сайтов за CDN адреса меняются, и часть
 * запросов уйдёт в туннель. Так и пишем под полем.
 */
object CustomRoutes {

    /** Сколько строк принимаем: больше — уже не «свои адреса», а список, и он замедлит подключение. */
    const val MAX = 200

    /**
     * Самые широкие сети, которые можно увести мимо туннеля. «0.0.0.0/0» в
     * поле выключил бы VPN целиком, а человек думал бы, что он работает.
     */
    private const val MIN_V4 = 8
    private const val MIN_V6 = 16

    sealed interface Entry {
        data class Net(val address: String, val length: Int, val v6: Boolean) : Entry
        data class Host(val name: String) : Entry
    }

    /** bad — что не разобралось, как человек это вписал; skipped — сколько верных строк не влезло в MAX. */
    data class Parsed(val entries: List<Entry>, val bad: List<String>, val skipped: Int = 0)

    private val v4 = Regex("""^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$""")
    private val domain = Regex("""^(?=.{1,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,62}$""")

    /** parse разбирает поле: по строке, запятой или пробелу; пустое и «#…» пропускает. */
    fun parse(text: String): Parsed {
        val entries = mutableListOf<Entry>()
        val bad = mutableListOf<String>()
        // Сначала строки и комментарии, потом разделители: иначе «# рабочий
        // сервер» распался бы на слова и попал в «не понял».
        val items = text.lineSequence()
            .map { it.substringBefore('#') }
            .flatMap { it.split(',', ' ', '\t', ';').asSequence() }
        for (raw in items) {
            val item = raw.trim()
            if (item.isEmpty()) continue
            val entry = entry(item)
            if (entry == null) bad += item else if (entry !in entries) entries += entry
        }
        return Parsed(entries.take(MAX), bad, maxOf(0, entries.size - MAX))
    }

    private fun entry(item: String): Entry? {
        // Ссылку вставляют целиком — из неё нужно только имя.
        var s = item.lowercase().substringAfter("://").substringBefore('/').substringBefore('?')
        val slash = item.indexOf('/')
        var length = -1
        if (!item.contains("://") && slash >= 0) {
            s = item.substring(0, slash).lowercase()
            length = item.substring(slash + 1).toIntOrNull() ?: return null
        }
        // IPv6 в ссылке пишут в скобках, с портом и без: https://[2001:db8::1]:443/.
        if (s.startsWith("[")) {
            if (!s.contains(']')) return null
            s = s.substring(1, s.indexOf(']'))
        }
        v4.matchEntire(s)?.let { m ->
            if (m.groupValues.drop(1).any { it.toInt() > 255 }) return null
            val len = if (length < 0) 32 else length
            if (len !in MIN_V4..32) return null
            return Entry.Net(s, len, v6 = false)
        }
        if (s.contains(':')) {
            // Только буквальный IPv6: ни одной буквы вне шестнадцатеричных,
            // чтобы разбор не пошёл в сеть за «именем».
            if (!s.all { it.isDigit() || it in 'a'..'f' || it == ':' || it == '.' }) {
                // «host:port» — порт тут не к месту, а имя примем.
                val host = s.substringBefore(':')
                return if (length < 0 && domain.matches(host)) Entry.Host(host) else null
            }
            val parsed = runCatching { InetAddress.getByName(s) }.getOrNull() ?: return null
            if (parsed is Inet4Address) return null
            val len = if (length < 0) 128 else length
            if (len !in MIN_V6..128) return null
            return Entry.Net(parsed.hostAddress ?: s, len, v6 = true)
        }
        if (length >= 0) return null
        return if (domain.matches(s)) Entry.Host(s) else null
    }

    /**
     * prefixes — маршруты для исключения. Имена разрешаются здесь, при
     * подключении, с общим пределом времени: недоступный DNS не должен
     * задерживать подключение дольше пары секунд.
     */
    @RequiresApi(Build.VERSION_CODES.TIRAMISU)
    fun prefixes(entries: List<Entry>, timeoutMs: Long = 2500): List<IpPrefix> {
        val out = mutableListOf<IpPrefix>()
        val hosts = entries.filterIsInstance<Entry.Host>()
        for (net in entries.filterIsInstance<Entry.Net>()) {
            runCatching { out += IpPrefix(InetAddress.getByName(net.address), net.length) }
        }
        if (hosts.isEmpty()) return out
        // Потоки — фоновые: запрос имени системой не прерывается, и зависший
        // DNS не должен держать процесс. Подключение их не ждёт — у него свой срок.
        val pool = Executors.newFixedThreadPool(minOf(hosts.size, 4)) { r -> Thread(r, "custom-routes-dns").apply { isDaemon = true } }
        try {
            val futures = hosts.map { h -> pool.submit<List<InetAddress>> { InetAddress.getAllByName(h.name).toList() } }
            val deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs)
            for (f in futures) {
                val left = deadline - System.nanoTime()
                val addresses = runCatching { f.get(maxOf(left, 0), TimeUnit.NANOSECONDS) }.getOrNull() ?: continue
                for (a in addresses) {
                    runCatching { out += IpPrefix(a, if (a is Inet4Address) 32 else 128) }
                }
            }
        } finally {
            pool.shutdownNow()
        }
        return out.distinct()
    }
}
