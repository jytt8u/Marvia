package io.marvia.android

import android.graphics.Bitmap
import android.graphics.BitmapShader
import android.graphics.Canvas
import android.graphics.ColorFilter
import android.graphics.LinearGradient
import android.graphics.Matrix
import android.graphics.Paint
import android.graphics.PixelFormat
import android.graphics.RadialGradient
import android.graphics.Rect
import android.graphics.RectF
import android.graphics.Shader
import android.graphics.SweepGradient
import android.graphics.Typeface
import android.graphics.drawable.Drawable
import android.graphics.drawable.GradientDrawable
import android.graphics.drawable.LayerDrawable
import android.os.SystemClock
import android.view.Gravity
import android.widget.TextView
import androidx.core.graphics.ColorUtils

/**
 * Фирменные детали образов — то, по чему образ узнаётся, когда цвет уже
 * подстроен под себя: жёсткие тени «Брутала», стекло, голографическая рамка
 * «Хрома», зерно «Плёнки». Те же, что в окне на компьютере (блок «Фирменные
 * детали образов» в app.html), насколько их позволяет нарисовать Android без
 * размытия по маске: на старых телефонах оно рисуется только программно, и
 * одна такая карточка роняла бы плавность всего экрана.
 *
 * Тема о деталях не знает: они зависят от образа, а не от палитры. Пустой
 * образ — деталей нет, экран такой же, как до их появления.
 */
object VibeDetails {
    private const val INK = 0xFF111111.toInt()
    private const val WHITE = 0xFFFFFFFF.toInt()

    /** Сдвиг жёсткой тени «Брутала», dp: на столько карточке нужен запас справа и снизу. */
    const val HARD = 4

    fun hardShift(vibe: String): Int = if (vibe == "brutal") HARD else 0

    /**
     * Карточка образа поверх обычной `base`; null — у образа своей нет.
     * Главная карточка (`feature`) у части образов выделена сильнее: у неё
     * голографическая или переливающаяся рамка, у тональных — тон гуще.
     */
    fun card(vibe: String, t: Theme, dp: Float, base: Drawable, feature: Boolean): Drawable? {
        val r = t.r * dp
        return when (vibe) {
            // Стекло: светлая кайма и блик по верхней кромке. Поверхность
            // прозрачная — сквозь неё видны пятна света фона, ради них образ.
            "glass" -> if (t.dark) glass(r, dp, feature) else null
            "aurora" -> when {
                feature -> Edge(t.surf, r, dp, intArrayOf(0xFFA98BFF.toInt(), 0xFF40C8FF.toInt(), 0xFFFF78C8.toInt(), 0xFFA98BFF.toInt()), sweep = false)
                t.dark -> glass(r, dp, false)
                else -> null
            }
            "chrome" -> if (feature) Edge(t.surf, r, 1.5f * dp, intArrayOf(0xFFA78BFA.toInt(), 0xFF5EEAD4.toInt(), 0xFFFACC15.toInt(), 0xFFF472B6.toInt(), 0xFFA78BFA.toInt()), sweep = true) else null
            // Тональные карточки Material: акцент, растворённый в поверхности, без рамки.
            "expressive" -> GradientDrawable().apply {
                setColor(ColorUtils.blendARGB(t.surf, t.acc, if (feature) .18f else .09f))
                cornerRadius = r
            }
            "brutal" -> hard(if (feature) ColorUtils.blendARGB(t.surf, t.acc, .12f) else t.surf, r, dp, HARD)
            // Пластилин: свет сверху слева, тень снизу справа — внутренние тени
            // макета градиентом. Внешнюю тень даёт elevation (см. lift).
            "clay" -> GradientDrawable(GradientDrawable.Orientation.TL_BR, intArrayOf(
                ColorUtils.blendARGB(t.surf, WHITE, .7f), t.surf, ColorUtils.blendARGB(t.surf, 0xFF000000.toInt(), .06f),
            )).apply { cornerRadius = r }
            // Терминал: кант акцента слева, как курсор строки.
            "terminal" -> LayerDrawable(arrayOf(base, GradientDrawable().apply { setColor(ColorUtils.setAlphaComponent(t.acc, 180)) })).apply {
                setLayerGravity(1, Gravity.START or Gravity.FILL_VERTICAL)
                setLayerWidth(1, (3 * dp).toInt().coerceAtLeast(2))
            }
            else -> null
        }
    }

    /**
     * Кнопка или чип «Брутала»: чёрная рамка и тень без размытия; у прочих
     * образов — null, и красит обычный код.
     */
    fun box(vibe: String, fill: Int, radiusDp: Int, dp: Float): Drawable? =
        if (vibe == "brutal") hard(fill, radiusDp * dp, dp, 3) else null

    /**
     * lift — внешняя тень «Пластилина»: розовая и мягкая, через системную
     * elevation. Цвет тени Android умеет только с 9-й версии; раньше она
     * просто серая, и образ от этого не ломается.
     */
    fun lift(vibe: String, v: android.view.View, dp: Float): Boolean {
        val clay = vibe == "clay"
        if (android.os.Build.VERSION.SDK_INT >= 28) {
            val shadow = if (clay) 0xFFBE5A8C.toInt() else 0xFF000000.toInt()
            v.outlineSpotShadowColor = shadow
            v.outlineAmbientShadowColor = shadow
        }
        if (clay) v.elevation = 10 * dp
        return clay
    }

    private fun glass(r: Float, dp: Float, feature: Boolean) = GradientDrawable(
        GradientDrawable.Orientation.TOP_BOTTOM,
        intArrayOf(ColorUtils.setAlphaComponent(WHITE, if (feature) 44 else 34), ColorUtils.setAlphaComponent(WHITE, 14), ColorUtils.setAlphaComponent(WHITE, 10)),
    ).apply {
        cornerRadius = r
        setStroke(dp.toInt().coerceAtLeast(1), ColorUtils.setAlphaComponent(WHITE, 46))
    }

    private fun hard(fill: Int, r: Float, dp: Float, shiftDp: Int): Drawable {
        val shift = (shiftDp * dp).toInt()
        val shadow = GradientDrawable().apply { setColor(INK); cornerRadius = r }
        val face = GradientDrawable().apply {
            setColor(fill); cornerRadius = r
            setStroke((2 * dp).toInt().coerceAtLeast(2), INK)
        }
        return LayerDrawable(arrayOf(shadow, face)).apply {
            setLayerInset(0, shift, shift, 0, 0)
            setLayerInset(1, 0, 0, shift, shift)
        }
    }

    /**
     * Edge — карточка с цветной рамкой: заливка поверхностью и обводка
     * градиентом, по кругу (голография «Хрома») или наискось («Аврора»).
     * GradientDrawable обводку градиентом не умеет. Рамка стоит: на
     * компьютере она медленно вращается, а на телефоне вечная перерисовка
     * карточки на главной стоила бы батареи.
     */
    private class Edge(private val fill: Int, private val r: Float, private val width: Float, private val colors: IntArray, private val sweep: Boolean) : Drawable() {
        private val face = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = fill }
        private val line = Paint(Paint.ANTI_ALIAS_FLAG).apply { style = Paint.Style.STROKE; strokeWidth = width; color = WHITE }
        private val box = RectF()

        override fun onBoundsChange(bounds: Rect) {
            val w = bounds.width().toFloat()
            val h = bounds.height().toFloat()
            line.shader = if (sweep) {
                SweepGradient(bounds.exactCenterX(), bounds.exactCenterY(), colors, null)
            } else {
                LinearGradient(bounds.left.toFloat(), bounds.top.toFloat(), bounds.left + w, bounds.top + h, colors, null, Shader.TileMode.CLAMP)
            }
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

    // ------------------------------------------------------ надпись состояния

    /**
     * status — надпись под кнопкой в образе: красная точка «в эфире» у табло,
     * фосфор и курсор «Терминала», курсив «Плёнки», металл «Хрома». Вызывается
     * при каждой смене состояния и снимает чужие детали, если образ сменился.
     */
    fun status(v: TextView, vibe: String, t: Theme, on: Boolean, motion: Boolean) {
        val size = v.textSize
        val mark: Drawable? = when (vibe) {
            "dot", "paper" -> Blink(Blink.DOT, if (on) t.acc else ColorUtils.setAlphaComponent(t.dim, 128), size, if (on && motion) 1600 else 0, 38)
            "terminal" -> Blink(Blink.BAR, v.currentTextColor, size, if (motion) 1000 else 0, 0)
            else -> null
        }
        if (vibe == "terminal") {
            v.setCompoundDrawablesRelativeWithIntrinsicBounds(null, null, mark, null)
        } else {
            v.setCompoundDrawablesRelativeWithIntrinsicBounds(mark, null, null, null)
        }
        v.compoundDrawablePadding = if (mark == null) 0 else (size * if (vibe == "terminal") .08f else .45f).toInt()

        if (vibe == "terminal") {
            v.setShadowLayer(size * .3f, 0f, 0f, ColorUtils.setAlphaComponent(t.acc, 140))
        } else if (v.getTag(R.id.photo_shadow) != true) {
            v.setShadowLayer(0f, 0f, 0f, 0)
        }

        // Курсив синтезируется наклоном: своего начертания у шрифтов нет, а
        // TextView сам снимает наклон, когда курсив больше не просят.
        v.setTypeface(v.typeface, if (vibe == "film") Typeface.BOLD_ITALIC else Typeface.BOLD)

        // Металл по буквам — градиент сверху вниз, как хром на ПК: светлое,
        // перелом в тёмное посередине и снова светлое.
        if (vibe == "chrome") {
            val h = (v.lineHeight.takeIf { it > 0 } ?: size.toInt()).toFloat()
            val top = v.totalPaddingTop.toFloat()
            v.paint.shader = LinearGradient(0f, top, 0f, top + h,
                intArrayOf(WHITE, 0xFFC9D1DB.toInt(), 0xFF6B7480.toInt(), 0xFFE6EBF1.toInt()), floatArrayOf(0f, .45f, .55f, 1f), Shader.TileMode.CLAMP)
        } else {
            v.paint.shader = null
        }
        v.invalidate()
    }

    /**
     * Blink — точка или курсор у надписи, мигающий ступенькой: половину
     * периода горит, половину почти гаснет, без плавного перехода — как
     * лампа записи. Сам себя перепланирует через хозяина-вьюху и стоит,
     * когда период нулевой (подключения нет или анимации выключены).
     */
    private class Blink(private val kind: Int, color: Int, private val text: Float, private val period: Long, private val low: Int) : Drawable(), Runnable {
        private val brush = Paint(Paint.ANTI_ALIAS_FLAG).apply { this.color = color }
        private var lit = period <= 0 || (SystemClock.uptimeMillis() / (period / 2)) % 2 == 0L

        override fun getIntrinsicWidth() = (text * if (kind == DOT) .32f else .5f).toInt().coerceAtLeast(1)
        override fun getIntrinsicHeight() = (text * if (kind == DOT) .32f else .72f).toInt().coerceAtLeast(1)

        override fun draw(canvas: Canvas) {
            val b = bounds
            brush.alpha = if (lit) 255 else low
            if (kind == DOT) {
                canvas.drawCircle(b.exactCenterX(), b.exactCenterY(), b.width() / 2f, brush)
            } else {
                // Курсор лежит на базовой линии: низ рамки, толщина в десятую кегля.
                canvas.drawRect(b.left.toFloat(), b.bottom - text * .1f, b.right.toFloat(), b.bottom.toFloat(), brush)
            }
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

        companion object { const val DOT = 0; const val BAR = 1 }
    }

    // ------------------------------------------------------ плёнка

    /**
     * Veil — зерно и виньетка «Плёнки» поверх всего экрана, как в окне на ПК:
     * фотография, а не экран. Рисуется передним планом корня и касаний не
     * перехватывает. Зерно — плитка, повторённая шейдером: шум на каждый
     * пиксель каждого кадра посчитать дороже, чем один раз нарисовать
     * квадрат и размножить его.
     */
    class Veil : Drawable() {
        private val grain = Paint().apply { shader = BitmapShader(tile, Shader.TileMode.REPEAT, Shader.TileMode.REPEAT) }
        private val shade = Paint(Paint.ANTI_ALIAS_FLAG)

        override fun onBoundsChange(bounds: Rect) {
            val w = bounds.width().toFloat()
            val h = bounds.height().toFloat()
            if (w <= 0f || h <= 0f) return
            // Эллипс по экрану, края темнеют с середины пути: на высоком
            // телефоне круглая виньетка оставила бы светлыми углы.
            val g = RadialGradient(0f, 0f, 1f, intArrayOf(0, 0, 0x73000000), floatArrayOf(0f, .55f, 1f), Shader.TileMode.CLAMP)
            g.setLocalMatrix(Matrix().apply { setScale(w * .9f, h * .75f); postTranslate(bounds.left + w / 2, bounds.top + h * .45f) })
            shade.shader = g
        }

        override fun draw(canvas: Canvas) {
            canvas.drawRect(bounds, grain)
            canvas.drawRect(bounds, shade)
        }

        override fun setAlpha(alpha: Int) { grain.alpha = alpha; shade.alpha = alpha }
        override fun setColorFilter(filter: ColorFilter?) { grain.colorFilter = filter; shade.colorFilter = filter }
        @Deprecated("Deprecated in Java")
        override fun getOpacity(): Int = PixelFormat.TRANSLUCENT

        companion object {
            // Зерно заметное, в отличие от того, что в Backdrop гасит кольца
            // градиента: здесь шум — часть образа, а не лекарство.
            private val tile: Bitmap by lazy {
                val n = 160
                val px = IntArray(n * n)
                val rnd = java.util.Random(11)
                for (i in px.indices) {
                    val a = 6 + rnd.nextInt(22)
                    px[i] = if (rnd.nextBoolean()) (a shl 24) or 0xFFFFFF else (a shl 24)
                }
                Bitmap.createBitmap(px, n, n, Bitmap.Config.ARGB_8888)
            }
        }
    }
}
