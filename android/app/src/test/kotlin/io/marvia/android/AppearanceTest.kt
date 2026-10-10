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
    @Test fun newInstallationKeepsTheClassicThemeUntilAnotherThemeIsSelected() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val look = Store(context).look
        assertEquals(Look.normalize(Look.Choice()), look)
        assertEquals(AppearanceStyle.CLASSIC, Store(context).appearanceStyle)
    }

    @Test fun onlyTheDotBoardsOptIntoTheDottedInterface() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val store = Store(context)
        for (profile in AppearanceProfiles.all) {
            AppearanceProfiles.apply(store, profile)
            val expected = if (profile.id == "dot" || profile.id == "paper") AppearanceStyle.SIGNAL else AppearanceStyle.CLASSIC
            assertEquals(expected, Store(context).appearanceStyle)
            assertEquals(profile, AppearanceProfiles.selected(Store(context)))
        }
    }

    @Test fun adjustingAndSavingNothingKeepsItsInterfaceStyle() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val store = Store(context)
        AppearanceProfiles.apply(store, AppearanceProfiles.all.first { it.style == AppearanceStyle.SIGNAL })
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
        store.look = AppearanceProfiles.all.first { it.style == AppearanceStyle.SIGNAL }.choice
        assertEquals(AppearanceStyle.CLASSIC, Store(context).appearanceStyle)
        assertEquals(null, AppearanceProfiles.selected(store))
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

    @Test fun displayPreferencesSurviveRestartWithoutChangingNetworkOptions() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val store = Store(context)
        val mtu = store.vpnMtu
        val dns = store.dns
        store.reduceMotion = true
        store.hapticFeedback = false
        store.homeStats = false
        MonitoringProfiles.apply(store, 0)
        val reopened = Store(context)
        assertTrue(reopened.reduceMotion)
        assertEquals(false, reopened.hapticFeedback)
        assertEquals(false, reopened.homeStats)
        assertEquals(10, reopened.liveRefreshSeconds)
        assertEquals(60, reopened.pingIntervalSeconds)
        assertEquals(mtu, reopened.vpnMtu)
        assertEquals(dns, reopened.dns)
    }
}
