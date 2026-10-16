package io.marvia.android

import android.app.Application
import android.view.View
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(application = Application::class, sdk = [35])
class SnakeViewTest {
    @Test fun theBoardFitsALandscapeScreenInsteadOfRunningOffTheBottom() {
        val context = RuntimeEnvironment.getApplication()
        val view = SnakeView(context, Look.theme(Look.Choice()), Store(context))
        // Повёрнутый телефон: широко, но низко.
        view.measure(
            View.MeasureSpec.makeMeasureSpec(2000, View.MeasureSpec.EXACTLY),
            View.MeasureSpec.makeMeasureSpec(700, View.MeasureSpec.AT_MOST),
        )
        assertTrue("поле ${view.measuredHeight} выше места 700", view.measuredHeight <= 700)
        assertTrue("клетки не квадратные", view.measuredWidth * SnakeView.ROWS == view.measuredHeight * SnakeView.COLS)
    }
}
