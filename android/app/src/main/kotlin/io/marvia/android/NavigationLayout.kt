package io.marvia.android

import android.view.Gravity
import android.widget.GridLayout

/** На крупном шрифте две строки сохраняют подписи и удобную область нажатия. */
object NavigationLayout {
    fun fit(grid: GridLayout) {
        val largeText = grid.resources.configuration.fontScale > 1.5f
        val columns = if (largeText) 6 else 5
        grid.columnCount = columns
        grid.rowCount = if (largeText) 2 else 1
        val dp = grid.resources.displayMetrics.density
        for (i in 0 until grid.childCount) {
            grid.getChildAt(i).apply {
                minimumHeight = (56 * dp).toInt()
                val row = if (largeText) i / 3 else 0
                val span = if (!largeText) 1 else if (row == 0) 2 else 3
                val column = if (largeText) (i % 3) * span else i
                layoutParams = GridLayout.LayoutParams(
                    GridLayout.spec(row), GridLayout.spec(column, span, 1f),
                ).apply {
                    width = 0
                    height = GridLayout.LayoutParams.WRAP_CONTENT
                    setGravity(Gravity.FILL_HORIZONTAL)
                }
            }
        }
    }
}
