package io.marvia.android

import android.content.Context
import android.graphics.Typeface
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView
import androidx.core.content.res.ResourcesCompat
import androidx.core.view.isVisible
import androidx.core.view.updateLayoutParams
import io.marvia.android.databinding.ScreenConnectBinding
import org.json.JSONObject

/**
 * Раскладка главной. Выбирается отдельно от темы: тема красит, а вид
 * главной человек берёт сам. Раньше образ решал и композицию, и смена цвета
 * заодно переставляла кнопку и прятала круг — это раздражало сильнее, чем
 * радовало разнообразие. По умолчанию — наша главная Marvia: кнопка,
 * состояние и строка ноды; карточки «где вы» и подписки — у «Сцены».
 */
enum class HomeLayout(val key: String) {
    MARVIA("marvia"), STAGE("stage"), DECK("deck"), CONSOLE("console");

    /** Карточки «где вы» и подписки — у сцены и панели; у Marvia строка ноды, у консоли строки-показания. */
    val cards: Boolean get() = this == STAGE || this == DECK

    companion object {
        fun of(key: String): HomeLayout = entries.firstOrNull { it.key == key } ?: MARVIA
    }
}

/**
 * HomeBlocks — общие блоки главной: «где вы», подписка и строки консоли.
 * Данные одни на все раскладки: раскладка меняет подачу, а не то, что
 * человек о себе узнаёт.
 */
object HomeBlocks {

    /**
     * Plan — срок и остаток рабочей подписки, как их последний раз прислал
     * продавец. Читается из кэша ядра: главной сеть ради этого не нужна, а
     * свежесть — забота «Серверов», где подписка и обновляется.
     */
    data class Plan(
        val name: String,
        val until: String,
        val limit: Long,
        val left: Long,
        val renew: String,
    )

    /** Где трафик сейчас или шёл в прошлый раз; пустое имя — автовыбор. */
    data class Place(val title: String, val ms: Long, val live: Boolean, val manual: Boolean)

    /**
     * pickedHere — идёт ли трафик через ту ноду, которую человек выбрал сам.
     * Если выбранная замолчала и ядро уехало на другую, приписать выбор той
     * значило бы соврать; об отъезде говорит строка под состоянием.
     */
    fun pickedHere(chosen: String, node: String) = chosen.isNotEmpty() && chosen == node

    fun parsePlan(name: String, json: String): Plan? {
        val o = try { JSONObject(json) } catch (_: Throwable) { return null }
        return Plan(
            name = name,
            until = o.optString("until", ""),
            limit = o.optLong("limit", 0),
            left = o.optLong("left", 0),
            renew = o.optString("renew_url", ""),
        )
    }

    /** Флаг страны из названия ноды; не узнали — пусто, а не чужой флаг. */
    fun flagFor(title: String): String {
        val country = title.substringBefore('·').trim()
        return Flags.of(CountryGuess.fromName(title).ifEmpty { country }).ifEmpty { "" }
    }

    fun place(c: ScreenConnectBinding, p: Place, t: Theme) {
        val ctx = c.root.context
        val auto = p.title.isEmpty()
        val flag = if (auto) "" else flagFor(p.title)
        c.placeFlag.text = flag
        c.placeIcon.isVisible = flag.isEmpty()
        c.placeIcon.setImageResource(if (auto) R.drawable.ic_bolt else R.drawable.ic_globe)
        // Ядро называет ноду «страна · город · имя»: крупно — где вы, а
        // служебное имя вроде «fi-1» — в подпись, иначе оно съедает место
        // у города и в плитке обрезает его.
        val parts = p.title.split(" · ")
        val node = if (parts.size >= 3) parts.last() else ""
        c.placeName.text = if (auto) ctx.getString(R.string.home_place_auto) else if (node.isEmpty()) p.title else parts.dropLast(1).joinToString(" · ")
        val status = when {
            auto -> ctx.getString(R.string.home_place_auto_sub)
            p.live && p.manual -> ctx.getString(R.string.home_place_live_manual)
            p.live -> ctx.getString(R.string.home_place_live)
            else -> ctx.getString(R.string.home_place_last)
        }
        c.placeSub.text = listOf(node, status).filter { it.isNotEmpty() }.joinToString(" · ")
        c.placeMs.isVisible = p.live && p.ms > 0
        c.placeMs.text = ctx.getString(R.string.node_ms, p.ms)
        // Отклик цветом по качеству: число без оценки ничего не говорит
        // человеку, который не знает, много ли это — 180 мс.
        c.placeMs.setTextColor(when {
            p.ms in 1..120 -> t.acc
            p.ms in 121..300 -> t.warn
            else -> t.fail
        })
    }

    fun plan(c: ScreenConnectBinding, plan: Plan?, t: Theme, layout: HomeLayout) {
        val ctx = c.root.context
        c.planCard.isVisible = plan != null && layout.cards
        if (plan == null) return
        c.planName.text = plan.name
        val days = if (plan.until.isEmpty()) null else Format.daysLeft(plan.until)
        when {
            days != null -> {
                c.planDays.text = days.coerceAtLeast(0).toString()
                c.planDaysWord.text = ctx.resources.getQuantityString(R.plurals.home_days_word, days.coerceAtLeast(0))
            }
            else -> {
                c.planDays.text = "∞"
                c.planDaysWord.text = ctx.getString(R.string.servers_no_term)
            }
        }
        // Меньше трёх дней — предупреждением: это и есть повод открыть
        // главную и нажать «Продлить».
        c.planDays.setTextColor(if (days != null && days <= 3) t.warn else t.fg)
        val quota = plan.limit > 0
        c.planTrack.isVisible = quota
        c.planLeft.text = if (quota) ctx.getString(R.string.home_plan_left, Format.size(ctx, plan.left), Format.size(ctx, plan.limit))
        else ctx.getString(R.string.home_plan_unlimited)
        val dp = ctx.resources.displayMetrics.density
        c.planTrack.background = Paint.rounded(t.shade, 999, dp)
        c.planTrack.clipToOutline = true
        c.planFill.background = Paint.rounded(t.acc, 999, dp)
        if (quota) c.planTrack.post {
            val share = (plan.left.toFloat() / plan.limit).coerceIn(0f, 1f)
            c.planFill.updateLayoutParams { width = (c.planTrack.width * share).toInt() }
        }
        c.planRenew.isVisible = SellerChannel.openable(plan.renew)
    }

    /**
     * Строки консоли: те же сведения, что в карточках, но столбиком, как
     * вывод команды. Пересобираются целиком: строк пять, и сверять каждую
     * дороже, чем нарисовать заново.
     */
    fun readout(c: ScreenConnectBinding, rows: List<Pair<String, String>>, t: Theme) {
        val box = c.readout
        box.removeAllViews()
        val ctx = c.root.context
        val mono: Typeface? = ResourcesCompat.getFont(ctx, R.font.mono)
        val dp = ctx.resources.displayMetrics.density
        for ((key, value) in rows) {
            val row = LinearLayout(ctx).apply {
                orientation = LinearLayout.HORIZONTAL
                setPadding(0, (5 * dp).toInt(), 0, (5 * dp).toInt())
            }
            row.addView(TextView(ctx).apply {
                text = key.uppercase()
                typeface = mono
                textSize = 11f
                letterSpacing = 0.1f
                setTextColor(t.dim)
                layoutParams = LinearLayout.LayoutParams((92 * dp).toInt(), ViewGroup.LayoutParams.WRAP_CONTENT)
            })
            row.addView(TextView(ctx).apply {
                text = value
                typeface = mono
                textSize = 13f
                setTextColor(t.fg)
                layoutParams = LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f)
            })
            box.addView(row)
        }
    }

    /**
     * arrange переставляет общие вьюхи под раскладку. Вызывается при каждой
     * покраске: образ сменили — сцена перестроилась без перезапуска.
     */
    fun arrange(c: ScreenConnectBinding, layout: HomeLayout) {
        val dp = c.root.resources.displayMetrics.density
        fun px(v: Int) = (v * dp).toInt()
        val deck = layout == HomeLayout.DECK
        val console = layout == HomeLayout.CONSOLE

        // Слово состояния: на панели — слева в шапке, на прочих — под сценой.
        move(c.statusBox, if (deck) c.deckLeft else c.homeColumn, if (deck) 0 else c.homeColumn.indexOfChild(c.deckRow) + 1)
        move(c.powerAction, if (deck) c.deckSlot else c.stageFrame, -1)
        c.deckRow.isVisible = deck
        c.stageFrame.isVisible = !deck && !console
        val start = deck || console
        c.statusBox.gravity = if (start) Gravity.START else Gravity.CENTER_HORIZONTAL
        c.statusText.gravity = if (start) Gravity.START else Gravity.CENTER
        c.powerHint.gravity = if (start) Gravity.START else Gravity.CENTER
        c.nodeNote.gravity = if (start) Gravity.START else Gravity.CENTER
        c.techText.gravity = if (start) Gravity.START else Gravity.CENTER
        // Моноширинный Paint потом не трогает (узнаёт по ширине букв), поэтому,
        // уходя из консоли, возвращаем обычный: шрифт образа он поставит сам.
        c.statusText.typeface = if (console) Typeface.create(ResourcesCompat.getFont(c.root.context, R.font.mono), Typeface.BOLD)
        else Typeface.DEFAULT_BOLD
        if (deck) {
            c.statusText.textSize = 34f
            c.powerAction.updateLayoutParams<FrameLayout.LayoutParams> { width = px(108); height = px(108); gravity = Gravity.CENTER }
        } else {
            c.powerAction.updateLayoutParams<FrameLayout.LayoutParams> { gravity = Gravity.CENTER }
        }
        if (console) c.statusText.textSize = 30f

        // Строка ноды — у главной Marvia; остальным «где вы» говорит карточка.
        c.nodeLine.isVisible = layout == HomeLayout.MARVIA && c.nodeLine.text.isNotEmpty()
        c.readout.isVisible = console
        c.consoleAction.isVisible = console

        // На панели «где вы» и подписка — плитками рядом, на сцене — столбиком.
        c.infoRow.orientation = if (deck) LinearLayout.HORIZONTAL else LinearLayout.VERTICAL
        // На панели шапка и плитки идут подряд: распорка, что на сцене
        // прижимает карточки к низу, здесь оставила бы дыру под шапкой.
        if (deck || console) {
            c.statsGap.updateLayoutParams<LinearLayout.LayoutParams> { height = px(14); weight = 0f }
            c.statsGap.minimumHeight = 0
        }
        c.placeCard.updateLayoutParams<LinearLayout.LayoutParams> {
            // Плитки одной высоты: рядом стоящие карточки разного роста
            // выглядят как ошибка вёрстки.
            height = if (deck) ViewGroup.LayoutParams.MATCH_PARENT else ViewGroup.LayoutParams.WRAP_CONTENT
            width = if (deck) 0 else ViewGroup.LayoutParams.MATCH_PARENT
            weight = if (deck) 1f else 0f
            marginEnd = if (deck) px(6) else 0
        }
        c.planCard.updateLayoutParams<LinearLayout.LayoutParams> {
            width = if (deck) 0 else ViewGroup.LayoutParams.MATCH_PARENT
            weight = if (deck) 1f else 0f
            marginStart = if (deck) px(6) else 0
            topMargin = if (deck) 0 else px(12)
        }
        // В плитке на полэкрана стрелка и число отклика не помещаются рядом с
        // названием — плитка и так целиком нажимается.
        c.placeChevron.isVisible = !deck
        c.placeName.maxLines = if (deck) 2 else 1
        c.placeCard.orientation = if (deck) LinearLayout.VERTICAL else LinearLayout.HORIZONTAL
        c.placeCard.gravity = if (deck) Gravity.START or Gravity.CENTER_VERTICAL else Gravity.CENTER_VERTICAL
        (c.placeName.parent as View).updateLayoutParams<LinearLayout.LayoutParams> {
            width = if (deck) ViewGroup.LayoutParams.MATCH_PARENT else 0
            weight = if (deck) 0f else 1f
            marginStart = if (deck) 0 else px(12)
            topMargin = if (deck) px(10) else 0
        }
        c.placeMs.updateLayoutParams<LinearLayout.LayoutParams> {
            marginStart = if (deck) 0 else px(8)
            topMargin = if (deck) px(6) else 0
        }
        c.tilesRow.isVisible = c.tilesRow.isVisible && !console
    }

    private fun move(v: View, parent: ViewGroup, index: Int) {
        val from = v.parent as? ViewGroup
        if (from === parent) return
        from?.removeView(v)
        if (index < 0 || index > parent.childCount) parent.addView(v) else parent.addView(v, index)
    }

    /** Подпись кнопки консоли — что она сделает, а не в каком мы состоянии. */
    fun consoleLabel(ctx: Context, state: TunnelState, hasKey: Boolean): String = when (state) {
        TunnelState.Off -> if (hasKey) ctx.getString(R.string.home_console_connect) else ctx.getString(R.string.connect_add_key)
        TunnelState.Connecting -> ctx.getString(R.string.home_console_cancel)
        is TunnelState.On -> ctx.getString(R.string.home_console_disconnect)
        is TunnelState.Failed -> ctx.getString(R.string.home_console_retry)
    }.uppercase()
}
