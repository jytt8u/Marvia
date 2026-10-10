package io.marvia.android

import android.content.Context
import android.graphics.Typeface
import android.view.Gravity
import android.widget.LinearLayout
import android.widget.TextView
import com.google.android.material.materialswitch.MaterialSwitch

/** Настройки отображения применяются сразу; счётчики службы продолжают работать. */
class AppearanceSettings(
    private val context: Context,
    private val rows: LinearLayout,
    private val store: Store,
    private val theme: () -> Theme,
    private val onChanged: () -> Unit,
) {
    private val dp = context.resources.displayMetrics.density

    fun render() {
        rows.removeAllViews()
        heading(R.string.appearance_controls)
        val display = card()
        toggle(display, R.string.appearance_motion, R.string.appearance_motion_sub, store.reduceMotion) { store.reduceMotion = it }
        toggle(display, R.string.appearance_haptics, R.string.appearance_haptics_sub, store.hapticFeedback) { store.hapticFeedback = it }
        toggle(display, R.string.appearance_home_stats, R.string.appearance_home_stats_sub, store.homeStats) { store.homeStats = it }
        heading(R.string.appearance_monitor)
        val monitor = card()
        description(monitor, R.string.appearance_monitor_sub)
        val largeText = context.resources.configuration.fontScale > 1.5f
        val strip = LinearLayout(context).apply { orientation = if (largeText) LinearLayout.VERTICAL else LinearLayout.HORIZONTAL }
        val selected = MonitoringProfiles.selected(store)
        val t = theme()
        MonitoringProfiles.all.forEachIndexed { index, profile ->
            strip.addView(TextView(context).apply {
                setText(profile.label)
                textSize = 12f
                gravity = Gravity.CENTER
                minHeight = px(48)
                isClickable = true
                isFocusable = true
                isSelected = selected == index
                background = Paint.rounded(if (isSelected) t.acc else t.surf2, 10, dp)
                setTextColor(if (isSelected) t.accFg else t.fg)
                setOnClickListener {
                    MonitoringProfiles.apply(store, index)
                    render()
                }
            }, LinearLayout.LayoutParams(if (largeText) -1 else 0, -2, if (largeText) 0f else 1f).apply {
                if (index > 0) { if (largeText) topMargin = px(6) else marginStart = px(6) }
            })
        }
        monitor.addView(strip, LinearLayout.LayoutParams(-1, -2).apply { topMargin = px(12) })
        Paint.apply(rows, t)
    }

    private fun heading(title: Int) {
        rows.addView(TextView(context).apply {
            setText(title)
            textSize = 14f
            setTypeface(typeface, Typeface.BOLD)
            tag = "fg"
        }, LinearLayout.LayoutParams(-1, -2).apply { topMargin = px(14); bottomMargin = px(9); marginStart = px(4) })
    }

    private fun card(): LinearLayout = LinearLayout(context).apply {
        orientation = LinearLayout.VERTICAL
        tag = "card"
        rows.addView(this, LinearLayout.LayoutParams(-1, -2))
    }

    private fun description(parent: LinearLayout, text: Int) {
        parent.addView(TextView(context).apply {
            setText(text)
            textSize = 13f
            tag = "dim"
        })
    }

    private fun toggle(parent: LinearLayout, title: Int, subtitle: Int, checked: Boolean, save: (Boolean) -> Unit) {
        val row = LinearLayout(context).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            minimumHeight = px(78)
            setPadding(0, px(10), 0, px(10))
        }
        val words = LinearLayout(context).apply { orientation = LinearLayout.VERTICAL }
        words.addView(TextView(context).apply {
            setText(title)
            textSize = 15f
            tag = "fg"
        })
        description(words, subtitle)
        row.addView(words, LinearLayout.LayoutParams(0, -2, 1f))
        val switch = MaterialSwitch(context).apply {
            tag = "switch"
            isChecked = checked
            contentDescription = context.getString(title)
            setOnCheckedChangeListener { _, value -> save(value); onChanged() }
        }
        row.addView(switch, LinearLayout.LayoutParams(-2, -2).apply { marginStart = px(12) })
        row.isClickable = true
        row.isFocusable = true
        row.setOnClickListener { switch.toggle() }
        parent.addView(row)
    }

    private fun px(value: Int) = (value * dp).toInt()
}
