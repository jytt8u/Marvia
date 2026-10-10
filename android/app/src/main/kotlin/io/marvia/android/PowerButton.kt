package io.marvia.android

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Canvas
import android.graphics.LinearGradient
import android.graphics.Matrix
import android.graphics.Paint
import android.graphics.RadialGradient
import android.graphics.RectF
import android.graphics.Shader
import android.graphics.SweepGradient
import android.util.AttributeSet
import android.view.View
import android.view.animation.DecelerateInterpolator
import android.view.animation.LinearInterpolator
import androidx.appcompat.widget.AppCompatButton
import androidx.core.graphics.ColorUtils

/**
 * Кнопка подключения: точечное кольцо в теме Nothing, дуга в остальных.
 * Вращение работает только при подключении и останавливается вне экрана.
 */
class PowerButton @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : AppCompatButton(context, attrs) {
    enum class State { OFF, CONNECTING, ON, FAILED }

    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)

    var appearanceStyle: AppearanceStyle = AppearanceStyle.CLASSIC
        set(value) {
            if (field == value) return
            field = value
            morpher?.cancel()
            from = arc(state)
            to = from
            mix = 1f
            invalidate()
        }

    /**
     * Образ, чью деталь носит кнопка: металл «Хрома», живая форма
     * «Экспрессива», жёсткая тень «Брутала», объём «Пластилина», блик
     * «Графита». Пусто — кнопка темы без деталей.
     */
    var vibe: String = ""
        set(value) {
            if (field == value) return
            field = value
            dirty = true
            ambient()
            invalidate()
        }

    // Ручной выбор другой кнопки или свечения имеет приоритет над точками.
    val dottedPower: Boolean get() = appearanceStyle == AppearanceStyle.SIGNAL && theme.btn == "ring" && theme.glowA == 0.0

    var theme: Theme = Look.theme(Look.Choice())
        set(value) {
            if (field == value) return
            field = value
            // Цвет дуги раньше оставался от предыдущей темы до смены состояния.
            morpher?.cancel()
            from = arc(state)
            to = from
            mix = 1f
            dirty = true
            ambient()
            invalidate()
        }

    var state: State = State.OFF
        set(value) {
            if (field == value) return
            from = arc(field)
            field = value
            to = arc(value)
            morph()
            spin(value == State.CONNECTING)
            invalidate()
        }

    /**
     * Arc — как выглядит дуга в состоянии: откуда и сколько градусов, есть ли
     * ровное кольцо, сколько свечения, каким цветом. Между двумя такими
     * кнопка и перетекает.
     */
    private data class Arc(val start: Float, val sweep: Float, val ring: Float, val glow: Float, val color: Int)

    private fun arc(s: State): Arc = when (s) {
        State.OFF -> Arc(205f, 108f, 1f, 0f, theme.acc)
        State.FAILED -> Arc(205f, 0f, 1f, 0f, theme.fail)
        State.CONNECTING -> Arc(90f + turn, 88f, 0f, 0f, theme.dim)
        State.ON -> Arc(90f, 270f, 0f, 1f, theme.acc)
    }

    private var from = arc(State.OFF)
    private var to = arc(State.OFF)

    /** Доля пути от from к to; 1 — перетекание закончилось. */
    private var mix = 1f
    private var morpher: ValueAnimator? = null

    /** Куда сейчас смотрит начало дуги, градусы; крутится только на подключении. */
    private var turn = 0f
    private var spinner: ValueAnimator? = null

    /** Предпросмотр не запускает анимацию подключения или переходов. */
    var motionEnabled: Boolean = true
        set(value) {
            field = value
            if (!value) stopAnimations() else { if (state == State.CONNECTING) spin(true); ambient() }
        }

    /**
     * Фоновое движение образа: доля цикла от 0 до 1. Блик «Графита» проходит
     * раз в шесть секунд, «Экспрессив» переливается формой за девять — те же
     * периоды, что у анимаций окна на ПК. Остальным образам оно не нужно, и
     * кнопка в покое не перерисовывается вовсе.
     */
    private var phase = 0f
    private var drift: ValueAnimator? = null
    private var sheenShown = false

    /** Волна от нажатия: 0 — не идёт, иначе доля пути от диска к краю. */
    private var pulse = 0f
    private var pulser: ValueAnimator? = null

    // ------------------------------------------------------ кэш геометрии

    private var dirty = true
    private var cx = 0f
    private var cy = 0f
    private var arcR = 0f
    private var radius = 0f
    private var reach = 0f
    private val box = RectF()
    private val iconBox = RectF()
    private var shadowShader: Shader? = null
    private var discShader: Shader? = null
    private var innerShader: Shader? = null
    private var rimShader: Shader? = null
    private var metalShader: Shader? = null
    private var puffShader: Shader? = null
    private var sheenShader: Shader? = null
    private val sheenMatrix = Matrix()
    private val blob = android.graphics.Path()
    private val spinMatrix = Matrix()

    /** Шейдер дуги кэшируется по цвету и длине: меняются только на переходе. */
    private var sweepKey = 0L
    private var sweepShader: Shader? = null

    init { background = null; setAllCaps(false); isHapticFeedbackEnabled = true; text = "" }

    override fun onSizeChanged(w: Int, h: Int, oldw: Int, oldh: Int) {
        super.onSizeChanged(w, h, oldw, oldh)
        dirty = true
    }

    private fun animationsAllowed(): Boolean {
        // Уважаем системный запрет на анимации — тогда всё просто стоит.
        val scale = android.provider.Settings.Global.getFloat(context.contentResolver, android.provider.Settings.Global.ANIMATOR_DURATION_SCALE, 1f)
        return motionEnabled && scale != 0f && isAttachedToWindow && isShown && windowVisibility == View.VISIBLE
    }

    private fun morph() {
        morpher?.cancel()
        if (dottedPower) { mix = 1f; return }
        if (!animationsAllowed() || !isAttachedToWindow) { mix = 1f; return }
        mix = 0f
        morpher = ValueAnimator.ofFloat(0f, 1f).apply {
            duration = 520
            interpolator = DecelerateInterpolator(1.8f)
            addUpdateListener { mix = it.animatedValue as Float; invalidate() }
            start()
        }
    }

    private fun spin(on: Boolean) {
        spinner?.cancel()
        spinner = null
        if (!on) { turn = 0f; return }
        if (!animationsAllowed()) return
        // Скорость как в CSS: оборот за 1,4 с.
        spinner = ValueAnimator.ofFloat(0f, 360f).apply {
            duration = 1400
            repeatCount = ValueAnimator.INFINITE
            interpolator = LinearInterpolator()
            addUpdateListener { turn = it.animatedValue as Float; invalidate() }
            start()
        }
    }

    private fun ambient() {
        drift?.cancel(); drift = null
        val period = when (vibe) { "graphite" -> 6000L; "expressive" -> 9000L; else -> 0L }
        if (period == 0L || dottedPower || !animationsAllowed()) { phase = 0f; return }
        drift = ValueAnimator.ofFloat(0f, 1f).apply {
            duration = period
            repeatCount = ValueAnimator.INFINITE
            interpolator = LinearInterpolator()
            addUpdateListener {
                phase = it.animatedValue as Float
                // Блик идёт последние сорок процентов цикла; в остальное время
                // кнопку не трогаем — один кадр, чтобы стереть ушедший блик.
                val sheen = vibe == "graphite" && phase >= SHEEN_FROM
                if (vibe != "graphite" || sheen || sheenShown) invalidate()
                sheenShown = sheen
            }
            start()
        }
    }

    private fun stopAnimations() {
        drift?.cancel(); drift = null; phase = 0f
        spinner?.cancel(); spinner = null
        morpher?.cancel(); morpher = null
        pulser?.cancel(); pulser = null
        pulse = 0f
        mix = 1f
    }

    override fun onDetachedFromWindow() {
        stopAnimations()
        super.onDetachedFromWindow()
    }

    override fun onAttachedToWindow() {
        super.onAttachedToWindow()
        if (state == State.CONNECTING) spin(true)
        ambient()
    }

    override fun onVisibilityChanged(changedView: View, visibility: Int) {
        super.onVisibilityChanged(changedView, visibility)
        // Экран не виден — не жжём кадры вращением.
        if (isShown) {
            if (state == State.CONNECTING && spinner == null) spin(true)
            if (drift == null) ambient()
        } else {
            stopAnimations()
        }
    }

    override fun onWindowVisibilityChanged(visibility: Int) {
        super.onWindowVisibilityChanged(visibility)
        if (visibility != View.VISIBLE) stopAnimations()
        else {
            if (state == State.CONNECTING && spinner == null) spin(true)
            if (drift == null) ambient()
        }
    }

    private fun rebuild() {
        val dp = resources.displayMetrics.density
        cx = width / 2f
        cy = height / 2f
        // Геометрия макета: дуга радиусом 104 при диске 86 — всё от центра.
        arcR = minOf(width, height) / 2f - ROOM * dp
        radius = arcR * (86f / 104f)
        reach = arcR + ROOM * dp
        box.set(cx - arcR, cy - arcR, cx + arcR, cy + arcR)
        val r = radius * 0.235f
        iconBox.set(cx - r, cy - r, cx + r, cy + r)

        val solid = theme.btn == "solid"
        val white = 0xFFFFFFFF.toInt()

        // Тень под диском: макет кладёт её вниз на 24px с размытием 48. На
        // светлой теме — вполсилы: чёрная тень на светлом фоне читалась грязью.
        // У «Пластилина» тень розовая и гуще: макет кладёт её цветом образа.
        val shade = when {
            vibe == "clay" -> 0x8CBE5A8C.toInt()
            theme.dark -> 0x73000000
            else -> 0x30000000
        }
        shadowShader = RadialGradient(
            cx, cy + radius * .28f, radius * 1.35f,
            intArrayOf(shade, 0x00000000), floatArrayOf(0.55f, 1f), Shader.TileMode.CLAMP,
        )

        // Металл «Хрома»: конический градиент от 210° по CSS. У Android ноль
        // смотрит вправо, у CSS — вверх, отсюда поворот на 120°.
        metalShader = if (vibe != "chrome") null else SweepGradient(cx, cy,
            intArrayOf(0xFFF4F7FB.toInt(), 0xFF8A95A3.toInt(), 0xFFE9EEF4.toInt(), 0xFF6B7480.toInt(), 0xFFF4F7FB.toInt()), null,
        ).apply { setLocalMatrix(Matrix().apply { setRotate(120f, cx, cy) }) }
        // Внутренняя тень снизу у металла и объём «Пластилина»: свет сверху
        // слева, тень снизу справа. Размытия по маске на старых телефонах в
        // аппаратной отрисовке нет, поэтому объём — градиентом поверх диска.
        puffShader = when (vibe) {
            "chrome" -> LinearGradient(cx, cy - radius, cx, cy + radius,
                intArrayOf(0x00000000, 0x00000000, 0x40000000), floatArrayOf(0f, .55f, 1f), Shader.TileMode.CLAMP)
            "clay" -> LinearGradient(cx - radius, cy - radius, cx + radius, cy + radius,
                intArrayOf(ColorUtils.setAlphaComponent(white, 150), 0x00FFFFFF, 0x00000000, 0x24000000), floatArrayOf(0f, .45f, .62f, 1f), Shader.TileMode.CLAMP)
            else -> null
        }
        // Блик «Графита» — полоса под 115°, как linear-gradient на ПК; едет
        // матрицей, шейдер один.
        sheenShader = if (vibe != "graphite") null else LinearGradient(0f, 0f, SHEEN_DX * radius * 2.4f, SHEEN_DY * radius * 2.4f,
            intArrayOf(0x00FFFFFF, 0x29FFFFFF, 0x00FFFFFF), floatArrayOf(.35f, .48f, .6f), Shader.TileMode.CLAMP)

        // Блик сверху слева, тень к низу: диск объёмный, а не плоский круг.
        // Блик — всегда светом, не цветом текста: на светлой теме текст тёмный,
        // и «блик» им выходил тёмным пятном.
        val top = if (solid) ColorUtils.blendARGB(theme.acc, white, 0.28f) else ColorUtils.blendARGB(theme.surf2, white, if (theme.dark) 0.16f else 0.45f)
        val mid = if (solid) theme.acc else ColorUtils.blendARGB(theme.surf2, theme.acc, if (theme.dark) .19f else .08f)
        val low = if (solid) ColorUtils.blendARGB(theme.acc, 0xFF000000.toInt(), 0.18f) else ColorUtils.blendARGB(theme.surf, theme.bg, .55f)
        discShader = if (theme.btn == "ring") null else RadialGradient(
            cx - radius * .32f, cy - radius * .58f, radius * 1.85f,
            intArrayOf(top, mid, low), floatArrayOf(0f, 0.52f, 1f), Shader.TileMode.CLAMP,
        )

        // Подсветка изнутри снизу: акцент отражается в диске; сила — alpha кисти.
        innerShader = if (theme.btn == "ring") null else RadialGradient(
            cx + radius * .4f, cy + radius, radius * 1.4f,
            intArrayOf(ColorUtils.setAlphaComponent(theme.acc, 80), ColorUtils.setAlphaComponent(theme.acc, 0)), null, Shader.TileMode.CLAMP,
        )

        // Световой кант по верхнему краю: диск освещён, а не вырезан.
        rimShader = LinearGradient(
            cx - radius, cy - radius, cx + radius, cy + radius,
            intArrayOf(ColorUtils.setAlphaComponent(white, if (theme.dark) 120 else 200), ColorUtils.setAlphaComponent(white, 0)),
            null, Shader.TileMode.CLAMP,
        )
        sweepShader = null
        dirty = false
    }

    private fun lerp(a: Float, b: Float, t: Float) = a + (b - a) * t

    /** Угол — по короткой дуге: с 350° на 10° ехать через 0, а не назад через 180. */
    private fun lerpDeg(a: Float, b: Float, t: Float): Float {
        var d = (b - a) % 360f
        if (d > 180f) d -= 360f
        if (d < -180f) d += 360f
        return a + d * t
    }

    override fun onDraw(canvas: Canvas) {
        if (dirty || width == 0) rebuild()
        if (dottedPower) {
            drawDottedButton(canvas)
            return
        }
        val dp = resources.displayMetrics.density
        val on = state == State.ON
        val bare = theme.btn == "bare"
        val brutal = vibe == "brutal"
        val expressive = vibe == "expressive"
        // «Брутал» не сжимается, а вдавливается в свою тень: кнопка едет на
        // 4dp, тень остаётся на месте — как translate(4px, 4px) на ПК.
        val scale = if (isPressed && !brutal) .965f else 1f
        canvas.save()
        if (brutal) {
            brush.style = Paint.Style.FILL
            brush.shader = null
            brush.color = INK
            canvas.drawCircle(cx + 6 * dp, cy + 6 * dp, radius + 2.5f * dp, brush)
            if (isPressed) canvas.translate(4 * dp, 4 * dp)
        }
        canvas.scale(scale, scale, cx, cy)
        if (expressive) blobPath()

        // Текущий вид — между прежним и целевым; на подключении начало живёт.
        val target = if (state == State.CONNECTING) to.copy(start = 90f + turn) else to
        val t = mix
        val start = lerpDeg(from.start, target.start, t)
        val sweepDeg = lerp(from.sweep, target.sweep, t)
        val ring = lerp(from.ring, target.ring, t)
        val lit = lerp(from.glow, target.glow, t)
        val glow = lit
        val color = ColorUtils.blendARGB(from.color, target.color, t)

        brush.style = Paint.Style.FILL
        brush.shader = null
        brush.alpha = 255
        // Мягкий обод вместо радиальной заливки: OLED сохраняет чёрный фон,
        // а выбор силы свечения в теме по-прежнему меняет вид кнопки.
        if (theme.glowA > 0 && glow > 0.01f) {
            brush.style = Paint.Style.STROKE
            brush.strokeWidth = (2f + theme.glowA.toFloat() * 2f) * dp
            brush.color = ColorUtils.setAlphaComponent(theme.acc, (theme.glowA * glow * 72).toInt().coerceIn(0, 255))
            canvas.drawCircle(cx, cy, arcR + 5 * dp, brush)
            brush.style = Paint.Style.FILL
        }
        // Прозрачность обода не должна доставаться тени и диску: кисть одна,
        // а шейдер берёт альфу из её цвета, и сплошная кнопка выходила блёклой.
        brush.color = 0xFFFFFFFF.toInt()

        if (!bare && !brutal) {
            brush.shader = shadowShader
            canvas.drawCircle(cx, cy + radius * .28f, radius * 1.35f, brush)
            brush.shader = null
        }

        val solid = theme.btn == "solid"
        val metal = metalShader
        when {
            metal != null -> {
                brush.color = 0xFFFFFFFF.toInt()
                brush.shader = metal
                canvas.drawCircle(cx, cy, radius, brush)
                brush.shader = null
            }
            // Плоская заливка без блика: у необрутализма объёма нет.
            brutal -> {
                brush.color = theme.acc
                canvas.drawCircle(cx, cy, radius, brush)
            }
            expressive -> {
                brush.color = 0xFFFFFFFF.toInt()
                brush.shader = discShader
                canvas.drawPath(blob, brush)
                brush.shader = null
            }
            else -> discFor(canvas, solid, lit)
        }
        puffShader?.let {
            brush.color = 0xFFFFFFFF.toInt()
            brush.shader = it
            canvas.drawCircle(cx, cy, radius, brush)
            brush.shader = null
        }
        sheenShader?.let { drawSheen(canvas, it) }

        // Тонкий обод по краю диска: линия, а подключено — акцент вполсилы.
        brush.style = Paint.Style.STROKE
        brush.strokeWidth = if (bare) 1.5f * dp else 1 * dp
        brush.color = when {
            bare -> theme.acc
            else -> ColorUtils.blendARGB(
                ColorUtils.setAlphaComponent(theme.fg, 20),
                ColorUtils.setAlphaComponent(theme.acc, if (theme.btn == "glass") 115 else 56),
                lit,
            )
        }
        when {
            brutal -> { brush.strokeWidth = 2.5f * dp; brush.color = INK; canvas.drawCircle(cx, cy, radius + 1.25f * dp, brush) }
            expressive -> canvas.drawPath(blob, brush)
            else -> canvas.drawCircle(cx, cy, radius, brush)
        }

        if (!bare && !brutal && !expressive) {
            brush.shader = rimShader
            canvas.drawArc(cx - radius + dp, cy - radius + dp, cx + radius - dp, cy + radius - dp, 190f, 150f, false, brush)
            brush.shader = null
        }

        // Волна от нажатия: кольцо расходится от диска к краю и тает.
        if (pulse > 0f) {
            brush.strokeWidth = 2 * dp
            brush.color = ColorUtils.setAlphaComponent(theme.acc, ((1f - pulse) * 150).toInt())
            canvas.drawCircle(cx, cy, lerp(radius, reach, pulse), brush)
        }

        // Ровное кольцо — выключено и ошибка; проявляется и гаснет с переходом.
        brush.strokeWidth = 3 * dp
        brush.strokeCap = Paint.Cap.ROUND
        if (ring > 0.01f) {
            val ringColor = if (state == State.FAILED) ColorUtils.setAlphaComponent(theme.fail, 140) else theme.line
            brush.color = ColorUtils.setAlphaComponent(ringColor, (android.graphics.Color.alpha(ringColor) * ring).toInt())
            canvas.drawCircle(cx, cy, arcR, brush)
        }

        // Дуга: градиент от цвета к почти прозрачному по ходу — хвост
        // растворяется, как в макете. Выключено — короткий тихий отрезок.
        if (sweepDeg > 0.5f) {
            val quiet = state == State.OFF && t >= 1f
            val arcColor = if (quiet) ColorUtils.setAlphaComponent(color, 145) else color
            brush.strokeWidth = if (quiet) 2 * dp else lerp(if (from.sweep == 108f) 2f else 3f, if (target.sweep == 108f) 2f else 3f, t) * dp
            brush.shader = sweep(start, sweepDeg, arcColor)
            canvas.drawArc(box, start, sweepDeg, false, brush)
            brush.shader = null
        }

        // Знак питания: линия и разомкнутое кольцо, как в значке макета.
        brush.color = when {
            metal != null -> INK_METAL
            solid -> theme.accFg
            on -> if (theme.dark) ColorUtils.blendARGB(theme.fg, theme.acc, 0.35f) else theme.acc
            state == State.FAILED -> theme.fail
            else -> ColorUtils.blendARGB(theme.fg, theme.acc, .25f)
        }
        brush.strokeWidth = 2.6f * dp
        val r = radius * 0.235f
        canvas.drawArc(iconBox, -55f, 290f, false, brush)
        canvas.drawLine(cx, cy - r - 3 * dp, cx, cy - r * 0.15f, brush)
        canvas.restore()
    }

    private fun drawDottedButton(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val signal = if (state == State.FAILED) theme.fail else theme.acc
        canvas.save()
        val scale = if (isPressed) .97f else 1f
        canvas.scale(scale, scale, cx, cy)
        brush.shader = null
        brush.style = Paint.Style.FILL
        brush.color = theme.surf
        canvas.drawCircle(cx, cy, radius, brush)
        brush.style = Paint.Style.STROKE
        brush.strokeWidth = dp
        brush.color = theme.line
        canvas.drawCircle(cx, cy, radius, brush)
        brush.style = Paint.Style.FILL
        val head = (turn / 360f * DOTS.size).toInt()
        DOTS.forEachIndexed { i, point ->
            val active = when (state) {
                State.ON -> i < 54
                State.CONNECTING -> (i - head + DOTS.size) % DOTS.size < 12
                State.FAILED -> i % 8 < 3
                State.OFF -> i < 3
            }
            brush.color = if (active) signal else theme.line
            canvas.drawCircle(cx + point.first * arcR, cy + point.second * arcR, if (active) 2.2f * dp else 1.8f * dp, brush)
        }
        brush.style = Paint.Style.STROKE
        brush.strokeCap = Paint.Cap.ROUND
        brush.strokeWidth = 3.1f * dp
        brush.color = if (state == State.OFF) theme.fg else signal
        canvas.drawArc(iconBox, -55f, 290f, false, brush)
        val r = radius * .235f
        canvas.drawLine(cx, cy - r - 3 * dp, cx, cy - r * .15f, brush)
        canvas.restore()
    }

    /**
     * Градиент вдоль дуги: полный цвет в начале, 12 % в конце. Шейдер один на
     * цвет и длину, поворот — матрицей: во время вращения меняется только он.
     */
    private fun sweep(start: Float, sweepDeg: Float, color: Int): Shader {
        val key = (color.toLong() shl 16) or (sweepDeg * 10).toLong().coerceIn(0, 0xFFFF)
        var shader = sweepShader
        if (shader == null || key != sweepKey) {
            shader = SweepGradient(
                cx, cy,
                intArrayOf(color, ColorUtils.setAlphaComponent(color, 30), ColorUtils.setAlphaComponent(color, 30)),
                floatArrayOf(0f, sweepDeg / 360f, 1f),
            )
            sweepShader = shader
            sweepKey = key
        }
        spinMatrix.setRotate(start, cx, cy)
        shader.setLocalMatrix(spinMatrix)
        return shader
    }

    /**
     * discFor — диск по виду кнопки темы, как до деталей образов. Вынесен из
     * onDraw, чтобы образы со своим диском просто его не вызывали.
     */
    private fun discFor(canvas: Canvas, solid: Boolean, lit: Float) {
        when (theme.btn) {
            "bare" -> Unit
            "glass" -> {
                brush.color = theme.accSoft
                canvas.drawCircle(cx, cy, radius, brush)
            }
            "ring" -> {
                // Ровный диск без цветного отражения: в OLED радиальная
                // заливка выглядела пятном даже при выключенном ореоле.
                brush.color = theme.surf
                canvas.drawCircle(cx, cy, radius, brush)
            }
            else -> {
                brush.shader = discShader
                canvas.drawCircle(cx, cy, radius, brush)
                if (!solid) {
                    // Подключено — отражение ярче; между состояниями — плавно.
                    brush.shader = innerShader
                    brush.alpha = (120 + 135 * lit).toInt().coerceIn(0, 255)
                    canvas.drawCircle(cx, cy, radius, brush)
                    brush.alpha = 255
                }
                brush.shader = null
            }
        }
    }

    /**
     * blobPath — форма «Экспрессива»: круг, по которому идут две медленные
     * волны в разные стороны. Обе целые по циклу, поэтому конец девяти
     * секунд совпадает с началом и форма не дёргается на стыке.
     */
    private fun blobPath() {
        blob.reset()
        val turn = phase * 2 * Math.PI
        val steps = 96
        for (i in 0..steps) {
            val a = i * 2 * Math.PI / steps
            val k = 1 + .05 * kotlin.math.sin(2 * a + turn) + .035 * kotlin.math.sin(3 * a - turn)
            val x = cx + (kotlin.math.cos(a) * radius * k).toFloat()
            val y = cy + (kotlin.math.sin(a) * radius * k).toFloat()
            if (i == 0) blob.moveTo(x, y) else blob.lineTo(x, y)
        }
        blob.close()
    }

    /** Блик «Графита»: из-за левого края за правый с разгоном и торможением, как ease-in-out. */
    private fun drawSheen(canvas: Canvas, shader: Shader) {
        if (phase < SHEEN_FROM) return
        val p = ((phase - SHEEN_FROM) / (1f - SHEEN_FROM)).let { it * it * (3 - 2 * it) }
        val len = radius * 2.4f
        val travel = lerp(-(radius + len * .2f), radius + len * .2f, p)
        sheenMatrix.setTranslate(cx + SHEEN_DX * (travel - len * .48f), cy + SHEEN_DY * (travel - len * .48f))
        shader.setLocalMatrix(sheenMatrix)
        brush.color = 0xFFFFFFFF.toInt()
        brush.shader = shader
        canvas.drawCircle(cx, cy, radius, brush)
        brush.shader = null
    }

    override fun drawableStateChanged() { super.drawableStateChanged(); invalidate() }

    override fun performClick(): Boolean {
        performHapticFeedback(android.view.HapticFeedbackConstants.CLOCK_TICK)
        if (animationsAllowed() && theme.btn != "bare" && !dottedPower) {
            pulser?.cancel()
            pulser = ValueAnimator.ofFloat(0.05f, 1f).apply {
                duration = 480
                interpolator = DecelerateInterpolator(1.5f)
                addUpdateListener { pulse = it.animatedValue as Float; invalidate() }
                doOnEnd { pulse = 0f; invalidate() }
                start()
            }
        }
        return super.performClick()
    }

    private fun ValueAnimator.doOnEnd(block: () -> Unit) {
        addListener(object : android.animation.AnimatorListenerAdapter() {
            override fun onAnimationEnd(animation: android.animation.Animator) = block()
        })
    }

    private companion object {
        val DOTS = Array(64) { i ->
            val angle = Math.toRadians(i * 360.0 / 64 - 90)
            kotlin.math.cos(angle).toFloat() to kotlin.math.sin(angle).toFloat()
        }
        const val INK = 0xFF111111.toInt()
        const val INK_METAL = 0xFF1B1F24.toInt()
        /** С какой доли цикла начинается блик «Графита»: на ПК 0–60 % он ждёт за краем. */
        const val SHEEN_FROM = .6f
        /** Направление полосы блика — 115° по CSS, от «вверх» по часовой. */
        val SHEEN_DX = kotlin.math.sin(Math.toRadians(115.0)).toFloat()
        val SHEEN_DY = (-kotlin.math.cos(Math.toRadians(115.0))).toFloat()
        /** Запас от дуги до края вьюхи под свечение и тень, dp. */
        const val ROOM = 16
    }
}
