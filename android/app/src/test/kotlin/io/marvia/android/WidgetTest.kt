package io.marvia.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Виджет обещает две вещи: он есть в списке виджетов телефона и
 * показывает, что с туннелем, — но не будит рабочий стол на каждый замер
 * скорости.
 */
class WidgetTest {
    @Test
    fun widgetIsDeclaredForTheLauncher() {
        val manifest = read("src/main/AndroidManifest.xml")
        assertTrue("виджета нет в манифесте", manifest.contains("android:name=\".MarviaWidget\""))
        assertTrue("у виджета нет описания для рабочего стола", manifest.contains("@xml/widget_marvia_info"))
        assertTrue("файла описания виджета нет", read("src/main/res/xml/widget_marvia_info.xml").contains("widget_marvia"))
    }

    @Test
    fun widgetRepaintsOnlyWhenWhatItShowsChanges() {
        val fast = TunnelState.On(node = "Финляндия · Хельсинки", ms = 30)
        val slow = fast.copy(ms = 80, warning = "одна цель не ответила")
        assertEquals("смена отклика перерисовала виджет", MarviaWidget.key(fast), MarviaWidget.key(slow))
        assertNotEquals("переезд на другую ноду не дошёл до виджета", MarviaWidget.key(fast), MarviaWidget.key(fast.copy(node = "ОАЭ · Дубай")))
        assertNotEquals(MarviaWidget.key(TunnelState.Off), MarviaWidget.key(TunnelState.Connecting))
    }

    private fun read(path: String): String {
        var dir: File? = File(System.getProperty("user.dir").orEmpty()).absoluteFile
        while (dir != null) {
            val file = File(dir, path)
            if (file.isFile) return file.readText()
            dir = dir.parentFile
        }
        throw AssertionError("не нашёл $path от " + System.getProperty("user.dir"))
    }
}
