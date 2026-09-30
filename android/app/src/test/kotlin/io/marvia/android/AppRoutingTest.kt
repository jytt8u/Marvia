package io.marvia.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AppRoutingTest {
    @Test
    fun removedSelectionDoesNotTurnIntoAllApplications() {
        assertFalse(AppRouting.addIncluded(setOf("removed.app"), "io.marvia.android") { false })
    }

    @Test
    fun remainingApplicationsKeepTheirSelection() {
        val added = mutableListOf<String>()
        val accepted = AppRouting.addIncluded(setOf("removed.app", "installed.app"), "io.marvia.android") {
            if (it == "installed.app") { added.add(it); true } else false
        }
        assertTrue(accepted)
        assertEquals(listOf("installed.app"), added)
    }

    @Test
    fun marviaCannotBecomeItsOwnAllowedApplication() {
        assertFalse(AppRouting.addIncluded(setOf("io.marvia.android"), "io.marvia.android") {
            throw AssertionError("свой трафик нельзя направлять в собственный туннель")
        })
    }

    @Test
    fun explicitlyEmptySelectionKeepsTheDocumentedAllApplicationsMode() {
        assertTrue(AppRouting.addIncluded(emptySet(), "io.marvia.android") {
            throw AssertionError("в пустом списке нет пакетов")
        })
    }
}
