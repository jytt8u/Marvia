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
    @Test fun theThemeChangesColoursButNeverTheLayoutAndMarviaIsTheDefault() = withScreen { ui ->
        assertEquals(HomeLayout.MARVIA, HomeLayout.of(""))
        assertEquals("незнакомый ключ — главная Marvia", HomeLayout.MARVIA, HomeLayout.of("signal"))
        for (profile in AppearanceProfiles.all) {
            Paint.style = Paint.style.copy(vibe = profile.id, appearanceStyle = profile.style)
            ConnectionAppearance.apply(ui, profile.style, Look.theme(Look.normalize(profile.choice)))
            assertTrue("образ ${profile.id} спрятал круглую кнопку", ui.stageFrame.isVisible)
            assertSame("образ ${profile.id} переставил кнопку", ui.stageFrame, ui.powerAction.parent)
            assertFalse("образ ${profile.id} включил консоль", ui.consoleAction.isVisible)
        }
    }

    @Test fun theMarviaHomeHasTheNodeLineAndNoCards() = withScreen { ui ->
        val plan = HomeBlocks.parsePlan("Phone", """{"until":"2099-01-01T00:00:00Z","limit":1,"left":1}""")!!
        HomeBlocks.plan(ui, plan, Look.theme(Look.Choice()), HomeLayout.MARVIA)
        assertFalse("у главной Marvia карточки подписки нет", ui.planCard.isVisible)
        ui.nodeLine.text = "Финляндия · Хельсинки"
        arrange(ui, "marvia")
        assertTrue(ui.nodeLine.isVisible)
    }

    @Test fun theConsoleHidesTheRoundButtonOnlyWithAWideOneInItsPlace() = withScreen { ui ->
        arrange(ui, "console")
        assertFalse(ui.stageFrame.isVisible)
        assertTrue("без круглой кнопки должна остаться другая", ui.consoleAction.isVisible)
    }

    @Test fun theDeckPutsTheButtonBesideTheStatusAndGivesItBackWhenTheViewChanges() = withScreen { ui ->
        arrange(ui, "deck")
        assertSame(ui.deckSlot, ui.powerAction.parent)
        assertSame(ui.deckLeft, ui.statusBox.parent)
        arrange(ui, "marvia")
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

    @Test fun aNodeTheCoreMovedToIsNotCalledYourPick() {
        assertTrue(HomeBlocks.pickedHere("Финляндия · Хельсинки · fi-1", "Финляндия · Хельсинки · fi-1"))
        assertFalse("выбранная замолчала, трафик идёт через другую", HomeBlocks.pickedHere("ОАЭ · Дубай · ae-1", "Финляндия · Хельсинки · fi-1"))
        assertFalse("автовыбор — не выбор человека", HomeBlocks.pickedHere("", "Финляндия · Хельсинки · fi-1"))
    }

    private fun arrange(ui: ScreenConnectBinding, home: String) {
        Paint.style = Paint.style.copy(home = home)
        ConnectionAppearance.apply(ui, AppearanceStyle.CLASSIC, Look.theme(Look.Choice()))
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
