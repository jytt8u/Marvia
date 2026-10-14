package io.marvia.android

import android.app.Application
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.isVisible
import io.marvia.android.databinding.ScreenConnectBinding
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(application = Application::class, sdk = [35], qualifiers = "ru")
class HomeLayoutTest {
    @Test fun themesDifferInLayoutNotOnlyInColour() {
        val layouts = AppearanceProfiles.all.map { HomeLayout.of(it.id, it.style) }.toSet()
        assertEquals("раскладки главной у образов: $layouts", HomeLayout.entries.toSet(), layouts)
    }

    @Test fun theConsoleHidesTheRoundButtonOnlyWithAWideOneInItsPlace() = withScreen { ui ->
        arrange(ui, "terminal")
        assertFalse(ui.stageFrame.isVisible)
        assertTrue("без круглой кнопки должна остаться другая", ui.consoleAction.isVisible)
    }

    @Test fun theDeckPutsTheButtonBesideTheStatusAndGivesItBackOnThemeChange() = withScreen { ui ->
        arrange(ui, "expressive")
        assertSame(ui.deckSlot, ui.powerAction.parent)
        assertSame(ui.deckLeft, ui.statusBox.parent)
        arrange(ui, "graphite")
        assertSame(ui.stageFrame, ui.powerAction.parent)
        assertSame(ui.homeColumn, ui.statusBox.parent)
        assertTrue(ui.stageFrame.isVisible)
        assertFalse(ui.deckRow.isVisible)
    }

    @Test fun subscriptionCardShowsDaysAndWhatIsLeftFromTheSellersData() = withScreen { ui ->
        val until = java.time.Instant.now().plusSeconds(5L * 86400 + 600).toString()
        val plan = HomeBlocks.parsePlan("Phone", """{"until":"$until","limit":107374182400,"left":53687091200}""")!!
        HomeBlocks.plan(ui, plan, Look.theme(Look.Choice()), HomeLayout.STAGE)
        assertTrue(ui.planCard.isVisible)
        assertEquals(Format.daysLeft(until).toString(), ui.planDays.text.toString())
        assertTrue(ui.planLeft.text.toString(), ui.planLeft.text.contains("50"))
        // Продавец не оставил ссылку «Продлить» — кнопки нет: нажать её было бы некуда.
        assertFalse(ui.planRenew.isVisible)
    }

    @Test fun placeCardShowsWhereYouAreLargeAndTheNodeIdSmall() = withScreen { ui ->
        HomeBlocks.place(ui, HomeBlocks.Place("Финляндия · Хельсинки · fi-1", 26, live = true, manual = false), Look.theme(Look.Choice()))
        assertEquals("Финляндия · Хельсинки", ui.placeName.text.toString())
        assertTrue(ui.placeSub.text.toString(), ui.placeSub.text.startsWith("fi-1"))
    }

    private fun arrange(ui: ScreenConnectBinding, vibe: String) {
        val profile = AppearanceProfiles.all.first { it.id == vibe }
        Paint.style = Paint.style.copy(vibe = vibe, appearanceStyle = profile.style)
        ConnectionAppearance.apply(ui, profile.style, Look.theme(Look.normalize(profile.choice)))
    }

    private fun withScreen(test: (ScreenConnectBinding) -> Unit) {
        val controller = Robolectric.buildActivity(AppCompatActivity::class.java)
        val activity = controller.get()
        activity.setTheme(R.style.Theme_Veil)
        controller.setup()
        try {
            test(ScreenConnectBinding.inflate(activity.layoutInflater))
        } finally {
            Paint.style = Paint.Style()
            controller.pause().stop().destroy()
        }
    }
}
