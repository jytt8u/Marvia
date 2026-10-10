package io.marvia.android

import android.app.Application
import android.view.View
import android.view.ViewGroup
import android.widget.GridLayout
import android.widget.LinearLayout
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.isVisible
import io.marvia.android.databinding.ScreenConnectBinding
import io.marvia.android.databinding.ViewNavBinding
import com.google.android.material.materialswitch.MaterialSwitch
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(application = Application::class, sdk = [35])
class AppearanceControlsTest {
    @Test @GraphicsMode(GraphicsMode.Mode.NATIVE)
    fun aSolidButtonWithGlowIsPaintedInFullColour() {
        val context = org.robolectric.RuntimeEnvironment.getApplication()
        val clay = AppearanceProfiles.all.first { it.id == "clay" }
        val button = PowerButton(context).apply {
            motionEnabled = false
            theme = Look.theme(clay.choice)
            state = PowerButton.State.ON
        }
        val size = 400
        button.measure(View.MeasureSpec.makeMeasureSpec(size, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(size, View.MeasureSpec.EXACTLY))
        button.layout(0, 0, size, size)
        val bitmap = android.graphics.Bitmap.createBitmap(size, size, android.graphics.Bitmap.Config.ARGB_8888)
        button.draw(android.graphics.Canvas(bitmap))
        // Под значком питания — сам диск: он непрозрачный, а не блёклый.
        val disc = bitmap.getPixel(size / 2, size * 3 / 4)
        assertTrue("альфа диска ${android.graphics.Color.alpha(disc)}", android.graphics.Color.alpha(disc) > 240)
    }

    @Test fun choosingALookAppliesItsPaletteAndBackgroundTogether() = withActivity { activity ->
        val store = Store(activity)
        store.look = Look.Choice(preset = "paper")
        store.pattern = "grid"
        val grid = GridLayout(activity).apply { columnCount = 2 }
        var changes = 0
        val picker = AppearancePicker(activity, grid, store) { changes++ }
        picker.render(Look.theme(store.look))
        assertEquals("paper", store.look.preset)
        // Точечное табло ищем по виду, а не по месту в ленте: порядок образов меняется.
        val dotted = AppearanceProfiles.all.indexOfFirst { it.style == AppearanceStyle.SIGNAL }
        grid.getChildAt(dotted).performClick()
        assertEquals(AppearanceProfiles.all.first { it.style == AppearanceStyle.SIGNAL }.choice, store.look)
        assertEquals(AppearanceStyle.SIGNAL, store.appearanceStyle)
        assertEquals(Store.PATTERN_DOTS, store.pattern)
        assertEquals(1, changes)
        picker.render(Look.theme(store.look))
        assertTrue(grid.getChildAt(dotted).isSelected)
        val width = View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED)
        grid.measure(width, View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED))
        grid.layout(0, 0, grid.measuredWidth, grid.measuredHeight)
        for (i in 0 until grid.childCount) {
            val card = grid.getChildAt(i)
            assertTrue(card.width > 0)
            assertTrue(card.right <= grid.measuredWidth)
            assertTrue(card.left >= 0)
        }
        grid.getChildAt(0).performClick()
        assertEquals(AppearanceStyle.CLASSIC, store.appearanceStyle)
        assertEquals(2, changes)
    }

    @Test fun switchingBackFromNothingRestoresTheClassicConnectionScreen() = withActivity { activity ->
        val ui = ScreenConnectBinding.inflate(activity.layoutInflater)
        val theme = Look.theme(AppearanceProfiles.all.first { it.style == AppearanceStyle.SIGNAL }.choice)
        ConnectionAppearance.apply(ui, AppearanceStyle.SIGNAL, theme)
        assertEquals(AppearanceStyle.SIGNAL, ui.powerAction.appearanceStyle)
        assertTrue(ui.heroDots.isVisible)
        assertTrue(ui.connectionBadge.isVisible)
        assertFalse(ui.halo.isVisible)
        assertTrue(ui.powerAction.dottedPower)
        assertEquals("card", ui.todayCard.tag)
        ConnectionAppearance.apply(ui, AppearanceStyle.CLASSIC)
        assertEquals(AppearanceStyle.CLASSIC, ui.powerAction.appearanceStyle)
        assertFalse(ui.heroDots.isVisible)
        assertFalse(ui.connectionBadge.isVisible)
        assertTrue(ui.halo.isVisible)
        assertFalse(ui.powerAction.dottedPower)
        assertEquals("card feature", ui.todayCard.tag)
    }

    @Test fun displaySwitchesSavePreferencesAndRequestImmediateRepaint() = withActivity { activity ->
        val store = Store(activity)
        val rows = LinearLayout(activity)
        var changes = 0
        AppearanceSettings(activity, rows, store, { Look.theme(store.look) }, { changes++ }).render()
        val switches = descendants(rows).filterIsInstance<MaterialSwitch>()
        assertEquals(3, switches.size)
        fun click(title: Int) = switches.single { it.contentDescription == activity.getString(title) }.performClick()
        click(R.string.appearance_motion)
        click(R.string.appearance_haptics)
        click(R.string.appearance_home_stats)
        assertTrue(store.reduceMotion)
        assertFalse(store.hapticFeedback)
        assertFalse(store.homeStats)
        assertEquals(3, changes)
    }

    @Test fun manualButtonSelectionStillWorksInsideNothing() = withActivity { activity ->
        val ui = ScreenConnectBinding.inflate(activity.layoutInflater)
        val theme = Look.theme(AppearanceProfiles.all.first { it.style == AppearanceStyle.SIGNAL }.choice)
        ConnectionAppearance.apply(ui, AppearanceStyle.SIGNAL, theme)
        assertTrue(ui.powerAction.dottedPower)
        ConnectionAppearance.apply(ui, AppearanceStyle.SIGNAL, theme.copy(btn = "bare"))
        assertFalse(ui.powerAction.dottedPower)
        assertTrue(ui.halo.isVisible)
        assertTrue(ui.connectionBadge.isVisible)
    }

    @Test fun expandedBrandDoesNotPushTheConnectionBadgeOffScreen() = withActivity { activity ->
        val ui = ScreenConnectBinding.inflate(activity.layoutInflater)
        ui.heroName.isVisible = true
        ConnectionAppearance.apply(ui, AppearanceStyle.SIGNAL, Look.theme(AppearanceProfiles.all.first { it.style == AppearanceStyle.SIGNAL }.choice))
        assertTrue(ui.heroName.isVisible)
        assertFalse(ui.heroDots.isVisible)
        assertFalse(ui.connectionBadge.isVisible)
        ui.heroName.isVisible = false
        ConnectionAppearance.apply(ui, AppearanceStyle.SIGNAL)
        assertTrue(ui.heroDots.isVisible)
        assertTrue(ui.connectionBadge.isVisible)
    }

    @Test @Config(qualifiers = "w320dp-h711dp-420dpi")
    @GraphicsMode(GraphicsMode.Mode.NATIVE)
    fun navigationLabelsKeepAGapWithLargeText() = withActivity { checkNavigationSpacing(it) }

    @Test @Config(sdk = [24], qualifiers = "w320dp-h711dp-420dpi")
    fun navigationLabelsKeepAGapOnAndroidSeven() = withActivity { checkNavigationSpacing(it) }

    private fun checkNavigationSpacing(activity: AppCompatActivity) {
        val context = android.view.ContextThemeWrapper(activity, R.style.Theme_Veil)
        context.applyOverrideConfiguration(android.content.res.Configuration(activity.resources.configuration).apply { fontScale = 1.3f })
        val resources = context.resources
        val ui = ViewNavBinding.inflate(activity.layoutInflater.cloneInContext(context))
        val t = Look.theme(Look.Choice())
        Paint.apply(ui.root, t)
        val columns = listOf(
            Triple(ui.navConnect, ui.navConnectLabel, "Туннель"),
            Triple(ui.navServers, ui.navServersLabel, "Серверы"),
            Triple(ui.navStats, ui.navStatsLabel, "Расход"),
            Triple(ui.navTheme, ui.navThemeLabel, "Тема"),
            Triple(ui.navMore, ui.navMoreLabel, "Настройки"),
        )
        columns.forEach { (_, label, text) -> label.text = text }
        Paint.navigationTab(ui.navConnectPill, ui.navConnectIcon, ui.navConnectLabel, true, t)
        val dp = resources.displayMetrics.density
        val width = (296 * dp).toInt()
        ui.root.measure(View.MeasureSpec.makeMeasureSpec(width, View.MeasureSpec.EXACTLY),
            View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED))
        ui.root.layout(0, 0, width, ui.root.measuredHeight)
        val gap = (2 * dp).toInt()
        for ((column, label, _) in columns) {
            assertTrue("${label.text}: отступ слева", label.left + label.paddingLeft >= gap)
            assertTrue("${label.text}: отступ справа", label.right - label.paddingRight <= column.width - gap)
        }
    }

    private fun descendants(view: View): List<View> = listOf(view) +
        if (view is ViewGroup) (0 until view.childCount).flatMap { descendants(view.getChildAt(it)) } else emptyList()

    private fun withActivity(test: (AppCompatActivity) -> Unit) {
        val controller = Robolectric.buildActivity(AppCompatActivity::class.java)
        val activity = controller.get()
        activity.setTheme(R.style.Theme_Veil)
        controller.setup()
        activity.getSharedPreferences("veil", 0).edit().clear().commit()
        try { test(activity) } finally { controller.pause().stop().destroy() }
    }
}
