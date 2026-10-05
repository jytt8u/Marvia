package io.marvia.android

import org.junit.Assert.*
import org.junit.Test

class NetworkMonitorTest {
    @Test fun initialNetworkAndRepeatedCapabilitiesDoNotRestartVpn() {
        val state = NetworkChangeState()
        assertFalse(state.available(1))
        assertFalse(state.available(1))
        assertEquals(0L, state.snapshot().first)
        assertTrue(state.snapshot().second)
    }
    @Test fun losingFormerNetworkDoesNotDropTheNewDefault() {
        val state = NetworkChangeState()
        state.available(1)
        assertTrue(state.available(2))
        assertFalse(state.lost(1))
        assertTrue(state.snapshot().second)
        assertTrue(state.lost(2))
        assertFalse(state.snapshot().second)
        assertTrue(state.available(1))
        assertEquals(3L, state.snapshot().first)
    }
}
