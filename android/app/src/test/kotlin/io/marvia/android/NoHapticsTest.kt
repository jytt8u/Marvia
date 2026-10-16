package io.marvia.android

import android.app.Application
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import com.google.android.material.materialswitch.MaterialSwitch
import org.junit.Assert.assertFalse
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(application = Application::class, sdk = [35])
class NoHapticsTest {
    @Test fun noViewInTheAppVibratesNotEvenTheSystemOnes() {
        val controller = Robolectric.buildActivity(AppCompatActivity::class.java)
        val activity = controller.get()
        activity.setTheme(R.style.Theme_Veil)
        controller.setup()
        try {
            // Свои строки, системный переключатель и строка из разметки — все без
            // вибрации, хотя их никто не перекрашивал.
            val row = activity.layoutInflater.inflate(R.layout.item_app, null)
            for (view in listOf(TextView(activity), MaterialSwitch(activity), row)) {
                assertFalse("${view.javaClass.simpleName} вибрирует", view.isHapticFeedbackEnabled)
            }
        } finally {
            controller.pause().stop().destroy()
        }
    }
}
