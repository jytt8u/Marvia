package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.util.AttributeSet
import android.view.View

/** Небольшая точечная надпись; обычный текст и подписи остаются читаемым шрифтом. */
class DotWordmark @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {
    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)
    var color: Int = 0xFFEEF1F4.toInt()
        set(value) { field = value; invalidate() }

    private val letters = listOf(
        intArrayOf(17, 27, 21, 21, 17, 17, 17),
        intArrayOf(14, 17, 17, 31, 17, 17, 17),
        intArrayOf(30, 17, 17, 30, 20, 18, 17),
        intArrayOf(17, 17, 17, 17, 17, 10, 4),
        intArrayOf(31, 4, 4, 4, 4, 4, 31),
        intArrayOf(14, 17, 17, 31, 17, 17, 17),
    )

    init { contentDescription = context.getString(R.string.app_name) }

    override fun onDraw(canvas: Canvas) {
        brush.color = color
        val cell = minOf(width / 36f, height / 7f)
        val left = (width - 35 * cell) / 2
        val top = (height - 7 * cell) / 2
        letters.forEachIndexed { letter, rows ->
            rows.forEachIndexed { y, bits ->
                for (x in 0..4) if (bits and (1 shl (4 - x)) != 0) {
                    canvas.drawCircle(left + (letter * 6 + x + .5f) * cell, top + (y + .5f) * cell, cell * .31f, brush)
                }
            }
        }
    }
}
