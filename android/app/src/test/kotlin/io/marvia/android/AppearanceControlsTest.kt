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
        val ubuntu = AppearanceProfiles.all.first { it.id == "ubuntu" }
        val button = PowerButton(context).apply {
            motionEnabled = false
            theme = AppearanceProfiles.theme(ubuntu.choice, ubuntu.id)
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

    @Test @GraphicsMode(GraphicsMode.Mode.NATIVE)
    fun theXpButtonTurnsGreenWhenConnected() {
        val context = org.robolectric.RuntimeEnvironment.getApplication()
        val xp = AppearanceProfiles.all.first { it.id == "xp" }
        fun centre(state: PowerButton.State): Int {
            val button = PowerButton(context).apply {
                motionEnabled = false
                vibe = xp.id
                theme = AppearanceProfiles.theme(xp.choice, xp.id)
                this.state = state
            }
            val size = 400
            button.measure(View.MeasureSpec.makeMeasureSpec(size, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(size, View.MeasureSpec.EXACTLY))
            button.layout(0, 0, size, size)
            val bitmap = android.graphics.Bitmap.createBitmap(size, size, android.graphics.Bitmap.Config.ARGB_8888)
            button.draw(android.graphics.Canvas(bitmap))
            // Между значком и краем диска — сама заливка кнопки.
            return bitmap.getPixel(size / 2, size * 7 / 10)
        }
        val on = centre(PowerButton.State.ON)
        assertTrue("подключено — зелёная: ${Integer.toHexString(on)}", android.graphics.Color.green(on) > android.graphics.Color.red(on) + 40 && android.graphics.Color.green(on) > android.graphics.Color.blue(on) + 40)
        val off = centre(PowerButton.State.OFF)
        assertTrue("в покое — светлая: ${Integer.toHexString(off)}", android.graphics.Color.red(off) > 180 && android.graphics.Color.blue(off) > 200)
        // Своя кнопка в «Форме» сильнее образа: зелёной «Пуск» больше нет.
        val ring = PowerButton(context).apply {
            motionEnabled = false
            vibe = xp.id
            theme = AppearanceProfiles.theme(xp.choice.copy(btn = "ring"), xp.id)
            state = PowerButton.State.ON
        }
        ring.measure(View.MeasureSpec.makeMeasureSpec(400, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(400, View.MeasureSpec.EXACTLY))
        ring.layout(0, 0, 400, 400)
        val plain = android.graphics.Bitmap.createBitmap(400, 400, android.graphics.Bitmap.Config.ARGB_8888)
        ring.draw(android.graphics.Canvas(plain))
        val px = plain.getPixel(200, 280)
        assertFalse("кольцо не красится зелёным", android.graphics.Color.green(px) > android.graphics.Color.red(px) + 40)
    }

    @Test fun textOnTheWindows98DesktopIsWhiteAndInItsWindowsBlack() = withActivity { activity ->
        val w98 = AppearanceProfiles.all.first { it.id == "w98" }
        val t = AppearanceProfiles.theme(w98.choice, w98.id)
        // Стол — сам фон приложения: корень с тегом bg, как в разметке.
        val root = LinearLayout(activity).apply { tag = "bg" }
        val desk = android.widget.TextView(activity).apply { tag = "fg" }
        // Подпись на своей подложке без тега — не на столе: белой ей не быть.
        val plate = LinearLayout(activity).apply { background = Paint.rounded(t.surf, 0, 1f) }
        val onPlate = android.widget.TextView(activity).apply { tag = "fg" }
        plate.addView(onPlate)
        root.addView(plate)
        val card = LinearLayout(activity).apply { tag = "card" }
        val inside = android.widget.TextView(activity).apply { tag = "fg" }
        card.addView(inside)
        root.addView(desk)
        root.addView(card)
        val before = Paint.style
        try {
            Paint.style = Paint.Style(vibe = w98.id)
            Paint.apply(root, t)
            assertEquals(0xFFFFFFFF.toInt(), desk.currentTextColor)
            assertEquals(0xFF000000.toInt(), inside.currentTextColor)
            assertEquals(0xFF000000.toInt(), onPlate.currentTextColor)
            // Другой образ — стол снова как карточки.
            Paint.style = Paint.Style()
            Paint.apply(root, Look.theme(Look.Choice()))
            assertEquals(Look.theme(Look.Choice()).fg, desk.currentTextColor)
        } finally {
            Paint.style = before
        }
    }

    // Ширину букв меряет только настоящая отрисовка: в упрощённой все буквы равны.
    @Test @GraphicsMode(GraphicsMode.Mode.NATIVE)
    fun aMonospaceFontSetByGruvboxLeavesWithGruvbox() = withActivity { activity ->
        val label = android.widget.TextView(activity).apply { text = "Подключено"; tag = "fg" }
        fun mono() = label.paint.measureText("i") == label.paint.measureText("W")
        val before = Paint.style
        try {
            Paint.style = Paint.Style(font = Store.FONT_MONO)
            Paint.apply(label, Look.theme(Look.Choice()))
            assertTrue(mono())
            Paint.style = Paint.Style(font = Store.FONT_ONEST)
            Paint.apply(label, Look.theme(Look.Choice()))
            assertFalse("строка осталась моноширинной после смены образа", mono())
        } finally {
            Paint.style = before
        }
    }

    @Test @GraphicsMode(GraphicsMode.Mode.NATIVE)
    fun everyThumbnailShowsItsOwnWallpaperBehindTheWindow() {
        val context = org.robolectric.RuntimeEnvironment.getApplication()
        val w = 336
        val h = 184
        val seen = HashSet<Int>()
        for (profile in AppearanceProfiles.all) {
            val preview = AppearancePreview(context, profile)
            preview.measure(View.MeasureSpec.makeMeasureSpec(w, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(h, View.MeasureSpec.EXACTLY))
            preview.layout(0, 0, w, h)
            val bitmap = android.graphics.Bitmap.createBitmap(w, h, android.graphics.Bitmap.Config.ARGB_8888)
            preview.draw(android.graphics.Canvas(bitmap))
            val scene = profile.scene
            if (scene != null) {
                // Угол миниатюры — сами обои, без пелены: как на ПК.
                val wall = Wallpaper.bitmap(scene.wall, w, h)!!
                assertEquals(profile.id, wall.getPixel(3, 3), bitmap.getPixel(3, 3))
            }
            val pixels = IntArray(w * h)
            bitmap.getPixels(pixels, 0, w, 0, 0, w, h)
            assertTrue("миниатюра ${profile.id} совпала с другой", seen.add(pixels.contentHashCode()))
        }
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

    @Test fun theWindows98ButtonStandsWithoutOrbitsLikeOnThePc() = withActivity { activity ->
        val ui = ScreenConnectBinding.inflate(activity.layoutInflater)
        val w98 = AppearanceProfiles.all.first { it.id == "w98" }
        val before = Paint.style
        try {
            Paint.style = Paint.Style(vibe = w98.id)
            ConnectionAppearance.apply(ui, AppearanceStyle.CLASSIC, AppearanceProfiles.theme(w98.choice, w98.id))
            assertTrue(ui.powerAction.square)
            assertFalse(ui.halo.isVisible)
            Paint.style = Paint.Style()
            ConnectionAppearance.apply(ui, AppearanceStyle.CLASSIC, Look.theme(Look.Choice()))
            assertFalse(ui.powerAction.square)
            assertTrue(ui.halo.isVisible)
        } finally {
            Paint.style = before
        }
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
