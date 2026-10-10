package io.marvia.android

import android.content.Context
import android.graphics.Typeface
import android.view.Gravity
import android.view.HapticFeedbackConstants
import android.widget.FrameLayout
import android.widget.GridLayout
import android.widget.LinearLayout
import android.widget.TextView

/**
 * Лента образов в два ряда, листается вбок — как в окне на компьютере. Образов
 * двенадцать, в одну строку они уходили бы далеко за край, а две строки
 * показывают сразу восемь и намекают, что дальше есть ещё.
 */
class AppearancePicker(
    private val context: Context,
    private val grid: GridLayout,
    private val store: Store,
    private val onChanged: () -> Unit,
) {
    private val dp = context.resources.displayMetrics.density

    fun render(t: Theme) {
        grid.removeAllViews()
        grid.rowCount = 2
        grid.columnCount = (AppearanceProfiles.all.size + 1) / 2
        val selected = AppearanceProfiles.selected(store)
        AppearanceProfiles.all.forEachIndexed { index, profile ->
            val active = selected == profile
            val card = LinearLayout(context).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(px(10), px(10), px(10), px(10))
                isClickable = true
                isFocusable = true
                isSelected = active
                contentDescription = context.getString(profile.name) + ": " + context.getString(profile.description) +
                    if (active) ". " + context.getString(R.string.appearance_selected) else ""
                setOnClickListener {
                    if (store.hapticFeedback) performHapticFeedback(HapticFeedbackConstants.CLOCK_TICK)
                    AppearanceProfiles.apply(store, profile)
                    onChanged()
                }
            }
            val thumbnail = FrameLayout(context)
            thumbnail.clipToOutline = true
            thumbnail.outlineProvider = object : android.view.ViewOutlineProvider() {
                override fun getOutline(view: android.view.View, outline: android.graphics.Outline) =
                    outline.setRoundRect(0, 0, view.width, view.height, 12 * dp)
            }
            thumbnail.addView(AppearancePreview(context, profile), FrameLayout.LayoutParams(-1, -1))
            if (active) thumbnail.addView(TextView(context).apply {
                text = "✓"
                textSize = 12f
                gravity = Gravity.CENTER
                setTextColor(t.accFg)
                background = Paint.circle(t.acc)
                importantForAccessibility = android.view.View.IMPORTANT_FOR_ACCESSIBILITY_NO
            }, FrameLayout.LayoutParams(px(24), px(24), Gravity.TOP or Gravity.END))
            card.addView(thumbnail, LinearLayout.LayoutParams(-1, px(92)))
            card.addView(TextView(context).apply {
                setText(profile.name)
                textSize = 15f
                setTypeface(typeface, Typeface.BOLD)
                tag = "fg"
            }, LinearLayout.LayoutParams(-1, -2).apply { topMargin = px(8) })
            // По столбцам: первый образ сверху, второй под ним, третий справа от первого.
            grid.addView(card, GridLayout.LayoutParams(GridLayout.spec(index % 2), GridLayout.spec(index / 2)).apply {
                width = px(if (context.resources.configuration.fontScale > 1.5f) 200 else 168)
                height = -2
                if (index / 2 > 0) marginStart = px(10)
                if (index % 2 > 0) topMargin = px(10)
                setGravity(Gravity.FILL)
            })
            Paint.apply(card, t)
            card.background = Paint.rounded(t.surf, t.r, dp).apply {
                setStroke(px(if (active) 2 else 1).coerceAtLeast(1), if (active) t.acc else t.line)
            }
        }
    }

    private fun px(value: Int) = (value * dp).toInt()
}
