package io.marvia.android

import android.graphics.Color
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@org.robolectric.annotation.Config(application = android.app.Application::class, sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ScenesTest {

    @Test fun everySceneLookIsAPictureNotAnEmptyBackground() {
        for (profile in AppearanceProfiles.all.filter { Scenes.isScene(it.pattern) }) {
            val t = Look.theme(Look.normalize(profile.choice))
            val bmp = Scenes.render(profile.pattern, 180, 360, t)
            // Картина — это разные тона по всему кадру, а не заливка. Порог низкий:
        // чёрно-белая «Вершина» честно беднее цветом, чем сакура.
            val seen = HashSet<Int>()
            for (y in 0 until bmp.height step 6) for (x in 0 until bmp.width step 6) {
                val c = bmp.getPixel(x, y)
                seen += (Color.red(c) / 8 shl 10) or (Color.green(c) / 8 shl 5) or (Color.blue(c) / 8)
            }
            assertTrue("${profile.id}: оттенков ${seen.size}", seen.size > 24)
        }
    }

    @Test fun theSamePictureIsDrawnOnEveryLaunch() {
        val t = Look.theme(Look.normalize(AppearanceProfiles.all.first { it.id == "sakura" }.choice))
        val a = Scenes.render(Scenes.BLOSSOM, 120, 240, t)
        val b = Scenes.render(Scenes.BLOSSOM, 120, 240, t)
        assertTrue(a.sameAs(b))
    }

    @Test fun cardsOverASceneAreGlassAndOverAPlainThemeStaySolid() {
        val t = Look.theme(Look.normalize(AppearanceProfiles.all.first { it.id == "summit" }.choice))
        val before = Paint.style
        try {
            Paint.style = Paint.Style(pattern = Scenes.PEAKS)
            val glass = fill(Paint.card(t, 1f))
            assertTrue("альфа стекла ${Color.alpha(glass)}", Color.alpha(glass) in 100..220)
            Paint.style = Paint.Style(pattern = Store.PATTERN_DOTS)
            val solid = fill(Paint.card(t, 1f))
            assertTrue("альфа карточки ${Color.alpha(solid)}", Color.alpha(solid) == 255)
        } finally {
            Paint.style = before
        }
    }

    private fun fill(d: android.graphics.drawable.GradientDrawable): Int = d.colors?.first() ?: d.color?.defaultColor ?: 0
}
