package io.marvia.android

import android.app.Application
import android.content.res.Configuration
import android.view.ContextThemeWrapper
import android.view.View
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.isVisible
import io.marvia.android.databinding.ScreenConnectBinding
import io.marvia.android.databinding.ScreenStatsBinding
import io.marvia.android.databinding.ViewNavBinding
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(application = Application::class, sdk = [35], qualifiers = "ru-w320dp-h711dp-420dpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class InterfacePolishTest {
    @Test fun theFirstLookIsTheOriginalGreyMarvia() {
        assertEquals(AppearanceProfiles.classic, AppearanceProfiles.all.first())
    }

    @Test fun missingSubscriptionDataDoesNotPromiseUnlimitedTraffic() = withActivity { activity ->
        val ui = ScreenStatsBinding.inflate(activity.layoutInflater)
        val store = Store(activity)
        StatsScreen(activity, ui, { Look.theme(store.look) }, Traffic(activity), store).paint(Look.theme(store.look))
        assertEquals("—", ui.factQuota.text.toString())
        assertNotEquals(activity.getString(R.string.stats_unlimited), ui.factQuotaNote.text.toString())
        assertTrue(ui.statsEmpty.isVisible)
        assertFalse(ui.trendCard.isVisible)
        assertFalse(ui.statsDetailsToggle.isVisible)
    }

    @Test fun anExplicitUnlimitedQuotaIsDifferentFromMissingOrInvalidData() {
        assertNull(AccountQuota.read(""))
        assertNull(AccountQuota.read("{}"))
        assertNull(AccountQuota.read("{\"limit\":-1}"))
        assertNull(AccountQuota.read("{\"limit\":\"unknown\"}"))
        assertEquals(AccountQuota(0, 0, ""), AccountQuota.read("{\"limit\":0}"))
        assertEquals(AccountQuota(100, 40, ""), AccountQuota.read("{\"limit\":100,\"left\":40}"))
    }

    @Test fun theCompactHeaderFitsWithDoubleSizeText() = withActivity { activity ->
        val context = largeText(activity)
        val ui = ScreenConnectBinding.inflate(activity.layoutInflater.cloneInContext(context))
        ConnectionAppearance.apply(ui, AppearanceStyle.SIGNAL)
        measure(ui.root, context, 320)
        if (ui.connectionBadge.isVisible) {
            assertTrue("статус должен помещаться целиком", ui.connectionBadge.paint.measureText(ui.connectionBadge.text.toString()) <=
                ui.connectionBadge.width - ui.connectionBadge.paddingLeft - ui.connectionBadge.paddingRight)
        }
    }

    @Test fun navigationKeepsEveryLabelReadableWithDoubleSizeText() = withActivity { activity ->
        val context = largeText(activity)
        val ui = ViewNavBinding.inflate(activity.layoutInflater.cloneInContext(context))
        Paint.apply(ui.root, Look.theme(Look.Choice()))
        val labels = listOf(ui.navConnectLabel, ui.navServersLabel, ui.navStatsLabel, ui.navThemeLabel, ui.navMoreLabel)
        labels.zip(listOf("Туннель", "Серверы", "Расход", "Тема", "Настройки")).forEach { (label, title) -> label.text = title }
        measure(ui.root, context, 296)
        labels.forEach { label ->
            val layout = label.layout
            assertNotNull(layout)
            for (line in 0 until layout.lineCount) {
                assertEquals("${label.text}: подпись обрезана", 0, layout.getEllipsisCount(line))
            }
        }
    }

    @Test fun theStatisticsHeadingDoesNotBreakIntoNarrowFragments() = withActivity { activity ->
        val context = largeText(activity)
        val ui = ScreenStatsBinding.inflate(activity.layoutInflater.cloneInContext(context))
        val store = Store(activity)
        StatsScreen(activity, ui, { Look.theme(store.look) }, Traffic(activity), store).paint(Look.theme(store.look))
        measure(ui.root, context, 320)
        val heading = ui.rangeBox.parent as LinearLayout
        val title = (heading.getChildAt(0) as LinearLayout).getChildAt(0) as TextView
        assertEquals("заголовок должен читаться целиком", 1, title.layout.lineCount)
    }

    @Test fun addingAccessRemainsVisibleWithoutScrollingWithLargeText() = withActivity { activity ->
        val context = largeText(activity)
        val ui = ScreenConnectBinding.inflate(activity.layoutInflater.cloneInContext(context))
        ui.statusText.setText(R.string.connect_signal_ready)
        ui.powerHint.setText(R.string.connect_signal_add)
        ui.accessAction.isVisible = true
        ui.todayCard.isVisible = false
        ui.tilesRow.isVisible = false
        ui.nodeLine.isVisible = false
        ui.nodeNote.isVisible = false
        ConnectionAppearance.apply(ui, AppearanceStyle.SIGNAL)
        val dp = context.resources.displayMetrics.density
        val width = (320 * dp).toInt()
        val height = (550 * dp).toInt()
        ui.root.measure(View.MeasureSpec.makeMeasureSpec(width, View.MeasureSpec.EXACTLY),
            View.MeasureSpec.makeMeasureSpec(height, View.MeasureSpec.EXACTLY))
        ui.root.layout(0, 0, width, height)
        val box = android.graphics.Rect(0, 0, ui.accessAction.width, ui.accessAction.height)
        ui.root.offsetDescendantRectToMyCoords(ui.accessAction, box)
        assertTrue("добавление доступа: ${box.bottom}, доступная высота: $height", box.bottom <= height)
    }

    private fun largeText(activity: AppCompatActivity) = ContextThemeWrapper(activity, R.style.Theme_Veil).apply {
        applyOverrideConfiguration(Configuration(activity.resources.configuration).apply { fontScale = 2f })
    }

    private fun measure(view: View, context: android.content.Context, widthDp: Int) {
        val width = (widthDp * context.resources.displayMetrics.density).toInt()
        view.measure(View.MeasureSpec.makeMeasureSpec(width, View.MeasureSpec.EXACTLY),
            View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED))
        view.layout(0, 0, width, view.measuredHeight)
    }

    private fun withActivity(test: (AppCompatActivity) -> Unit) {
        val controller = Robolectric.buildActivity(AppCompatActivity::class.java)
        val activity = controller.get()
        activity.setTheme(R.style.Theme_Veil)
        controller.setup()
        activity.getSharedPreferences("veil", 0).edit().clear().commit()
        try { test(activity) } finally { controller.pause().stop().destroy() }
    }
}
