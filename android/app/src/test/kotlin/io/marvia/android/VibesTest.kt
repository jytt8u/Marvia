package io.marvia.android

import android.app.Application
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/**
 * Телефон показывает те же образы, что окно на ПК: те же имена по порядку,
 * та же палитра, те же обои и тот же переезд со старых имён. Пелену не
 * сверяем: на ПК это заливка окна, стоящего на столе, а на телефоне стола
 * вокруг нет, и пелена своя — лёгкая, чтобы обои были видны. Сверяем с
 * таблицей VIBES в app.html, а не с копией в тесте: разойдутся две копии —
 * тест этого не заметит, а человек, сменивший ПК на телефон, заметит.
 */
@RunWith(RobolectricTestRunner::class)
@Config(application = Application::class, sdk = [35])
class VibesTest {
    private val page: String by lazy { read("cmd/marvia-windows/ui/app.html").replace("\r\n", "\n") }

    private val vibes: String by lazy {
        val start = page.indexOf("const VIBES = {")
        assertTrue("в окне на ПК нет таблицы образов", start >= 0)
        page.substring(start, page.indexOf("\n};", start))
    }

    /** Тело образа id в таблице ПК: от «  id: {» до следующего образа. */
    private fun entry(id: String): String {
        val start = vibes.indexOf("\n  $id: {\n")
        assertTrue("образа $id нет на ПК", start >= 0)
        val next = Regex("""\n  [a-z0-9]+: \{\n""").find(vibes, start + 1)?.range?.first ?: vibes.length
        return vibes.substring(start, next)
    }

    private fun field(body: String, name: String): String? =
        Regex("""\b$name: "?(#?[a-zA-Z0-9]+)"?""").find(body)?.groupValues?.get(1)

    private fun color(hex: String) = (0xFF000000 or hex.removePrefix("#").toLong(16)).toInt()

    @Test fun thePhoneHasTheSameVibesInTheSameOrderAsThePc() {
        val ids = Regex("""(?m)^  ([a-z0-9]+): \{$""").findAll(vibes).map { it.groupValues[1] }.toList()
        assertEquals(ids, AppearanceProfiles.all.map { it.id })
    }

    @Test fun everyVibeInTheSpiritOfASystemHasThePcPaletteAndWallpaper() {
        for (profile in AppearanceProfiles.all) {
            val body = entry(profile.id)
            val wall = field(body, "wall")
            assertEquals("${profile.id}: обои", wall, profile.scene?.wall)
            val p = profile.palette
            if (field(body, "bg") == null) {
                assertEquals("${profile.id}: палитра своя только у образов систем", null, p)
                continue
            }
            assertTrue("${profile.id}: нет палитры", p != null)
            p!!
            assertEquals("${profile.id}: фон", color(field(body, "bg")!!), p.bg)
            assertEquals("${profile.id}: карточки", color(field(body, "surf")!!), p.surf)
            assertEquals("${profile.id}: текст", color(field(body, "fg")!!), p.fg)
            assertEquals("${profile.id}: подписи", color(field(body, "dim")!!), p.dim)
            assertEquals("${profile.id}: обводка", color(field(body, "line")!!), p.line)
            assertEquals("${profile.id}: скругление", field(body, "r")!!.toInt(), p.r)
            val look = body.substringAfter("look: {").substringBefore("}")
            val c = profile.choice
            assertEquals("${profile.id}: палитра темы", field(look, "preset"), c.preset)
            assertEquals("${profile.id}: акцент", color(field(look, "acc")!!), c.accent)
            for ((key, value) in listOf("kind" to c.kind, "radius" to c.radius, "density" to c.density, "btn" to c.btn, "glow" to c.glow, "card" to c.card)) {
                assertEquals("${profile.id}: $key", field(look, key), value)
            }
            Regex("""tile: \["(#[0-9a-f]{6})", "(#[0-9a-f]{6})"\]""").find(body)?.let {
                assertEquals("${profile.id}: рамка плитки", listOf(color(it.groupValues[1]), color(it.groupValues[2])), profile.tile?.toList())
            }
        }
    }

    @Test fun oldVibeNamesMoveWhereThePcMovesThem() {
        val line = page.substringAfter("const VIBE_RENAMED = {").substringBefore("};")
        val pc = Regex("""([a-z0-9]+): "([a-z0-9]+)"""").findAll(line).associate { it.groupValues[1] to it.groupValues[2] }
        assertEquals(pc, AppearanceProfiles.renamed)
        for (id in pc.values) assertTrue("переезд ведёт в несуществующий образ $id", AppearanceProfiles.byId(id) != null)
    }

    @Test fun everyVibeIsNamedAsOnThePc() {
        val dicts = Regex("""(?s)    vibes: \{\n(.*?)\n    \},""").findAll(page).map { it.groupValues[1] }.toList()
        assertEquals("подписи образов на ПК — в двух словарях", 2, dicts.size)
        // Первый словарь — русский, второй — английский.
        for ((dict, values) in listOf(dicts[0] to "src/main/res/values-ru/appearance.xml", dicts[1] to "src/main/res/values/appearance.xml")) {
            val strings = read(values)
            for (profile in AppearanceProfiles.all) {
                val pc = Regex("""      ${profile.id}: \["([^"]+)"""").find(dict)?.groupValues?.get(1)
                assertTrue("${profile.id} не подписан на ПК", pc != null)
                assertTrue("${profile.id}: на телефоне не «$pc»", strings.contains("<string name=\"vibe_${profile.id}\">$pc</string>"))
            }
        }
    }

    @Test @GraphicsMode(GraphicsMode.Mode.NATIVE)
    fun everyWallpaperIsDrawnTheSameEveryTime() {
        for (name in Wallpaper.names) {
            val a = android.graphics.Bitmap.createBitmap(160, 100, android.graphics.Bitmap.Config.ARGB_8888)
            val b = android.graphics.Bitmap.createBitmap(160, 100, android.graphics.Bitmap.Config.ARGB_8888)
            Wallpaper.draw(android.graphics.Canvas(a), name, 160f, 100f)
            Wallpaper.draw(android.graphics.Canvas(b), name, 160f, 100f)
            assertTrue("$name рисуется по-разному", a.sameAs(b))
            val colours = HashSet<Int>()
            for (y in 0 until 100 step 7) for (x in 0 until 160 step 7) colours.add(a.getPixel(x, y))
            // Стол 98-й — просто цвет, остальные — картинка.
            if (name == "teal") assertEquals(setOf(0xFF008080.toInt()), colours) else assertTrue("$name пустые", colours.size > 20)
        }
    }

    /** Тест запускается из каталога модуля, но лучше не верить в это молча. */
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
