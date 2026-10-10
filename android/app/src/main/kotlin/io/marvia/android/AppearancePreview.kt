package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.LinearGradient
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RadialGradient
import android.graphics.RectF
import android.graphics.Shader
import android.graphics.Typeface
import android.view.View
import androidx.core.content.res.ResourcesCompat
import androidx.core.graphics.ColorUtils

/**
 * Миниатюра образа — как обои в выборе темы: фон образа во всю карточку, его
 * кнопка, надпись «Подключено» его шрифтом и маленькая карточка с цифрой и
 * графиком. Серые полоски «вместо интерфейса» выглядели заготовкой, а не
 * будущим экраном. Цифра — пример, не текущее подключение.
 */
class AppearancePreview(context: Context, private val profile: AppearanceProfiles.Profile) : View(context) {
    private val theme = Look.theme(Look.normalize(profile.choice))
    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)
    private val rect = RectF()
    private val spark = Path()
    private val status = context.getString(R.string.status_on)
    private val face: Typeface? = when (profile.font) {
        "manrope" -> ResourcesCompat.getFont(context, R.font.manrope)
        "geologica" -> ResourcesCompat.getFont(context, R.font.geologica)
        else -> ResourcesCompat.getFont(context, R.font.onest)
    }
    private val mono: Typeface? = ResourcesCompat.getFont(context, R.font.mono)

    override fun onDraw(canvas: Canvas) {
        val w = width.toFloat()
        val h = height.toFloat()
        val dp = resources.displayMetrics.density
        val p = profile.preview
        val t = theme

        // Фон и пятна света.
        brush.shader = LinearGradient(0f, 0f, 0f, h, p.top, p.bottom, Shader.TileMode.CLAMP)
        canvas.drawRect(0f, 0f, w, h, brush)
        fun blob(color: Int, x: Float, y: Float, r: Float) {
            if (color == 0) return
            brush.shader = RadialGradient(x, y, r, color, ColorUtils.setAlphaComponent(color, 0), Shader.TileMode.CLAMP)
            canvas.drawRect(0f, 0f, w, h, brush)
        }
        blob(p.blobA, w * .22f, h * .3f, w * .55f)
        blob(p.blobB, w * .82f, h * .72f, w * .5f)
        brush.shader = null

        // Узор образа — тонко, чтобы угадывался характер.
        brush.color = ColorUtils.setAlphaComponent(t.fg, if (t.dark) 38 else 46)
        val step = 9 * dp
        when (profile.pattern) {
            Store.PATTERN_DOTS -> { var y = step / 2; while (y < h) { var x = step / 2; while (x < w) { canvas.drawCircle(x, y, .9f * dp, brush); x += step }; y += step } }
            "grid" -> { brush.strokeWidth = .7f * dp; var x = 0f; while (x < w) { canvas.drawLine(x, 0f, x, h, brush); x += step * 1.5f }; var y = 0f; while (y < h) { canvas.drawLine(0f, y, w, y, brush); y += step * 1.5f } }
            "lines" -> { var y = 0f; while (y < h) { canvas.drawRect(0f, y, w, y + .6f * dp, brush); y += 3.5f * dp } }
            "rings" -> { brush.style = Paint.Style.STROKE; brush.strokeWidth = .8f * dp; for (i in 1..4) canvas.drawCircle(w * .8f, h * .3f, i * 14 * dp, brush); brush.style = Paint.Style.FILL }
        }

        // Кнопка образа.
        val r = minOf(h * .22f, w * .14f)
        val cx = 14 * dp + r
        val cy = 14 * dp + r
        when {
            profile.style == AppearanceStyle.SIGNAL -> {
                brush.color = t.surf; canvas.drawCircle(cx, cy, r, brush)
                for (i in 0..23) {
                    val a = Math.toRadians(i * 15.0 - 90)
                    brush.color = if (i < 6) t.acc else ColorUtils.setAlphaComponent(t.fg, 70)
                    canvas.drawCircle(cx + kotlin.math.cos(a).toFloat() * r * 1.22f, cy + kotlin.math.sin(a).toFloat() * r * 1.22f, 1.2f * dp, brush)
                }
            }
            profile.id == "chrome" -> {
                // Прозрачность кисти действует и на градиент — сначала делаем её полной.
                brush.color = 0xFFFFFFFF.toInt()
                brush.shader = android.graphics.SweepGradient(cx, cy,
                    intArrayOf(0xFFF4F7FB.toInt(), 0xFF8A95A3.toInt(), 0xFFE9EEF4.toInt(), 0xFF6B7480.toInt(), 0xFFF4F7FB.toInt()), null)
                canvas.drawCircle(cx, cy, r, brush); brush.shader = null
            }
            t.btn == "solid" -> {
                brush.color = t.acc; canvas.drawCircle(cx, cy, r, brush)
                brush.shader = RadialGradient(cx - r * .3f, cy - r * .4f, r, ColorUtils.setAlphaComponent(-1, 90), 0, Shader.TileMode.CLAMP)
                canvas.drawCircle(cx, cy, r, brush); brush.shader = null
            }
            t.btn == "glass" -> {
                brush.color = ColorUtils.setAlphaComponent(-1, 40); canvas.drawCircle(cx, cy, r, brush)
                brush.style = Paint.Style.STROKE; brush.strokeWidth = dp; brush.color = ColorUtils.setAlphaComponent(-1, 110)
                canvas.drawCircle(cx, cy, r, brush); brush.style = Paint.Style.FILL
            }
            else -> {
                brush.color = ColorUtils.setAlphaComponent(t.surf, 220); canvas.drawCircle(cx, cy, r, brush)
                brush.style = Paint.Style.STROKE; brush.strokeWidth = 2 * dp; brush.color = t.acc
                canvas.drawCircle(cx, cy, r, brush); brush.style = Paint.Style.FILL
            }
        }
        if (profile.id == "brutal") {
            brush.style = Paint.Style.STROKE; brush.strokeWidth = 2 * dp; brush.color = 0xFF111111.toInt()
            canvas.drawCircle(cx, cy, r, brush); brush.style = Paint.Style.FILL
        }
        // Значок питания.
        val iconColor = when {
            profile.id == "chrome" -> 0xFF1B1F24.toInt()
            t.btn == "solid" && profile.style != AppearanceStyle.SIGNAL -> t.accFg
            else -> t.acc
        }
        brush.style = Paint.Style.STROKE; brush.strokeWidth = 1.8f * dp; brush.strokeCap = Paint.Cap.ROUND; brush.color = iconColor
        rect.set(cx - r * .32f, cy - r * .28f, cx + r * .32f, cy + r * .36f)
        canvas.drawArc(rect, -60f, 300f, false, brush)
        canvas.drawLine(cx, cy - r * .42f, cx, cy, brush)
        brush.style = Paint.Style.FILL

        // Надпись шрифтом образа.
        brush.typeface = if (profile.style == AppearanceStyle.SIGNAL) mono else face
        brush.isFakeBoldText = profile.style != AppearanceStyle.SIGNAL
        brush.color = if (profile.id == "terminal") t.acc else t.fg
        var size = 11 * dp
        val label = if (profile.style == AppearanceStyle.SIGNAL) status.uppercase() else status
        val maxText = w * .55f - 14 * dp
        brush.textSize = size
        while (brush.measureText(label) > maxText && size > 6 * dp) { size -= .5f * dp; brush.textSize = size }
        canvas.drawText(label, 14 * dp, h - 14 * dp, brush)
        brush.isFakeBoldText = false

        // Карточка с цифрой и графиком.
        val cw = w * .38f
        rect.set(w - cw - 10 * dp, 12 * dp, w - 10 * dp, h - 12 * dp)
        val cr = (t.r * .5f * dp).coerceAtMost(14 * dp)
        brush.color = when (profile.id) {
            "glass", "aurora" -> ColorUtils.setAlphaComponent(-1, 34)
            "expressive" -> ColorUtils.blendARGB(0xFFFFFFFF.toInt(), t.acc, .14f)
            else -> t.surf
        }
        canvas.drawRoundRect(rect, cr, cr, brush)
        brush.style = Paint.Style.STROKE; brush.strokeWidth = if (profile.id == "brutal") 2 * dp else dp
        brush.color = if (profile.id == "brutal") 0xFF111111.toInt() else ColorUtils.setAlphaComponent(t.line, 180)
        canvas.drawRoundRect(rect, cr, cr, brush); brush.style = Paint.Style.FILL
        brush.typeface = mono; brush.textSize = 13 * dp; brush.isFakeBoldText = true; brush.color = t.fg
        canvas.drawText("48,0", rect.left + 7 * dp, rect.top + 22 * dp, brush)
        brush.isFakeBoldText = false
        spark.reset()
        val pts = floatArrayOf(.85f, .75f, .8f, .55f, .62f, .4f, .48f, .2f, .26f, .1f)
        pts.forEachIndexed { i, v ->
            val x = rect.left + 6 * dp + (rect.width() - 12 * dp) * i / (pts.size - 1)
            val y = rect.bottom - 6 * dp - (rect.height() * .38f) * (1 - v)
            if (i == 0) spark.moveTo(x, y) else spark.lineTo(x, y)
        }
        brush.style = Paint.Style.STROKE; brush.strokeWidth = 1.6f * dp; brush.strokeJoin = Paint.Join.ROUND; brush.color = t.acc
        canvas.drawPath(spark, brush); brush.style = Paint.Style.FILL
    }
}
