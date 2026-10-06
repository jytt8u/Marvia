package io.marvia.android

import android.app.Application
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(application = Application::class, sdk = [35])
class SessionFinishTest {
    @After fun clearCurrentSession() { SessionDetails.finish() }

    @Test fun savingFinishedSessionCannotEraseTheNextConnection() {
        val context = RuntimeEnvironment.getApplication()
        context.deleteDatabase("session_history.db")
        SessionDetails.begin(1000)
        SessionDetails.sample(1200, 300, 2000)
        val finished = SessionDetails.finish()!!
        assertNull(SessionDetails.snapshot().session)
        SessionDetails.begin(3000)
        SessionDetails.sample(250, 100, 4000)
        SessionDetails.save(context, finished)
        assertNotNull(SessionDetails.snapshot().session)
        assertEquals(250L, SessionDetails.snapshot().session!!.received)
        assertEquals(1200L, SessionDetails.history(context, 10).single().received)
    }
}
