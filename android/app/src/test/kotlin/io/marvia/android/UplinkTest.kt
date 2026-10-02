package io.marvia.android

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class UplinkTest {
    @Test
    fun noNetworkAfterSettlingMeansOffline() {
        assertTrue(Uplink.isOffline(since = 1_000, networks = 0, now = 1_000 + Uplink.SETTLE_MS))
    }

    @Test
    fun firstMomentsAfterStartAreNotReportedAsOffline() {
        assertFalse(Uplink.isOffline(since = 1_000, networks = 0, now = 1_500))
    }

    @Test
    fun anyNetworkMeansOnline() {
        assertFalse(Uplink.isOffline(since = 1_000, networks = 1, now = 60_000))
    }

    @Test
    fun notStartedIsNeverOffline() {
        assertFalse(Uplink.isOffline(since = 0, networks = 0, now = 60_000))
    }
}
