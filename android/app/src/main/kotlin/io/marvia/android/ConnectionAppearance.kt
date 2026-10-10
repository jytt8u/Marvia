package io.marvia.android

import android.widget.LinearLayout
import androidx.core.view.isVisible
import androidx.core.view.updateLayoutParams
import io.marvia.android.databinding.ScreenConnectBinding

/** Одинаковая композиция на главной и в предпросмотре темы. */
object ConnectionAppearance {
    fun apply(ui: ScreenConnectBinding, style: AppearanceStyle, theme: Theme = ui.powerAction.theme) {
        val signal = style == AppearanceStyle.SIGNAL
        val dp = ui.root.resources.displayMetrics.density
        fun px(value: Int) = (value * dp).toInt()
        ui.powerAction.appearanceStyle = style
        ui.powerAction.theme = theme
        header(ui, style)
        ui.statusText.textSize = if (signal) 30f else 28f
        ui.heroMarkButton.updateLayoutParams { width = px(48); height = px(48) }
        val largeText = ui.root.resources.configuration.fontScale > 1.5f
        val buttonSize = px(if (largeText) 144 else 208)
        ui.powerAction.updateLayoutParams { width = buttonSize; height = buttonSize }
        (ui.powerAction.parent as android.view.View).updateLayoutParams { height = px(if (largeText) 164 else 238) }
        ui.todayCard.tag = if (signal) "card" else "card feature"
        ui.todayBars.updateLayoutParams { height = px(if (signal) 44 else 52) }
        ui.statsGap.minimumHeight = px(if (signal) 0 else 18)
        ui.statsGap.updateLayoutParams<LinearLayout.LayoutParams> {
            height = px(if (signal) 22 else 0)
            weight = if (signal) 0f else 1f
        }
        ui.halo.isVisible = !ui.powerAction.dottedPower
    }

    fun header(ui: ScreenConnectBinding, style: AppearanceStyle) {
        // Раскрытое имя и статус вместе не помещаются при крупном шрифте.
        // Полное состояние подключения уже есть под кнопкой.
        val compact = style == AppearanceStyle.SIGNAL && !ui.heroName.isVisible
        ui.heroDots.isVisible = compact
        ui.heroSpacer.isVisible = !ui.heroName.isVisible
        ui.connectionBadge.isVisible = compact && ui.root.resources.configuration.fontScale <= 1.5f
    }
}
