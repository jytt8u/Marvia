package io.marvia.android

import android.app.Application
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(application = Application::class, sdk = [28])
class AppearanceTest {
    private val DOT = Look.Choice(preset = "oled", accent = 0xFFFF3B30.toInt(), kind = "flat", dir = "n", depth = 0.35,
        radius = "soft", density = "normal", btn = "ring", glow = "none", card = "outline")

    @Test fun newInstallationKeepsTheClassicThemeUntilAnotherThemeIsSelected() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val look = Store(context).look
        assertEquals(Look.normalize(Look.Choice()), look)
        assertEquals(AppearanceStyle.CLASSIC, Store(context).appearanceStyle)
    }

    @Test fun noVibeOptsIntoTheDottedInterface() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val store = Store(context)
        for (profile in AppearanceProfiles.all) {
            AppearanceProfiles.apply(store, profile)
            assertEquals(AppearanceStyle.CLASSIC, Store(context).appearanceStyle)
            assertEquals(profile, AppearanceProfiles.selected(Store(context)))
        }
    }

    @Test fun adjustingAndSavingNothingKeepsItsInterfaceStyle() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val store = Store(context)
        store.look = DOT
        store.appearanceStyle = AppearanceStyle.SIGNAL
        store.look = store.look.copy(accent = 0xFFBBDC78.toInt())
        store.profiles = listOf(Look.encode(store.look), null, null)
        store.saveProfileStyle(0)
        assertEquals(AppearanceStyle.SIGNAL, Store(context).appearanceStyle)
        AppearanceProfiles.apply(store, AppearanceProfiles.classic)
        store.look = Look.decode(store.profiles.first().orEmpty())!!
        store.appearanceStyle = store.profileStyle(0)
        assertEquals(AppearanceStyle.SIGNAL, Store(context).appearanceStyle)
        assertEquals(0xFFBBDC78.toInt(), store.look.accent)
        assertEquals(AppearanceStyle.CLASSIC, store.profileStyle(1))
    }

    @Test fun aSavedFlatPaletteDoesNotOptIntoNothing() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val store = Store(context)
        store.look = DOT
        assertEquals(AppearanceStyle.CLASSIC, Store(context).appearanceStyle)
        assertEquals(null, AppearanceProfiles.selected(store))
    }

    @Test fun aRetiredVibeTurnsIntoTheClosestNewOneOnceAndACustomThemeStays() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val store = Store(context)
        store.vibe = "terminal"
        store.look = Look.Choice(preset = "emerald")
        AppearanceProfiles.migrate(store)
        assertEquals("brass", Store(context).vibe)
        assertEquals("brass", Store(context).look.preset)
        // Своя тема без образа остаётся своей.
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        store.look = Look.Choice(preset = "rose")
        AppearanceProfiles.migrate(store)
        assertEquals("rose", Store(context).look.preset)
        // Свежая установка получает образ из коробки.
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        AppearanceProfiles.migrate(store)
        assertEquals(AppearanceProfiles.classic.id, Store(context).vibe)
    }

    @Test fun savedAppearanceSurvivesReopeningTheStore() {
        val context = RuntimeEnvironment.getApplication()
        val store = Store(context)
        val custom = Look.Choice(preset = "paper", kind = "flat", accent = 0xFF205E47.toInt())
        store.look = custom
        assertEquals(Look.normalize(custom), Store(context).look)
    }

    @Test fun legacyLightAppearanceIsNotReplacedByTheNewDefault() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().putInt("theme", 1).commit()
        assertEquals("daylight", Store(context).look.preset)
    }

    @Test fun curatedLooksKeepReadableTextAndCanBeShared() {
        for (profile in AppearanceProfiles.all) {
            val choice = Look.normalize(profile.choice)
            assertEquals(choice, Look.decode(Look.encode(choice)))
            val t = Look.theme(choice)
            for (surface in listOf(t.bg, t.surf, t.surf2)) {
                assertTrue("${profile.name}: текст", androidx.core.graphics.ColorUtils.calculateContrast(t.fg, surface) >= 4.5)
                assertTrue("${profile.name}: подпись", androidx.core.graphics.ColorUtils.calculateContrast(t.dim, surface) >= 4.5)
            }
            assertTrue(androidx.core.graphics.ColorUtils.calculateContrast(t.accFg, t.acc) >= 4.5)
        }
    }

    @Test fun tuningAVibeKeepsItsDetailsButAnotherPaletteDropsThem() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val store = Store(context)
        AppearanceProfiles.apply(store, AppearanceProfiles.all.first { it.id == "kiln" })
        assertEquals("kiln", AppearanceProfiles.detail(Store(context)))
        store.look = store.look.copy(accent = 0xFF40C8FF.toInt(), btn = "glass")
        assertEquals("kiln", AppearanceProfiles.detail(Store(context)))
        store.look = store.look.copy(preset = "paper")
        assertEquals("", AppearanceProfiles.detail(Store(context)))
    }

    @Test fun aThemeChosenBeforeVibesExistedStillGetsItsDetails() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val store = Store(context)
        val board = AppearanceProfiles.all.first { it.id == "signboard" }
        store.look = board.choice
        store.pattern = board.pattern
        store.appearanceStyle = board.style
        store.font = board.font
        assertEquals("signboard", AppearanceProfiles.detail(Store(context)))
    }

    @Test fun brutalCardContentStaysClearOfItsHardShadow() {
        val context = RuntimeEnvironment.getApplication()
        val dp = context.resources.displayMetrics.density
        val card = android.widget.LinearLayout(context).apply { tag = "card" }
        val before = Paint.style
        try {
            Paint.style = Paint.Style(vibe = "brutal")
            Paint.apply(card, Look.theme(Look.Choice(preset = "linen", radius = "sharp", card = "outline")))
            assertEquals((VibeDetails.HARD * dp).toInt(), card.paddingRight - card.paddingLeft)
            Paint.style = Paint.Style()
            Paint.apply(card, Look.theme(Look.Choice()))
            assertEquals(card.paddingLeft, card.paddingRight)
        } finally {
            Paint.style = before
        }
    }

    @Test fun displayPreferencesSurviveRestartWithoutChangingNetworkOptions() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val store = Store(context)
        val mtu = store.vpnMtu
        val dns = store.dns
        store.reduceMotion = true
        store.homeStats = false
        MonitoringProfiles.apply(store, 0)
        val reopened = Store(context)
        assertTrue(reopened.reduceMotion)
        assertEquals(false, reopened.homeStats)
        assertEquals(10, reopened.liveRefreshSeconds)
        assertEquals(60, reopened.pingIntervalSeconds)
        assertEquals(mtu, reopened.vpnMtu)
        assertEquals(dns, reopened.dns)
    }
}
