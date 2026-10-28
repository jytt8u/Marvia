package io.marvia.android

import android.app.Application
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(application = Application::class, sdk = [35])
class ConnectionCancelTest {
    @After fun resetState() { MarviaState.set(TunnelState.Off) }

    @Test fun pressingPowerWhileConnectingStopsTheService() {
        val activity = Robolectric.buildActivity(MainActivity::class.java).get()
        MarviaState.set(TunnelState.Connecting)
        // Вызываем тот же обработчик, что и кнопка, без запуска сетевых экранов.
        MainActivity::class.java.getDeclaredMethod("toggle").apply { isAccessible = true }.invoke(activity)
        val application = RuntimeEnvironment.getApplication()
        val intent = Shadows.shadowOf(application).nextStartedService
        assertEquals(MarviaVpnService.ACTION_STOP, intent?.action)
    }
}
