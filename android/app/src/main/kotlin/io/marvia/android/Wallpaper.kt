package io.marvia.android

import android.graphics.Bitmap
import android.graphics.BlurMaskFilter
import android.graphics.Canvas
import android.graphics.LinearGradient
import android.graphics.Matrix
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RadialGradient
import android.graphics.RectF
import android.graphics.Shader
import android.util.LruCache
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt
import kotlin.math.sin

/**
 * Wallpaper — обои образов в духе систем, те же, что в окне на ПК (WALLS в
 * app.html): по мотивам Windows, macOS и Linux, но свои — картинки систем
 * принадлежат их авторам. Нарисованы кодом, а не лежат файлами: растягиваются
 * на любой экран без мыла и не весят ничего.
 *
 * Формулы переписаны из SVG один в один: та же сетка 1600×1000, те же
 * градиенты, кривые и случайность с тем же зерном — обои на телефоне те же,
 * что на ПК, у всех и при каждом запуске. Градиенты SVG по умолчанию живут
 * в рамке фигуры (objectBoundingBox), поэтому здесь они строятся в единичном
 * квадрате и растягиваются матрицей на рамку — иначе диагональ шла бы не так.
 *
 * Готовые обои — растр в кэше: размытие облаков и сияния на каждом кадре
 * анимации стоило бы плавности, а на старых телефонах аппаратный холст
 * размытие по маске не рисует вовсе.
 */
object Wallpaper {

    /** Сцена образа: обои и пелена над ними сверху вниз — как veil на ПК. */
    class Scene(val wall: String, val veil: IntArray = intArrayOf(), val stops: FloatArray? = null)

    val names = listOf("bloom", "bloomLight", "waves", "wavesLight", "hill", "aubergine", "blobs", "shards", "peaks", "arctic", "sunset", "teal")

    private const val W = 1600f
    private const val H = 1000f

    /**
     * focus — какая доля ширины обоев стоит посередине экрана. На ПК окно
     * лежит боком, и обои показываются целиком; телефон стоит, и от тех же
     * обоев ему достаётся середина шириной в треть. Цветок Windows 11, зарево
     * Ubuntu, голубой угол Plasma и луна над горами стоят правее середины и
     * ушли бы за край — кадр сдвигается к ним, а сами формулы остаются те же.
     */
    private fun focus(name: String): Float = when (name) {
        "bloom", "bloomLight" -> .66f
        "aubergine" -> .74f
        "blobs" -> .62f
        "shards" -> .72f
        "peaks" -> .64f
        else -> .5f
    }

    /** draw рисует обои на w×h с обрезкой, как preserveAspectRatio="xMidYMid slice", но со своей серединой. */
    fun draw(canvas: Canvas, name: String, w: Float, h: Float) {
        val s = max(w / W, h / H)
        val dx = (w / 2 - focus(name) * W * s).coerceIn(min(0f, w - W * s), 0f)
        val dy = (h - H * s) / 2
        canvas.save()
        canvas.translate(dx, dy)
        canvas.scale(s, s)
        when (name) {
            "bloom" -> bloom(canvas, false)
            "bloomLight" -> bloom(canvas, true)
            "waves" -> waves(canvas, false)
            "wavesLight" -> waves(canvas, true)
            "hill" -> hill(canvas)
            "aubergine" -> aubergine(canvas)
            "blobs" -> blobs(canvas)
            "shards" -> shards(canvas)
            "peaks" -> peaks(canvas)
            "arctic" -> arctic(canvas)
            "sunset" -> sunset(canvas)
            // Стол Windows 98 — просто цвет; картинка не нужна.
            "teal" -> canvas.drawColor(rgb(0x008080))
        }
        canvas.restore()
    }

    // --------------------------------------------------------------- кэш

    /**
     * Готовые растры по имени и размеру. Потолок — восьмая часть памяти
     * приложения: экран, предпросмотр и миниатюры ленты помещаются, а
     * вытесненное просто нарисуется заново.
     */
    private val cache = object : LruCache<String, Bitmap>((Runtime.getRuntime().maxMemory() / 8).coerceAtMost(48L shl 20).toInt()) {
        override fun sizeOf(key: String, value: Bitmap) = value.byteCount
    }

    /** cached отдаёт растр из кэша или рисует его один раз; null — размер нулевой. */
    fun cached(key: String, w: Int, h: Int, paint: (Canvas) -> Unit): Bitmap? {
        if (w <= 0 || h <= 0) return null
        val k = "$key@${w}x$h"
        cache.get(k)?.let { return it }
        val bmp = Bitmap.createBitmap(w, h, Bitmap.Config.ARGB_8888)
        paint(Canvas(bmp))
        cache.put(k, bmp)
        return bmp
    }

    fun bitmap(name: String, w: Int, h: Int): Bitmap? = cached("wall:$name", w, h) { draw(it, name, w.toFloat(), h.toFloat()) }

    // ------------------------------------------------------------- обои

    /** Цветок из лепестков вокруг точки справа внизу — мотив Windows 11. */
    private fun bloom(c: Canvas, light: Boolean) {
        val bg = if (light) intArrayOf(rgb(0xF4F8FD), rgb(0xD5E6F8), rgb(0xB9D3F0)) else intArrayOf(rgb(0x1F5AA8), rgb(0x0B2450), rgb(0x040B1C))
        fill(c, FULL, radial(FULL, .68f, .62f, .85f, bg, if (light) floatArrayOf(0f, .6f, 1f) else floatArrayOf(0f, .45f, 1f)))
        val pet = if (light) intArrayOf(rgb(0xFFFFFF, .95), rgb(0x8CC4F7, .9), rgb(0x1F6FD6, .85)) else intArrayOf(rgb(0xBFE4FF, .95), rgb(0x3A8EF0, .85), rgb(0x0A3C96, .8))
        val at = if (light) floatArrayOf(0f, .45f, 1f) else floatArrayOf(0f, .4f, 1f)
        val p = brush(blur = 5f)
        c.save()
        c.translate(1060f, 600f)
        intArrayOf(-28, 8, 44, 80, 116, 152, 188).forEachIndexed { i, a ->
            val ry = 150f - i * 6
            val box = RectF(-430f, -ry, 430f, ry)
            c.save()
            c.rotate(a.toFloat())
            c.translate(250f, 0f)
            p.shader = linear(box, 0f, 0f, 1f, 1f, pet, at)
            p.alpha = alpha(0.55 + (i % 2) * 0.15)
            c.drawOval(box, p)
            c.restore()
        }
        c.restore()
    }

    /** Слои волн с закатным градиентом — мотив нынешних обоев macOS. */
    private fun waves(c: Canvas, light: Boolean) {
        val sky = if (light) intArrayOf(rgb(0xDFE8FF), rgb(0xF2E6FF), rgb(0xFFE9DF)) else intArrayOf(rgb(0x070B2B), rgb(0x21144F), rgb(0x4A1D6B))
        fill(c, FULL, linear(FULL, 0f, 0f, 0f, 1f, sky, if (light) floatArrayOf(0f, .55f, 1f) else floatArrayOf(0f, .5f, 1f)))
        val bands = if (light) {
            listOf(0xC7D7FF to 0xE9D2FF, 0xB9C8FF to 0xF7C6E6, 0xF7C6E6 to 0xFFD8B8, 0x9FC0FF to 0xC9B6FF, 0xFFE1C7 to 0xFFC1C7)
        } else {
            listOf(0x2B2A9E to 0x5B33C4, 0x7A36C9 to 0xD1468F, 0xE0567A to 0xFF9A5C, 0x2563EB to 0x53B8FF, 0x141447 to 0x2A1D6E)
        }
        bands.forEachIndexed { i, (from, to) ->
            val y = 360f + i * 120
            val a = 140f - i * 12
            val pen = Pen().m(-50f, y).c(380f, y - a, 760f, y + a, 1120f, y - a * 0.4f).s(1500f, y + a * 0.6f, 1660f, y - 20).l(1660f, 1100f).l(-50f, 1100f).z()
            val p = brush()
            p.shader = linear(pen.box, 0f, 0f, 1f, 0f, intArrayOf(rgb(from), rgb(to)), null)
            p.alpha = alpha(0.92 - i * 0.04)
            c.drawPath(pen.path, p)
        }
    }

    /** Небо, облака и зелёный холм — мотив стола XP; не фотография. */
    private fun hill(c: Canvas) {
        val r = Rnd(7)
        fill(c, FULL, linear(FULL, 0f, 0f, 0f, 1f, intArrayOf(rgb(0x2C66D6), rgb(0x77B1EE), rgb(0xC5E3FB)), floatArrayOf(0f, .55f, 1f)))
        val cloud = brush(blur = 18f)
        repeat(7) {
            val x = r() * 1600
            val y = 60 + r() * 260
            val w = 120 + r() * 220
            cloud.color = rgb(0xFFFFFF, 0.5 + r() * 0.4)
            c.drawOval((x - w).toFloat(), (y - w * 0.28).toFloat(), (x + w).toFloat(), (y + w * 0.28).toFloat(), cloud)
        }
        val back = Pen().m(-40f, 700f).q(420f, 560f, 900f, 650f).t(1660f, 640f).l(1660f, 1040f).l(-40f, 1040f).z()
        fill(c, back, linear(back.box, 0f, 0f, 0f, 1f, intArrayOf(rgb(0x5FB83A), rgb(0x2F7A1D)), null))
        val front = Pen().m(-40f, 760f).q(520f, 560f, 1050f, 690f).t(1660f, 720f).l(1660f, 1040f).l(-40f, 1040f).z()
        fill(c, front, linear(front.box, 0f, 0f, 0f, 1f, intArrayOf(rgb(0x86D84A), rgb(0x4FAE2C), rgb(0x2D7D1B)), floatArrayOf(0f, .5f, 1f)))
    }

    /** Баклажан с оранжевым заревом и кольцами света — мотив Ubuntu. */
    private fun aubergine(c: Canvas) {
        fill(c, FULL, linear(FULL, 0f, 0f, 1f, 1f, intArrayOf(rgb(0x2C001E), rgb(0x5E2750), rgb(0x77216F)), floatArrayOf(0f, .55f, 1f)))
        fill(c, FULL, radial(FULL, .1f, .05f, .6f, intArrayOf(rgb(0xA0469D, .55), rgb(0xA0469D, 0.0)), null))
        fill(c, FULL, radial(FULL, .8f, .85f, .55f, intArrayOf(rgb(0xE95420, .85), rgb(0xE95420, .25), rgb(0xE95420, 0.0)), floatArrayOf(0f, .5f, 1f)))
        val ring = brush().apply { style = Paint.Style.STROKE; strokeWidth = 2f }
        for (i in 1..7) {
            ring.color = rgb(0xFFFFFF, 0.07 - i * 0.007)
            c.drawCircle(1260f, 820f, i * 120f, ring)
        }
    }

    /** Плоские синие фигуры на светлом — мотив обоев GNOME. */
    private fun blobs(c: Canvas) {
        fill(c, FULL, linear(FULL, 0f, 0f, 1f, 1f, intArrayOf(rgb(0xE8F0FB), rgb(0xC6DAF5)), null))
        val p = brush()
        p.color = rgb(0x3584E4); c.drawCircle(1230f, 260f, 330f, p)
        c.save(); c.rotate(-18f, 390f, 840f)
        p.color = rgb(0x99C1F1); c.drawRoundRect(110f, 560f, 670f, 1120f, 150f, 150f, p)
        c.restore()
        p.color = rgb(0x1C71D8); c.drawCircle(760f, 700f, 200f, p)
        p.color = rgb(0x62A0EA); c.drawCircle(1420f, 760f, 120f, p)
        c.save(); c.rotate(14f, 570f, 240f)
        p.color = rgb(0xC0D8F6); c.drawRoundRect(420f, 90f, 720f, 390f, 90f, 90f, p)
        c.restore()
    }

    /** Острые треугольники от тёмного к голубому — мотив Plasma. */
    private fun shards(c: Canvas) {
        val r = Rnd(11)
        val cols = 9
        val rows = 6
        val px = DoubleArray((rows + 1) * (cols + 1))
        val py = DoubleArray(px.size)
        for (j in 0..rows) for (i in 0..cols) {
            val edge = i == 0 || j == 0 || i == cols || j == rows
            val k = j * (cols + 1) + i
            px[k] = i * 1600.0 / cols + if (edge) 0.0 else (r() - 0.5) * 140
            py[k] = j * 1000.0 / rows + if (edge) 0.0 else (r() - 0.5) * 120
        }
        val p = brush().apply { style = Paint.Style.FILL_AND_STROKE; strokeWidth = 1.5f }
        for (j in 0 until rows) for (i in 0 until cols) {
            fun at(a: Int, b: Int) = b * (cols + 1) + a
            val tris = arrayOf(intArrayOf(at(i, j), at(i + 1, j), at(i, j + 1)), intArrayOf(at(i + 1, j), at(i + 1, j + 1), at(i, j + 1)))
            for (t in tris) {
                val cx = (px[t[0]] + px[t[1]] + px[t[2]]) / 3
                val cy = (py[t[0]] + py[t[1]] + py[t[2]]) / 3
                val k = ((cx / 1600) * 0.6 + (cy / 1000) * 0.5 - 0.15 + (r() - 0.5) * 0.18).coerceIn(0.0, 1.0)
                p.color = if (k < 0.6) mixHex(0x0B141D, 0x123A5C, k / 0.6) else mixHex(0x123A5C, 0x1D99F3, (k - 0.6) / 0.4)
                // В SVG вершины округлены до целых (toFixed(0)) — и здесь.
                val path = Path()
                t.forEachIndexed { n, q ->
                    val x = round(px[q], 0)
                    val y = round(py[q], 0)
                    if (n == 0) path.moveTo(x, y) else path.lineTo(x, y)
                }
                path.close()
                c.drawPath(path, p)
            }
        }
    }

    /** Горы под ночным небом — любимые обои плиточных столов. */
    private fun mountains(c: Canvas, sky: IntArray, at: FloatArray, layers: List<Triple<Int, Int, Int>>, extra: (Canvas) -> Unit) {
        val r = Rnd(5)
        fill(c, FULL, linear(FULL, 0f, 0f, 0f, 1f, sky, at))
        val star = brush()
        repeat(110) {
            val x = round(r() * 1600, 0)
            val y = round(r() * 520, 0)
            val radius = round(0.6 + r() * 1.6, 1)
            star.color = rgb(0xFFFFFF, round(0.25 + r() * 0.6, 2).toDouble())
            c.drawCircle(x, y, radius, star)
        }
        extra(c)
        layers.forEachIndexed { li, (color, base, amp) ->
            val path = Path()
            path.moveTo(-20f, 1020f)
            path.lineTo(-20f, base.toFloat())
            var x = 0
            while (x <= 1640) {
                path.lineTo(x.toFloat(), round(base - r() * amp - (if (li == 0) sin(x / 260.0) * amp * 0.5 else 0.0), 0))
                x += 80
            }
            path.lineTo(1660f, 1020f)
            path.close()
            c.drawPath(path, brush().apply { this.color = color })
        }
    }

    private fun peaks(c: Canvas) {
        // Закат за горами в цветах Catppuccin: обои плиточного стола должны
        // быть видны сквозь полупрозрачные плитки, тёмные там просто пропадали.
        mountains(c,
            intArrayOf(rgb(0x1E1E2E), rgb(0x45375C), rgb(0xB48AD4), rgb(0xF5C2E7), rgb(0xFAB387)), floatArrayOf(0f, .24f, .46f, .6f, .76f),
            listOf(Triple(rgb(0x6C5F8D), 600, 230), Triple(rgb(0x45475A), 730, 170), Triple(rgb(0x1E1E2E), 870, 120)),
        ) { it.drawCircle(1120f, 520f, 90f, brush().apply { color = rgb(0xF9E2AF, .9) }) }
    }

    private fun arctic(c: Canvas) {
        mountains(c,
            intArrayOf(rgb(0x232831), rgb(0x2E3440), rgb(0x4C566A)), floatArrayOf(0f, .6f, 1f),
            listOf(Triple(rgb(0xD8DEE9), 660, 230), Triple(rgb(0x4C566A), 760, 150), Triple(rgb(0x3B4252), 880, 90)),
        ) { k ->
            // Сияние: размытые полосы, прозрачные все вместе, а не каждая
            // сама по себе, — как opacity у группы в SVG.
            k.saveLayerAlpha(0f, 0f, W, H, alpha(.55))
            val p = brush(blur = 28f).apply { style = Paint.Style.STROKE }
            fun band(pen: Pen, color: Int, width: Float) { p.color = rgb(color); p.strokeWidth = width; k.drawPath(pen.path, p) }
            band(Pen().m(0f, 260f).c(300f, 160f, 600f, 360f, 900f, 220f).s(1400f, 300f, 1600f, 180f), 0x8FBCBB, 70f)
            band(Pen().m(0f, 340f).c(400f, 260f, 700f, 420f, 1000f, 300f).s(1450f, 380f, 1600f, 280f), 0xA3BE8C, 40f)
            band(Pen().m(200f, 200f).c(500f, 120f, 900f, 260f, 1300f, 140f), 0xB48EAD, 30f)
            k.restore()
        }
    }

    /** Солнце с прорезями над сеткой — тёплый ретро в цветах Gruvbox. */
    private fun sunset(c: Canvas) {
        fill(c, FULL, linear(FULL, 0f, 0f, 0f, 1f, intArrayOf(rgb(0x1D2021), rgb(0x32302F), rgb(0x282828)), floatArrayOf(0f, .7f, 1f)))
        val sun = RectF(530f, 330f, 1070f, 870f)
        c.drawCircle(800f, 600f, 270f, brush().apply {
            shader = linear(sun, 0f, 0f, 0f, 1f, intArrayOf(rgb(0xFABD2F), rgb(0xFE8019), rgb(0xFB4934)), floatArrayOf(0f, .55f, 1f))
        })
        val p = brush().apply { color = rgb(0x282828) }
        for (i in 0 until 6) c.drawRect(500f, 600f + i * 34, 1100f, 600f + i * 34 + 6 + i * 3, p)
        p.color = rgb(0x1D2021)
        c.drawRect(0f, 740f, 1600f, 1000f, p)
        p.color = rgb(0x504945)
        p.strokeWidth = 2f
        for (i in 0..16) c.drawLine(800f + (i - 8) * 60, 740f, 800f + (i - 8) * 260, 1000f, p)
        for (i in 0 until 6) {
            val y = 750f + i * i * 9
            c.drawLine(0f, y, 1600f, y, p)
        }
    }

    // ------------------------------------------------------- инструменты

    private val FULL = RectF(0f, 0f, W, H)

    /**
     * Rnd — тот же генератор, что в app.html: Парк — Миллер с зерном. Свой,
     * а не java.util.Random: обои обязаны совпасть с ПК до звезды.
     */
    private class Rnd(private var seed: Long) {
        operator fun invoke(): Double {
            seed = seed * 16807 % 2147483647
            return (seed - 1) / 2147483646.0
        }
    }

    /**
     * Pen — путь из команд SVG (M, L, C, S, Q, T, Z) с рамкой самой кривой,
     * а не её опорных точек: по этой рамке SVG растягивает градиент фигуры.
     * computeBounds у Android даёт рамку опорных точек — она шире, и
     * градиент холма лёг бы не туда.
     */
    private class Pen {
        val path = Path()
        val box = RectF(Float.MAX_VALUE, Float.MAX_VALUE, -Float.MAX_VALUE, -Float.MAX_VALUE)
        private var x = 0f
        private var y = 0f
        private var kx = 0f
        private var ky = 0f

        private fun add(px: Float, py: Float) {
            box.left = min(box.left, px); box.right = max(box.right, px)
            box.top = min(box.top, py); box.bottom = max(box.bottom, py)
        }

        fun m(px: Float, py: Float) = apply { path.moveTo(px, py); x = px; y = py; kx = px; ky = py; add(px, py) }
        fun l(px: Float, py: Float) = apply { path.lineTo(px, py); x = px; y = py; kx = px; ky = py; add(px, py) }
        fun z() = apply { path.close() }

        fun c(x1: Float, y1: Float, x2: Float, y2: Float, px: Float, py: Float) = apply {
            path.cubicTo(x1, y1, x2, y2, px, py)
            for (n in 1..STEPS) {
                val t = n / STEPS.toFloat()
                val u = 1 - t
                add(u * u * u * x + 3 * u * u * t * x1 + 3 * u * t * t * x2 + t * t * t * px,
                    u * u * u * y + 3 * u * u * t * y1 + 3 * u * t * t * y2 + t * t * t * py)
            }
            x = px; y = py; kx = x2; ky = y2
        }

        /** S — первая опора отражена от второй опоры прошлой кривой. */
        fun s(x2: Float, y2: Float, px: Float, py: Float) = c(2 * x - kx, 2 * y - ky, x2, y2, px, py)

        fun q(x1: Float, y1: Float, px: Float, py: Float) = apply {
            path.quadTo(x1, y1, px, py)
            for (n in 1..STEPS) {
                val t = n / STEPS.toFloat()
                val u = 1 - t
                add(u * u * x + 2 * u * t * x1 + t * t * px, u * u * y + 2 * u * t * y1 + t * t * py)
            }
            x = px; y = py; kx = x1; ky = y1
        }

        /** T — опора отражена от опоры прошлой кривой. */
        fun t(px: Float, py: Float) = q(2 * x - kx, 2 * y - ky, px, py)

        private companion object { const val STEPS = 64 }
    }

    /** linear — градиент в рамке фигуры: x1…y2 в долях рамки, как в objectBoundingBox. */
    private fun linear(box: RectF, x1: Float, y1: Float, x2: Float, y2: Float, colors: IntArray, stops: FloatArray?): Shader =
        LinearGradient(x1, y1, x2, y2, colors, stops, Shader.TileMode.CLAMP).apply { setLocalMatrix(frame(box)) }

    /** radial — круг в долях рамки; на неквадратной рамке он становится эллипсом, как в SVG. */
    private fun radial(box: RectF, cx: Float, cy: Float, r: Float, colors: IntArray, stops: FloatArray?): Shader =
        RadialGradient(cx, cy, r, colors, stops, Shader.TileMode.CLAMP).apply { setLocalMatrix(frame(box)) }

    private fun frame(box: RectF) = Matrix().apply {
        setScale(box.width(), box.height())
        postTranslate(box.left, box.top)
    }

    private fun fill(c: Canvas, box: RectF, shader: Shader) = c.drawRect(box, brush().apply { this.shader = shader })
    private fun fill(c: Canvas, pen: Pen, shader: Shader) = c.drawPath(pen.path, brush().apply { this.shader = shader })

    /**
     * brush — кисть; blur — stdDeviation размытия из SVG в единицах обоев.
     * BlurMaskFilter принимает радиус, а Skia переводит его в сигму как
     * 0,57735·r + 0,5 — обратный пересчёт здесь. Размытие масштабируется
     * вместе с холстом, поэтому и на миниатюре, и на экране оно то же.
     */
    private fun brush(blur: Float = 0f) = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = 0xFFFFFFFF.toInt()
        if (blur > 0f) maskFilter = BlurMaskFilter((blur - 0.5f) / 0.57735f, BlurMaskFilter.Blur.NORMAL)
    }

    private fun alpha(a: Double) = (a * 255).roundToInt().coerceIn(0, 255)

    private fun rgb(hex: Int, a: Double = 1.0): Int = (alpha(a) shl 24) or (hex and 0xFFFFFF)

    /** mixHex — как mix в shards(): каждый канал округлён по Math.round. */
    private fun mixHex(a: Int, b: Int, t: Double): Int {
        fun ch(shift: Int) = floor(((a shr shift) and 0xFF) * (1 - t) + ((b shr shift) and 0xFF) * t + 0.5).toInt()
        return (0xFF shl 24) or (ch(16) shl 16) or (ch(8) shl 8) or ch(0)
    }

    /** round — как toFixed(n) в JavaScript: числа в разметке SVG округлены, и звёзды стоят ровно там. */
    private fun round(v: Double, digits: Int): Float {
        var k = 1.0
        repeat(digits) { k *= 10 }
        return (floor(v * k + 0.5) / k).toFloat()
    }
}
