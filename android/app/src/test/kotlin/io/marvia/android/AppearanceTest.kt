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
        fun readable(fg: Int, bg: Int) = androidx.core.graphics.ColorUtils.calculateContrast(fg, bg) >= 4.5
        for (profile in AppearanceProfiles.all) {
            val choice = Look.normalize(profile.choice)
            assertEquals(choice, Look.decode(Look.encode(choice)))
            val t = AppearanceProfiles.theme(choice, profile.id)
            val name = profile.id
            for (surface in listOf(t.surf, t.surf2)) {
                assertTrue("$name: текст", readable(t.fg, surface))
                assertTrue("$name: подпись", readable(t.dim, surface))
            }
            // Прямо на столе — свой цвет, если он у образа есть: чёрный текст
            // 98-й на бирюзе не читается, и там он белый.
            val desk = VibeDetails.desk(profile.id)
            assertTrue("$name: текст на столе", readable(if (desk != 0) desk else t.fg, t.bg))
            assertTrue("$name: подпись на столе", readable(if (desk != 0) desk else t.dim, t.bg))
            assertTrue("$name: кнопка", readable(t.accFg, t.acc))
        }
    }

    @Test fun tuningAVibeKeepsItsDetailsButAnotherPaletteDropsThem() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val store = Store(context)
        AppearanceProfiles.apply(store, AppearanceProfiles.all.first { it.id == "xp" })
        assertEquals("xp", AppearanceProfiles.detail(Store(context)))
        store.look = store.look.copy(accent = 0xFF40C8FF.toInt(), btn = "glass")
        assertEquals("xp", AppearanceProfiles.detail(Store(context)))
        store.look = store.look.copy(preset = "paper")
        assertEquals("", AppearanceProfiles.detail(Store(context)))
    }

    @Test fun aThemeChosenBeforeVibesExistedStillGetsItsDetails() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val store = Store(context)
        val kde = AppearanceProfiles.all.first { it.id == "kde" }
        store.look = kde.choice
        store.pattern = kde.pattern
        store.appearanceStyle = kde.style
        store.font = kde.font
        assertEquals("kde", AppearanceProfiles.detail(Store(context)))
    }

    @Test fun windows98CardContentStaysBelowItsTitleBar() {
        val context = RuntimeEnvironment.getApplication()
        val dp = context.resources.displayMetrics.density
        val card = android.widget.LinearLayout(context).apply { tag = "card" }
        val before = Paint.style
        try {
            val w98 = AppearanceProfiles.all.first { it.id == "w98" }
            Paint.style = Paint.Style(vibe = "w98")
            Paint.apply(card, AppearanceProfiles.theme(w98.choice, "w98"))
            assertEquals((VibeDetails.TITLE * dp).toInt(), card.paddingTop - card.paddingBottom)
            Paint.style = Paint.Style()
            Paint.apply(card, Look.theme(Look.Choice()))
            assertEquals(card.paddingTop, card.paddingBottom)
        } finally {
            Paint.style = before
        }
    }

    @Test fun aVibeInTheSpiritOfASystemPaintsItsOwnPaletteUntilAnotherPaletteIsChosen() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val store = Store(context)
        val win11 = AppearanceProfiles.all.first { it.id == "win11" }
        AppearanceProfiles.apply(store, win11)
        val t = AppearanceProfiles.theme(Store(context))
        assertEquals(0xFF202020.toInt(), t.bg)
        assertEquals(0xFF2D2D2D.toInt(), t.surf)
        assertEquals(8, t.r)
        // Своё скругление в «Форме» — его, а не восьмёрка Windows.
        store.look = store.look.copy(radius = "pill")
        assertEquals(Look.radius("pill"), AppearanceProfiles.theme(Store(context)).r)
        store.look = store.look.copy(preset = "paper")
        assertEquals(Look.theme(store.look), AppearanceProfiles.theme(Store(context)))
    }

    @Test fun theWallpaperGivesWayToAChosenLightAndReturnsWithTheEvenOne() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val store = Store(context)
        AppearanceProfiles.apply(store, AppearanceProfiles.all.first { it.id == "xp" })
        assertEquals("hill", AppearanceProfiles.scene(Store(context))?.wall)
        store.look = store.look.copy(kind = "radial")
        assertEquals(null, AppearanceProfiles.scene(Store(context)))
        store.look = store.look.copy(kind = "flat")
        assertEquals("hill", AppearanceProfiles.scene(Store(context))?.wall)
        AppearanceProfiles.apply(store, AppearanceProfiles.classic)
        assertEquals(null, AppearanceProfiles.scene(Store(context)))
    }

    @Test fun aVibeChosenBeforeTheUpdateMovesToTheClosestNewOne() {
        val context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", 0).edit().clear().commit()
        val store = Store(context)
        // Прежнее «Стекло» на своей палитре — становится macOS целиком.
        store.look = Look.Choice(preset = "midnight", accent = 0xFF5AA9FF.toInt(), kind = "aurora", radius = "pill")
        store.font = "manrope"
        store.vibe = "glass"
        AppearanceProfiles.migrate(store)
        val mac = AppearanceProfiles.all.first { it.id == "mac" }
        assertEquals("mac", Store(context).vibe)
        assertEquals(Look.normalize(mac.choice), Store(context).look)
        assertEquals(Store.FONT_INTER, Store(context).font)
        assertEquals("mac", AppearanceProfiles.detail(Store(context)))
        // Прежний «Хром», уже снятый другой палитрой, — выбор человека: его не трогаем.
        val own = Look.Choice(preset = "paper", accent = 0xFF205E47.toInt())
        store.look = own
        store.vibe = "chrome"
        AppearanceProfiles.migrate(store)
        assertEquals("", Store(context).vibe)
        assertEquals(Look.normalize(own), Store(context).look)
        // Новый образ переезд не трогает.
        AppearanceProfiles.apply(store, mac)
        AppearanceProfiles.migrate(store)
        assertEquals("mac", Store(context).vibe)
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
