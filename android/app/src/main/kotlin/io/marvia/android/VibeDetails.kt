package io.marvia.android

import android.graphics.Canvas
import android.graphics.ColorFilter
import android.graphics.LinearGradient
import android.graphics.Paint
import android.graphics.PixelFormat
import android.graphics.Rect
import android.graphics.RectF
import android.graphics.Shader
import android.graphics.Typeface
import android.graphics.drawable.Drawable
import android.graphics.drawable.GradientDrawable
import android.graphics.drawable.LayerDrawable
import android.os.SystemClock
import android.view.Gravity
import android.view.View
import android.widget.TextView
import androidx.core.graphics.ColorUtils

/**
 * Детали образов — то, по чему узнают саму систему, а не только её цвет:
 * слюда и полоска выбора Windows 11, синяя панель задач и зелёная кнопка XP,
 * выпуклые серые окна 98-й, стекло macOS, карточки GNOME, голубой фокус
 * Breeze, плитки с цветной рамкой у Hyprland, Nord и Gruvbox. Те же, что в
 * окне на ПК (блок «Образы в духе систем» в app.html), насколько их
 * позволяет нарисовать Android без размытия по маске: на старых телефонах
 * оно рисуется только программно, и одна такая карточка роняла бы плавность
 * всего экрана. Поэтому стекло здесь — полупрозрачная пластина над обоями,
 * а не размытие.
 *
 * Тема о деталях не знает: они зависят от образа, а не от палитры. Пустой
 * образ — деталей нет, экран такой же, как до их появления. Деталь поля —
 * карточки, кнопки питания — держится, пока человек не выбрал в редакторе
 * своё: иначе «Карточки» и «Кнопка» на этих образах ничего бы не меняли.
 */
object VibeDetails {
    private const val WHITE = 0xFFFFFFFF.toInt()
    private const val BLACK = 0xFF000000.toInt()

    /** Серый и синий Windows 98, синий заголовок XP — цвета систем, как в CSS на ПК. */
    const val SILVER = 0xFFC0C0C0.toInt()
    const val NAVY = 0xFF000080.toInt()
    private const val NAVY_LIGHT = 0xFF1084D0.toInt()
    private const val XP_PANEL_TOP = 0xFF5B82DA.toInt()
    private const val XP_PANEL_BOTTOM = 0xFF3D56C6.toInt()
    private const val XP_INK = 0xFF0B3FA5.toInt()
    private const val XP_EDGE = 0xFF003C74.toInt()

    /** Высота синей полоски заголовка у карточек «Windows 98», dp. */
    const val TITLE = 18

    fun profile(vibe: String): AppearanceProfiles.Profile? = AppearanceProfiles.byId(vibe)

    /** Оболочка образа; пусто — образа нет. */
    fun shell(vibe: String): String = profile(vibe)?.shell.orEmpty()

    /** Карточки образа в силе: образ в духе системы, и подачу карточек человек не менял. */
    private fun ownCards(vibe: String, t: Theme): AppearanceProfiles.Profile? =
        profile(vibe)?.takeIf { it.palette != null && it.choice.card == t.card }

    private fun alpha(color: Int, a: Double) = ColorUtils.setAlphaComponent(color, (a * 255).toInt())

    private fun plate(fill: Int, r: Float, line: Int, width: Int) = GradientDrawable().apply {
        setColor(fill)
        cornerRadius = r
        if (line != 0) setStroke(width.coerceAtLeast(1), line)
    }

    // ------------------------------------------------------------ карточки

    /**
     * Карточка образа вместо обычной; null — у образа своей нет.
     * Главная карточка (`feature`) — активное окно: у XP синяя кромка
     * сверху, у KDE рамка фокуса, у плиточных цветная рамка.
     */
    fun card(vibe: String, t: Theme, dp: Float, feature: Boolean): Drawable? {
        val p = ownCards(vibe, t) ?: return null
        val r = t.r * dp
        val hair = dp.toInt()
        return when (p.shell) {
            // Слюда: поверхность на просвет и волосяная кайма текстом.
            "win11" -> plate(alpha(t.surf, if (feature) .82 else .72), r, alpha(t.fg, .08), hair)
            "mac" -> plate(alpha(t.surf, .70), r, alpha(t.fg, .09), hair)
            "xp" -> if (!feature) plate(t.surf, r, t.line, hair) else LayerDrawable(arrayOf(
                plate(t.surf, r, t.line, hair),
                GradientDrawable().apply { setColor(t.acc); cornerRadii = floatArrayOf(r, r, r, r, 0f, 0f, 0f, 0f) },
            )).apply {
                setLayerGravity(1, Gravity.TOP or Gravity.FILL_HORIZONTAL)
                setLayerHeight(1, (3 * dp).toInt())
            }
            "w98" -> Bevel(t.surf, dp, raised = true, title = true)
            "gnome" -> plate(t.surf, r, 0, 0)
            "ubuntu" -> plate(alpha(t.surf, .90), r, alpha(t.fg, .07), hair)
            // Breeze: рамка обводкой, у главной — голубым фокусом.
            "kde" -> plate(alpha(t.surf, .92), r, if (feature) t.acc else t.line, hair)
            // Плитки: серая рамка у остальных, цветная — у активной.
            AppearanceProfiles.TILE -> {
                val tile = p.tile
                if (feature && tile != null) Edge(t.surf, r, 2 * dp, tile) else plate(t.surf, r, t.line, (2 * dp).toInt())
            }
            else -> null
        }
    }

    /** cardTop — запас сверху под полоску заголовка «Windows 98», dp; у остальных 0. */
    fun cardTop(vibe: String, t: Theme): Int = if (ownCards(vibe, t)?.shell == "w98") TITLE else 0

    /**
     * lift — тень карточки системной elevation: мягкая у GNOME, едва
     * заметная у XP. Полупрозрачным пластинам слюды и стекла её не даём:
     * тень проступила бы сквозь них грязным пятном. Цвет тени — чёрный:
     * установка могла помнить розовую тень прежнего «Пластилина».
     */
    fun lift(vibe: String, v: View, dp: Float, t: Theme): Boolean {
        if (android.os.Build.VERSION.SDK_INT >= 28) {
            v.outlineSpotShadowColor = BLACK
            v.outlineAmbientShadowColor = BLACK
        }
        val height = when (ownCards(vibe, t)?.shell) {
            "gnome" -> 2f
            "xp" -> 1f
            else -> return false
        }
        v.elevation = height * dp
        return true
    }

    // ----------------------------------------------------- нижняя панель

    /** Нижняя панель — то, чем у системы служит панель задач или боковая; null — обычная. */
    fun navbar(vibe: String, t: Theme, dp: Float): Drawable? {
        val p = profile(vibe)?.takeIf { it.palette != null } ?: return null
        return when (p.shell) {
            "win11" -> plate(alpha(t.surf, .78), 8 * dp, alpha(t.fg, .08), dp.toInt())
            "mac" -> plate(alpha(t.bg, .62), 22 * dp, alpha(t.fg, .12), dp.toInt())
            "xp" -> GradientDrawable(GradientDrawable.Orientation.TOP_BOTTOM, intArrayOf(XP_PANEL_TOP, XP_PANEL_BOTTOM)).apply { cornerRadius = 6 * dp }
            "w98" -> Bevel(SILVER, dp, raised = true, title = false)
            "gnome", "ubuntu" -> plate(alpha(t.bg, .82), 16 * dp, 0, 0)
            "kde" -> plate(alpha(t.bg, .88), 4 * dp, t.line, dp.toInt())
            AppearanceProfiles.TILE -> plate(alpha(t.bg, .72), t.r * dp, t.line, (2 * dp).toInt())
            else -> null
        }
    }

    /** Вкладка панели: подложка под значком, цвет значка и подписи. */
    class Tab(val pill: Drawable?, val icon: Int, val label: Int)

    /**
     * tab — выбранная и невыбранная вкладка в духе системы: полоска акцента
     * под значком Windows 11, белая плашка на синей панели XP, тёмно-синяя
     * 98-й, рамка фокуса Breeze, залитая капсула waybar. null — обычная.
     */
    fun tab(vibe: String, t: Theme, dp: Float, active: Boolean): Tab? {
        val p = profile(vibe)?.takeIf { it.palette != null } ?: return null
        fun soft(a: Double, r: Int) = if (active) plate(alpha(t.fg, a), r * dp, 0, 0) else null
        val plain = if (active) t.fg else t.dim
        return when (p.shell) {
            "win11" -> Tab(if (!active) null else LayerDrawable(arrayOf(
                plate(alpha(t.fg, .07), 4 * dp, 0, 0),
                GradientDrawable().apply { setColor(t.acc); cornerRadius = 2 * dp },
            )).apply {
                setLayerGravity(1, Gravity.BOTTOM or Gravity.CENTER_HORIZONTAL)
                setLayerSize(1, (16 * dp).toInt(), (3 * dp).toInt())
            }, plain, plain)
            "mac" -> Tab(soft(.12, 10), plain, plain)
            "xp" -> Tab(if (active) plate(WHITE, 4 * dp, 0, 0) else null, if (active) XP_INK else WHITE, WHITE)
            "w98" -> Tab(if (active) plate(NAVY, 0f, 0, 0) else null, if (active) WHITE else BLACK, BLACK)
            "gnome" -> Tab(soft(.10, 6), plain, plain)
            "ubuntu" -> Tab(soft(.10, 6), if (active) t.acc else t.dim, plain)
            "kde" -> Tab(if (active) plate(alpha(t.acc, .26), 3 * dp, t.acc, dp.toInt()) else null, plain, plain)
            AppearanceProfiles.TILE -> Tab(if (active) plate(t.acc, 8 * dp, 0, 0) else null, if (active) t.accFg else t.dim, plain)
            else -> null
        }
    }

    // ------------------------------------------------------------- кнопки

    /** Поверхность кнопки или чипа и цвет текста на ней. */
    class Face(val bg: Drawable, val ink: Int)

    /**
     * button — главная кнопка образа: серая выпуклая у 98-й, светлая с
     * тёмно-синей кромкой у XP, у прочих — заливка акцентом со скруглением
     * своей системы. null — кнопка темы.
     */
    fun button(vibe: String, t: Theme, dp: Float): Face? = face(vibe, t, dp, primary = true)

    fun chip(vibe: String, t: Theme, dp: Float): Face? = face(vibe, t, dp, primary = false)

    private fun face(vibe: String, t: Theme, dp: Float, primary: Boolean): Face? {
        val p = profile(vibe)?.takeIf { it.palette != null } ?: return null
        val fill = if (primary) t.acc else t.surf2
        val ink = if (primary) t.accFg else t.dim
        return when (p.shell) {
            "xp" -> Face(GradientDrawable(GradientDrawable.Orientation.TOP_BOTTOM, intArrayOf(WHITE, 0xFFECEBE6.toInt(), 0xFFD6D0C5.toInt())).apply {
                cornerRadius = 3 * dp
                setStroke(dp.toInt().coerceAtLeast(1), XP_EDGE)
            }, t.fg)
            "w98" -> Face(Bevel(SILVER, dp, raised = true, title = false), t.fg)
            "win11" -> Face(plate(fill, 4 * dp, 0, 0), ink)
            "mac" -> Face(plate(fill, 999 * dp, 0, 0), ink)
            "gnome" -> Face(plate(if (primary) fill else alpha(t.fg, .08), 6 * dp, 0, 0), ink)
            "ubuntu" -> Face(plate(fill, 6 * dp, 0, 0), ink)
            "kde" -> Face(plate(fill, 3 * dp, 0, 0), ink)
            else -> null
        }
    }

    /**
     * desk — цвет текста прямо на рабочем столе, а не в окне; 0 — как в
     * карточках. Сейчас он свой только у «Windows 98»: белый на бирюзе.
     */
    fun desk(vibe: String): Int = profile(vibe)?.palette?.desk ?: 0

    // ------------------------------------------------------ надпись состояния

    /**
     * status — надпись под кнопкой в образе: красная точка «в эфире» у табло,
     * моноширинный шрифт бара у плиточных, белая на бирюзе у 98-й. Вызывается
     * при каждой смене состояния и снимает чужие детали, если образ сменился.
     */
    fun status(v: TextView, vibe: String, t: Theme, on: Boolean, motion: Boolean) {
        val size = v.textSize
        val mark: Drawable? = when (vibe) {
            "dot", "paper" -> Blink(if (on) t.acc else ColorUtils.setAlphaComponent(t.dim, 128), size, if (on && motion) 1600 else 0, 38)
            else -> null
        }
        v.setCompoundDrawablesRelativeWithIntrinsicBounds(mark, null, null, null)
        v.compoundDrawablePadding = if (mark == null) 0 else (size * .45f).toInt()
        val face = io.marvia.android.Paint.family(v.context, if (shell(vibe) == AppearanceProfiles.TILE) Store.FONT_MONO else io.marvia.android.Paint.style.font)
        v.setTypeface(face, Typeface.BOLD)
        // На ПК надпись 98-й белая при любом состоянии: цвет состояния на
        // бирюзе не читается, а само состояние сказано словами и кнопкой.
        desk(vibe).takeIf { it != 0 }?.let { v.setTextColor(it) }
        v.invalidate()
    }

    /**
     * Blink — точка у надписи, мигающая ступенькой: половину периода горит,
     * половину почти гаснет, без плавного перехода — как лампа записи. Сама
     * себя перепланирует через хозяина-вьюху и стоит, когда период нулевой
     * (подключения нет или анимации выключены).
     */
    private class Blink(color: Int, private val text: Float, private val period: Long, private val low: Int) : Drawable(), Runnable {
        private val brush = Paint(Paint.ANTI_ALIAS_FLAG).apply { this.color = color }
        private var lit = period <= 0 || (SystemClock.uptimeMillis() / (period / 2)) % 2 == 0L

        override fun getIntrinsicWidth() = (text * .32f).toInt().coerceAtLeast(1)
        override fun getIntrinsicHeight() = (text * .32f).toInt().coerceAtLeast(1)

        override fun draw(canvas: Canvas) {
            val b = bounds
            brush.alpha = if (lit) 255 else low
            canvas.drawCircle(b.exactCenterX(), b.exactCenterY(), b.width() / 2f, brush)
            if (period > 0) {
                unscheduleSelf(this)
                val half = period / 2
                scheduleSelf(this, SystemClock.uptimeMillis() + half - SystemClock.uptimeMillis() % half)
            }
        }

        override fun run() { lit = (SystemClock.uptimeMillis() / (period / 2)) % 2 == 0L; invalidateSelf() }
        override fun setAlpha(alpha: Int) { brush.alpha = alpha }
        override fun setColorFilter(filter: ColorFilter?) { brush.colorFilter = filter }
        @Deprecated("Deprecated in Java")
        override fun getOpacity(): Int = PixelFormat.TRANSLUCENT
    }

    // ---------------------------------------------------------- рисование

    /**
     * Bevel — выпуклая (или вдавленная) серая поверхность Windows 98: по
     * краю две пары линий, светлые сверху слева и тёмные снизу справа, как
     * четыре inset-тени в CSS на ПК. С заголовком — синяя полоска сверху,
     * карточка выглядит окном. Скругления нет: у 98-й его не было.
     */
    class Bevel(private val fill: Int, private val dp: Float, private val raised: Boolean, private val title: Boolean) : Drawable() {
        private val brush = Paint()
        private val strip = Paint()

        override fun onBoundsChange(bounds: Rect) {
            strip.shader = LinearGradient(bounds.left.toFloat(), 0f, bounds.right.toFloat(), 0f, NAVY, NAVY_LIGHT, Shader.TileMode.CLAMP)
        }

        override fun draw(canvas: Canvas) {
            val b = bounds
            if (b.isEmpty) return
            brush.color = fill
            canvas.drawRect(b, brush)
            if (title) canvas.drawRect(b.left.toFloat(), b.top.toFloat(), b.right.toFloat(), b.top + TITLE * dp, strip)
            val px = dp.coerceAtLeast(1f)
            // Внешняя пара, затем внутренняя: у выпуклой свет сверху слева.
            edge(canvas, b, 0f, px, if (raised) WHITE else 0xFF808080.toInt(), if (raised) 0xFF0A0A0A.toInt() else WHITE)
            edge(canvas, b, px, px, if (raised) 0xFFDFDFDF.toInt() else 0xFF0A0A0A.toInt(), if (raised) 0xFF808080.toInt() else 0xFFDFDFDF.toInt())
        }

        private fun edge(c: Canvas, b: Rect, inset: Float, w: Float, light: Int, dark: Int) {
            val l = b.left + inset
            val t = b.top + inset
            val r = b.right - inset
            val bt = b.bottom - inset
            brush.color = light
            c.drawRect(l, t, r - w, t + w, brush)
            c.drawRect(l, t, l + w, bt - w, brush)
            brush.color = dark
            c.drawRect(l, bt - w, r, bt, brush)
            c.drawRect(r - w, t, r, bt, brush)
        }

        override fun getOutline(outline: android.graphics.Outline) = outline.setRect(bounds)
        override fun setAlpha(alpha: Int) { brush.alpha = alpha; strip.alpha = alpha }
        override fun setColorFilter(filter: ColorFilter?) { brush.colorFilter = filter; strip.colorFilter = filter }
        @Deprecated("Deprecated in Java")
        override fun getOpacity(): Int = PixelFormat.OPAQUE
    }

    /**
     * Edge — карточка с цветной рамкой наискось, как активная плитка
     * Hyprland: заливка поверхностью и обводка градиентом из двух цветов
     * образа под 45°. GradientDrawable обводку градиентом не умеет.
     */
    class Edge(private val fill: Int, private val r: Float, private val width: Float, private val colors: IntArray) : Drawable() {
        private val face = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = fill }
        private val line = Paint(Paint.ANTI_ALIAS_FLAG).apply { style = Paint.Style.STROKE; strokeWidth = width; color = WHITE }
        private val box = RectF()

        override fun onBoundsChange(bounds: Rect) {
            // linear-gradient(45deg) идёт из левого нижнего угла в правый верхний.
            line.shader = LinearGradient(bounds.left.toFloat(), bounds.bottom.toFloat(), bounds.right.toFloat(), bounds.top.toFloat(), colors, null, Shader.TileMode.CLAMP)
            box.set(bounds)
            box.inset(width / 2, width / 2)
        }

        override fun draw(canvas: Canvas) {
            canvas.drawRoundRect(box, r, r, face)
            canvas.drawRoundRect(box, r, r, line)
        }

        override fun getOutline(outline: android.graphics.Outline) = outline.setRoundRect(bounds, r)
        override fun setAlpha(alpha: Int) { face.alpha = alpha; line.alpha = alpha }
        override fun setColorFilter(filter: ColorFilter?) { face.colorFilter = filter; line.colorFilter = filter }
        @Deprecated("Deprecated in Java")
        override fun getOpacity(): Int = PixelFormat.TRANSLUCENT
    }
}
