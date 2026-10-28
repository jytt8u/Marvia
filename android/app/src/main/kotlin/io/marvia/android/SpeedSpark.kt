package io.marvia.android

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Canvas
import android.graphics.LinearGradient
import android.graphics.Paint
import android.graphics.Path
import android.graphics.Shader
import android.util.AttributeSet
import android.view.View
import android.view.animation.LinearInterpolator
import androidx.core.graphics.ColorUtils

/**
 * SpeedSpark — бегущая линия скорости в карточке «Загрузка» или «Отдача».
 *
 * Замер приходит раз в секунду. Перерисуй линию скачком — и она дёргается
 * влево на целый шаг каждую секунду, как старый осциллограф. Поэтому между
 * замерами линия плавно съезжает на один шаг, и новая точка выезжает справа.
 *
 * Значения настоящие: тишина рисуется нулём, а не придуманным шумом. Без
 * подключения — тусклая ровная линия, она показывает, где будет график.
 */
class SpeedSpark @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {
    private val pen = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeJoin = Paint.Join.ROUND
        strokeCap = Paint.Cap.ROUND
    }
    private val fill = Paint(Paint.ANTI_ALIAS_FLAG)
    private val line = Path()
    private val area = Path()

    var theme: Theme = Look.theme(Look.Choice())
        set(value) { field = value; invalidate() }

    /** Сколько последних замеров помещается в карточку. */
    private val window = 30
    private var values: List<Double> = emptyList()
    private var shift = 0f
    private val slide = ValueAnimator.ofFloat(1f, 0f).apply {
        interpolator = LinearInterpolator()
        addUpdateListener { shift = it.animatedValue as Float; invalidate() }
    }

    /** Новые замеры; последний — самый свежий. */
    fun show(next: List<Double>) {
        // В начале сессии замеров меньше, чем места. Слева дорисовываем нули:
        // до подключения скорость и была нулевой, а линия на всю ширину читается
        // как график, а не как обрывок в углу.
        val tail = next.takeLast(window + 1)
        val trimmed = if (tail.isEmpty()) tail else List(window + 1 - tail.size) { 0.0 } + tail
        val grew = trimmed.isNotEmpty() && trimmed != values
        values = trimmed
        if (grew && values.size > 1 && isAttachedToWindow) {
            slide.cancel()
            slide.duration = 950
            slide.start()
        } else {
            shift = 0f
            invalidate()
        }
    }

    override fun onDetachedFromWindow() {
        slide.cancel()
        super.onDetachedFromWindow()
    }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val w = width.toFloat()
        val h = height.toFloat()
        val top = 3 * dp
        val bottom = h - 2 * dp
        val live = values.any { it > 0 }
        pen.strokeWidth = 1.8f * dp
        if (!live) {
            pen.color = ColorUtils.setAlphaComponent(theme.acc, if (theme.dark) 60 else 70)
            canvas.drawLine(0f, bottom, w, bottom, pen)
            return
        }
        // Шкала — по видимому окну, но не меньше 0,5 Мбит/с: иначе фоновый
        // шорох в килобайты рисовался бы горами.
        val floor = 0.5 * 1_000_000 / 8
        val max = maxOf(values.maxOrNull() ?: 0.0, floor)
        val step = w / (window - 1)
        line.reset()
        area.reset()
        val n = values.size
        values.forEachIndexed { i, v ->
            val x = w - (n - 1 - i - shift) * step
            val y = bottom - ((bottom - top) * v / max).toFloat()
            if (i == 0) { line.moveTo(x, y); area.moveTo(x, bottom); area.lineTo(x, y) }
            else { line.lineTo(x, y); area.lineTo(x, y) }
        }
        area.lineTo(w + shift * step, bottom)
        area.close()
        fill.shader = LinearGradient(0f, top, 0f, bottom,
            ColorUtils.setAlphaComponent(theme.acc, if (theme.dark) 70 else 50), ColorUtils.setAlphaComponent(theme.acc, 0), Shader.TileMode.CLAMP)
        canvas.save()
        canvas.clipRect(0f, 0f, w, h)
        canvas.drawPath(area, fill)
        pen.color = theme.acc
        canvas.drawPath(line, pen)
        canvas.restore()
    }
}
