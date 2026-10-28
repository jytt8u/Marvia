package io.marvia.android

import android.view.Gravity
import android.widget.LinearLayout
import android.widget.TextView
import io.marvia.android.databinding.ScreenStatsBinding

/** Заголовок и факты получают полную ширину, когда системный текст крупнее. */
object StatisticsLayout {
    fun fit(ui: ScreenStatsBinding) {
        if (ui.root.resources.configuration.fontScale <= 1.5f) return
        val dp = ui.root.resources.displayMetrics.density
        val heading = ui.rangeBox.parent as LinearLayout
        heading.orientation = LinearLayout.VERTICAL
        heading.gravity = Gravity.START
        heading.getChildAt(0).layoutParams = LinearLayout.LayoutParams(-1, -2)
        ui.rangeBox.layoutParams = LinearLayout.LayoutParams(-2, -2).apply { topMargin = (8 * dp).toInt() }
        val facts = ui.factQuota.parent.parent as LinearLayout
        facts.orientation = LinearLayout.VERTICAL
        for (i in 0 until facts.childCount) {
            val child = facts.getChildAt(i)
            child.layoutParams = LinearLayout.LayoutParams(-1, if (child is LinearLayout) -2 else (1 * dp).toInt()).apply {
                topMargin = (8 * dp).toInt()
            }
            if (child is LinearLayout) {
                child.gravity = Gravity.START
                for (j in 0 until child.childCount) {
                    (child.getChildAt(j) as? TextView)?.apply {
                        gravity = Gravity.START
                        layoutParams = LinearLayout.LayoutParams(-1, -2)
                    }
                }
            }
        }
    }
}
