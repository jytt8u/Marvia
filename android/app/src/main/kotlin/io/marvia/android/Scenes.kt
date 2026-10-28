package io.marvia.android

import android.graphics.Bitmap
import android.graphics.BlurMaskFilter
import android.graphics.Canvas
import android.graphics.LinearGradient
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RadialGradient
import android.graphics.Shader
import androidx.core.graphics.ColorUtils
import kotlin.math.cos
import kotlin.math.max
import kotlin.math.min
import kotlin.math.pow
import kotlin.math.sin

/**
 * Scenes — картины вместо узора: горы, сакура, огни, город, туман, осень,
 * лилии, пудра.
 *
 * Образцы, по которым люди выбирают оформление телефона, держатся на снимке во
 * весь экран и матовых плитках поверх него. Чужие снимки в приложение класть
 * нельзя — они чужие, — а свой фон из галереи и так есть. Поэтому картины
 * рисуются здесь, кистью по холсту: силуэт хребта, ветка с цветами, огни.
 * Случайность у каждой сцены своя и постоянная — одно и то же зерно, — и
 * картина не меняется от запуска к запуску.
 *
 * Рисуется один раз на размер в битмап (см. Backdrop): размытие и сотни
 * кругов на каждый кадр анимации телефону не по силам.
 */
object Scenes {
    const val PEAKS = "peaks"
    const val BLOSSOM = "blossom"
    const val BOKEH = "bokeh"
    const val SKYLINE = "skyline"
    const val HAZE = "haze"
    const val AUTUMN = "autumn"
    const val LILIES = "lilies"
    const val POWDER = "powder"

    val keys = listOf(PEAKS, BLOSSOM, BOKEH, SKYLINE, HAZE, AUTUMN, LILIES, POWDER)

    fun isScene(pattern: String) = pattern in keys

    fun render(key: String, w: Int, h: Int, t: Theme): Bitmap {
        val bmp = Bitmap.createBitmap(max(w, 1), max(h, 1), Bitmap.Config.ARGB_8888)
        val s = Stage(Canvas(bmp), w.toFloat(), h.toFloat(), t, java.util.Random(key.hashCode().toLong()))
        when (key) {
            PEAKS -> s.peaks()
            BLOSSOM -> s.blossom()
            BOKEH -> s.bokeh()
            SKYLINE -> s.skyline()
            HAZE -> s.haze()
            AUTUMN -> s.autumn()
            LILIES -> s.lilies()
            POWDER -> s.powder()
        }
        return bmp
    }

    /** Stage — холст сцены и общие приёмы: небо, хребет, дерево, пятно света. */
    private class Stage(val c: Canvas, val w: Float, val h: Float, val t: Theme, val rnd: java.util.Random) {
        /** u — сотая короткой стороны: размеры не зависят от экрана и миниатюры. */
        val u = min(w, h) / 100f
        val p = Paint(Paint.ANTI_ALIAS_FLAG)

        fun r(a: Float, b: Float) = a + rnd.nextFloat() * (b - a)
        fun tone(k: Double) = Look.mix(t.bg, t.fg, k)
        fun alpha(color: Int, a: Int) = ColorUtils.setAlphaComponent(color, a.coerceIn(0, 255))

        fun sky(top: Int, bottom: Int, mid: Int = Look.mix(top, bottom, 0.5), at: Float = 0.5f) {
            p.reset(); p.isAntiAlias = true
            p.shader = LinearGradient(0f, 0f, 0f, h, intArrayOf(top, mid, bottom), floatArrayOf(0f, at, 1f), Shader.TileMode.CLAMP)
            c.drawRect(0f, 0f, w, h, p)
            p.shader = null
        }

        fun glow(x: Float, y: Float, radius: Float, color: Int) {
            p.reset(); p.isAntiAlias = true
            p.shader = RadialGradient(x, y, max(radius, 1f), color, alpha(color, 0), Shader.TileMode.CLAMP)
            c.drawCircle(x, y, radius, p)
            p.shader = null
        }

        /** fade — низ экрана уходит в цвет фона: там кнопки и карточки, картине там тише. */
        fun fade(from: Float, strength: Int = 235) {
            p.reset()
            p.shader = LinearGradient(0f, h * from, 0f, h, alpha(t.bg, 0), alpha(t.bg, strength), Shader.TileMode.CLAMP)
            c.drawRect(0f, h * from, w, h, p)
            p.shader = null
        }

        /** ridge — ломаная хребта: середина отрезка сдвигается всё меньше с каждым делением. */
        fun ridge(y0: Float, y1: Float, rough: Float, depth: Int = 7): List<Pair<Float, Float>> {
            var pts = listOf(0f to y0, w to y1)
            var amp = rough
            repeat(depth) {
                val next = ArrayList<Pair<Float, Float>>(pts.size * 2)
                for (i in 0 until pts.size - 1) {
                    val (ax, ay) = pts[i]
                    val (bx, by) = pts[i + 1]
                    next += ax to ay
                    next += (ax + bx) / 2 to (ay + by) / 2 + r(-amp, amp)
                }
                next += pts.last()
                pts = next
                amp *= 0.55f
            }
            return pts
        }

        fun fillBelow(pts: List<Pair<Float, Float>>, paint: Paint) {
            val path = Path()
            path.moveTo(0f, h)
            pts.forEach { (x, y) -> path.lineTo(x, y) }
            path.lineTo(w, h)
            path.close()
            c.drawPath(path, paint)
        }

        /** tree — ветвление вверх: каждая ветка короче и тоньше родительской. */
        fun tree(x: Float, y: Float, angle: Float, len: Float, width: Float, depth: Int, color: Int, spread: Float = 34f, tips: ((Float, Float) -> Unit)? = null) {
            if (depth == 0 || len < u * 0.6f) {
                tips?.invoke(x, y)
                return
            }
            val a = Math.toRadians(angle.toDouble())
            val bend = r(-0.18f, 0.18f) * len
            val x2 = x + cos(a).toFloat() * len
            val y2 = y + sin(a).toFloat() * len
            p.reset(); p.isAntiAlias = true
            p.style = Paint.Style.STROKE
            p.strokeCap = Paint.Cap.ROUND
            p.strokeWidth = max(width, u * 0.12f)
            p.color = color
            val path = Path()
            path.moveTo(x, y)
            path.quadTo((x + x2) / 2 + bend, (y + y2) / 2, x2, y2)
            c.drawPath(path, p)
            val branches = if (depth > 6) 2 else if (rnd.nextFloat() < 0.35f) 3 else 2
            repeat(branches) {
                tree(x2, y2, angle + r(-spread, spread), len * r(0.62f, 0.8f), width * 0.68f, depth - 1, color, spread, tips)
            }
        }

        /** petal — лепесток: заострённый на конце, от основания к кончику. */
        fun petal(cx: Float, cy: Float, angle: Float, len: Float, width: Float, base: Int, tipColor: Int, blur: Float = 0f) {
            c.save()
            c.rotate(angle, cx, cy)
            val path = Path()
            path.moveTo(cx, cy)
            path.cubicTo(cx + len * 0.25f, cy - width, cx + len * 0.75f, cy - width * 0.8f, cx + len, cy)
            path.cubicTo(cx + len * 0.75f, cy + width * 0.8f, cx + len * 0.25f, cy + width, cx, cy)
            p.reset(); p.isAntiAlias = true
            p.shader = LinearGradient(cx, cy, cx + len, cy, base, tipColor, Shader.TileMode.CLAMP)
            if (blur > 0f) p.maskFilter = BlurMaskFilter(blur, BlurMaskFilter.Blur.NORMAL)
            c.drawPath(path, p)
            // Жилка по середине — без неё лепесток читается как лист.
            if (blur == 0f) {
                p.shader = null
                p.style = Paint.Style.STROKE
                p.strokeWidth = u * 0.15f
                p.color = alpha(Look.mix(base, t.bg, 0.3), 120)
                c.drawLine(cx + len * 0.08f, cy, cx + len * 0.85f, cy, p)
            }
            c.restore()
            p.maskFilter = null
            p.shader = null
        }

        fun soft(x: Float, y: Float, radius: Float, color: Int, blur: Float) {
            p.reset(); p.isAntiAlias = true
            p.color = color
            if (blur > 0f) p.maskFilter = BlurMaskFilter(blur, BlurMaskFilter.Blur.NORMAL)
            c.drawCircle(x, y, radius, p)
            p.maskFilter = null
        }

        // ------------------------------------------------------------ сцены

        /** Вершина: чёрно-белый пик со снегом, ниже — тёмные отроги. */
        fun peaks() {
            sky(tone(0.16), t.bg, tone(0.07), 0.55f)
            glow(w * 0.72f, h * 0.16f, w * 0.45f, alpha(t.fg, 22))

            // Дальний хребет — светлее и в дымке.
            p.reset(); p.isAntiAlias = true
            p.color = tone(0.14)
            fillBelow(ridge(h * 0.5f, h * 0.46f, h * 0.06f), p)

            // Главный пик: склоны к краям, вершина чуть правее середины.
            val peakX = w * 0.54f
            val peakY = h * 0.27f
            val left = ridge(h * 0.62f, peakY, h * 0.05f, 6).map { (x, y) -> x * peakX / w to y }
            val right = ridge(peakY, h * 0.6f, h * 0.05f, 6).map { (x, y) -> peakX + x * (w - peakX) / w to y }
            val mountain = Path()
            mountain.moveTo(0f, h)
            (left + right).forEach { (x, y) -> mountain.lineTo(x, y) }
            mountain.lineTo(w, h)
            mountain.close()
            p.reset(); p.isAntiAlias = true
            p.shader = LinearGradient(0f, peakY, 0f, h * 0.68f, tone(0.62), tone(0.08), Shader.TileMode.CLAMP)
            c.drawPath(mountain, p)
            p.shader = null

            // Снежная шапка: от вершины до неровной кромки, внизу тает.
            c.save()
            c.clipPath(mountain)
            val snowLine = peakY + h * 0.15f
            val snow = Path()
            snow.moveTo(0f, 0f)
            snow.lineTo(w, 0f)
            var sx = w
            while (sx > 0f) { sx -= r(1.5f, 4f) * u; snow.lineTo(sx, snowLine + r(-h * 0.03f, h * 0.05f) + kotlin.math.abs(sx - peakX) * 0.12f) }
            snow.close()
            p.reset(); p.isAntiAlias = true
            p.shader = LinearGradient(0f, peakY, 0f, snowLine + h * 0.05f, alpha(t.fg, 235), alpha(t.fg, 40), Shader.TileMode.CLAMP)
            c.drawPath(snow, p)
            p.shader = null
            val shadow = Path()
            shadow.moveTo(peakX, peakY)
            var x = peakX
            var y = peakY
            while (y < h) { x += r(-0.4f, 1.6f) * u; y += r(1.2f, 2.4f) * u; shadow.lineTo(x, y) }
            shadow.lineTo(w, h); shadow.lineTo(w, 0f); shadow.close()
            p.reset(); p.isAntiAlias = true
            p.color = alpha(t.bg, 120)
            c.drawPath(shadow, p)
            // Кулуары — светлые прожилки снега вниз от гребней.
            p.style = Paint.Style.STROKE
            p.strokeCap = Paint.Cap.ROUND
            repeat(12) {
                var cx = peakX + r(-w * 0.16f, w * 0.12f)
                var cy = snowLine + r(-h * 0.02f, h * 0.03f)
                val path = Path(); path.moveTo(cx, cy)
                repeat(6) { cx += r(-0.8f, 0.8f) * u; cy += r(1.2f, 2.4f) * u; path.lineTo(cx, cy) }
                p.strokeWidth = r(0.3f, 0.8f) * u
                p.color = alpha(t.fg, rnd.nextInt(50) + 30)
                c.drawPath(path, p)
            }
            c.restore()

            // Ближние отроги — почти фон.
            p.reset(); p.isAntiAlias = true
            p.color = tone(0.05)
            fillBelow(ridge(h * 0.7f, h * 0.66f, h * 0.05f), p)
            p.color = t.bg
            fillBelow(ridge(h * 0.8f, h * 0.84f, h * 0.04f), p)
            fade(0.55f, 180)
        }

        /** Сакура: тёмная ветка через экран, розовые цветы и огоньки гирлянды. */
        fun blossom() {
            sky(tone(0.12), t.bg)
            glow(w * 0.8f, h * 0.1f, w * 0.9f, alpha(Look.mix(t.fg, t.acc, 0.3), 40))
            val bark = Look.mix(t.bg, 0xFF000000.toInt(), 0.5)
            // Ветки — изогнутые линии с отростками, а не дерево целиком:
            // рекурсивное дерево давало сотню кончиков, и цветы на них
            // слипались в вату. Главная — от правого края через верх, вторая —
            // короче, снизу слева. Угол со знаком Marvia остаётся пустым.
            val flowers = mutableListOf<Pair<Float, Float>>()
            branch(Path().apply { moveTo(w * 1.05f, h * 0.06f); cubicTo(w * 0.8f, h * 0.2f, w * 0.55f, h * 0.12f, w * 0.18f, h * 0.3f) },
                u * 2.8f, bark, flowers)
            branch(Path().apply { moveTo(-w * 0.05f, h * 0.5f); cubicTo(w * 0.12f, h * 0.44f, w * 0.22f, h * 0.47f, w * 0.38f, h * 0.38f) },
                u * 2f, bark, flowers)
            for ((fx, fy) in flowers) flower(fx, fy, r(1.6f, 2.6f) * u, rnd.nextFloat() < 0.15f)
            // Опавшие лепестки — редкие и размытые, как не в фокусе.
            repeat(18) {
                soft(r(0f, w), r(0.05f, 0.7f) * h, r(0.6f, 1.3f) * u, alpha(t.acc, rnd.nextInt(60) + 40), u * 0.5f)
            }
            // Гирлянда: яркая точка и ореол вокруг.
            repeat(24) {
                val (fx, fy) = flowers[rnd.nextInt(flowers.size)]
                val x = fx + r(-5f, 5f) * u
                val y = fy + r(-5f, 5f) * u
                glow(x, y, u * 2.6f, alpha(Look.mix(t.fg, t.acc, 0.2), 70))
                soft(x, y, u * 0.35f, alpha(t.fg, 250), 0f)
            }
            fade(0.6f)
        }

        /**
         * branch — ветка по кривой: толщина сходит на нет к концу, по пути
         * короткие отростки, а места для цветов собираются в flowers.
         */
        private fun branch(path: Path, width: Float, color: Int, flowers: MutableList<Pair<Float, Float>>) {
            val m = android.graphics.PathMeasure(path, false)
            val pos = FloatArray(2)
            val tan = FloatArray(2)
            val steps = 90
            p.reset(); p.isAntiAlias = true
            p.style = Paint.Style.STROKE
            p.strokeCap = Paint.Cap.ROUND
            p.color = color
            var prevX = 0f
            var prevY = 0f
            for (i in 0..steps) {
                val k = i / steps.toFloat()
                m.getPosTan(m.length * k, pos, tan)
                if (i > 0) {
                    p.strokeWidth = width * (1f - k * 0.8f)
                    c.drawLine(prevX, prevY, pos[0], pos[1], p)
                }
                prevX = pos[0]; prevY = pos[1]
                if (i % 9 == 4 && k > 0.15f) {
                    // Отросток вбок от ветки, к свету — чаще вверх.
                    val side = if (rnd.nextFloat() < 0.7f) -1f else 1f
                    val a = kotlin.math.atan2(tan[1], tan[0]) + side * r(0.5f, 1.1f)
                    val len = r(5f, 11f) * u
                    val ex = pos[0] + cos(a) * len
                    val ey = pos[1] + sin(a) * len
                    p.strokeWidth = width * 0.35f
                    c.drawLine(pos[0], pos[1], ex, ey, p)
                    flowers += ex to ey
                    if (rnd.nextFloat() < 0.6f) flowers += (pos[0] + ex) / 2 + r(-1f, 1f) * u to (pos[1] + ey) / 2
                }
                if (i % 3 == 0 && rnd.nextFloat() < 0.7f) flowers += pos[0] + r(-2f, 2f) * u to pos[1] + r(-2f, 2f) * u
            }
        }

        /** flower — пять лепестков вокруг тёмной серединки; дальние — размыты. */
        private fun flower(x: Float, y: Float, size: Float, far: Boolean) {
            val tone = Look.mix(t.acc, t.fg, r(0.1f, 0.45f).toDouble())
            val blur = if (far) size * 0.6f else 0f
            val turn = r(0f, 72f)
            for (i in 0 until 5) {
                val a = Math.toRadians((turn + i * 72).toDouble())
                soft(x + cos(a).toFloat() * size * 0.55f, y + sin(a).toFloat() * size * 0.55f, size * 0.52f,
                    alpha(tone, if (far) 120 else 225), blur)
            }
            if (!far) soft(x, y, size * 0.22f, alpha(Look.mix(t.acc, 0xFF6B1E3A.toInt(), 0.5), 230), 0f)
        }

        /** Огни: тёплые размытые круги, гуще наверху, и лучи снизу. */
        fun bokeh() {
            sky(Look.mix(t.bg, t.acc, 0.14), t.bg, Look.mix(t.bg, t.acc, 0.05), 0.6f)
            glow(w * 0.5f, h * 0.05f, w * 0.8f, alpha(t.acc, 40))
            // Лучи от источника под нижним краем.
            p.reset(); p.isAntiAlias = true
            p.style = Paint.Style.STROKE
            for (i in 0 until 40) {
                val a = Math.toRadians(-90.0 + r(-62f, 62f))
                p.strokeWidth = r(0.08f, 0.35f) * u
                p.color = alpha(t.acc, rnd.nextInt(40) + 12)
                val len = r(0.25f, 0.55f) * h
                c.drawLine(w * 0.5f, h * 1.04f, w * 0.5f + cos(a).toFloat() * len, h * 1.04f + sin(a).toFloat() * len, p)
            }
            repeat(70) {
                val y = h * 0.78f * rnd.nextFloat().pow(1.6f)
                val radius = r(1.5f, 11f) * u
                val col = Look.mix(t.acc, t.fg, r(0f, 0.45f).toDouble())
                soft(r(-0.05f, 1.05f) * w, y, radius, alpha(col, rnd.nextInt(70) + 22), radius * r(0.15f, 0.5f))
            }
            repeat(30) {
                val x = r(0f, w); val y = h * 0.7f * rnd.nextFloat().pow(1.3f)
                soft(x, y, u * 0.35f, alpha(t.fg, 230), 0f)
                p.reset(); p.isAntiAlias = true
                p.strokeWidth = u * 0.12f; p.color = alpha(t.fg, 120)
                c.drawLine(x - u * 1.6f, y, x + u * 1.6f, y, p)
                c.drawLine(x, y - u * 1.6f, x, y + u * 1.6f, p)
            }
            fade(0.6f)
        }

        /** Город: серое небо, башни с огнями окон и шпиль. */
        fun skyline() {
            sky(tone(0.22), t.bg, tone(0.1), 0.62f)
            fun block(x: Float, bw: Float, top: Float, body: Int, lit: Double) {
                p.reset(); p.isAntiAlias = true
                p.color = body
                c.drawRect(x, top, x + bw, h, p)
                val ww = u * 0.55f
                val wh = u * 0.75f
                var wy = top + u * 1.2f
                while (wy < h - wh) {
                    var wx = x + u * 0.7f
                    while (wx < x + bw - ww - u * 0.4f) {
                        if (rnd.nextFloat() < lit) {
                            p.color = alpha(Look.mix(t.fg, 0xFFFFE6B0.toInt(), 0.25), rnd.nextInt(130) + 40)
                            c.drawRect(wx, wy, wx + ww, wy + wh, p)
                        }
                        wx += u * 1.45f
                    }
                    wy += u * 1.7f
                }
            }
            // Дальний ряд — светлее, ближний — почти фон.
            var x = -u * 2
            while (x < w) { val bw = r(6f, 11f) * u; block(x, bw, h * r(0.48f, 0.66f), tone(0.12), 0.18); x += bw + r(0f, 1.5f) * u }
            // Шпиль — одна высокая башня, как у настоящего горизонта.
            val sx = w * 0.6f
            val sw = u * 9f
            block(sx, sw, h * 0.32f, tone(0.08), 0.28)
            p.reset(); p.isAntiAlias = true
            p.color = tone(0.08)
            val spire = Path(); spire.moveTo(sx, h * 0.32f); spire.lineTo(sx + sw / 2, h * 0.24f); spire.lineTo(sx + sw, h * 0.32f); spire.close()
            c.drawPath(spire, p)
            p.strokeWidth = u * 0.25f
            c.drawLine(sx + sw / 2, h * 0.24f, sx + sw / 2, h * 0.17f, p)
            soft(sx + sw / 2, h * 0.17f, u * 0.4f, alpha(t.fg, 220), 0f)
            x = -u * 3
            while (x < w) {
                val bw = r(7f, 14f) * u
                if (x + bw < sx - u || x > sx + sw + u) block(x, bw, h * r(0.58f, 0.78f), tone(0.04), 0.32)
                x += bw + r(0.5f, 2f) * u
            }
            fade(0.72f, 160)
        }

        /** Туман: дерево в дымке, бледно-розовые цветы и полосы тумана. */
        fun haze() {
            sky(tone(0.34), t.bg, tone(0.2), 0.5f)
            val near = Look.mix(t.bg, 0xFF000000.toInt(), 0.35)
            val far = tone(0.22)
            val blooms = mutableListOf<Pair<Float, Float>>()
            tree(w * 0.86f, h * 1.02f, -100f, h * 0.2f, u * 2.2f, 8, far) { x, y -> blooms += x to y }
            band(h * 0.55f, 34)
            tree(w * 0.1f, h * 1.02f, -78f, h * 0.24f, u * 3f, 9, near) { x, y -> blooms += x to y }
            for ((bx, by) in blooms) {
                if (rnd.nextFloat() < 0.55f) soft(bx + r(-1f, 1f) * u, by, r(0.4f, 1.1f) * u, alpha(t.acc, rnd.nextInt(90) + 80), 0f)
            }
            band(h * 0.3f, 22)
            band(h * 0.78f, 40)
            fade(0.62f, 170)
        }

        private fun band(y: Float, a: Int) {
            p.reset(); p.isAntiAlias = true
            p.color = alpha(t.fg, a)
            p.maskFilter = BlurMaskFilter(h * 0.06f, BlurMaskFilter.Blur.NORMAL)
            c.drawRect(-w * 0.1f, y - h * 0.04f, w * 1.1f, y + h * 0.04f, p)
            p.maskFilter = null
        }

        /** Осень: деревья по краям, фонари в тумане, листья. */
        fun autumn() {
            sky(Look.mix(t.bg, t.acc, 0.16), t.bg, Look.mix(t.bg, t.acc, 0.08), 0.5f)
            // Мокрая аллея уходит вдаль: светлее у горизонта, темнее у ног.
            val road = Path()
            road.moveTo(w * 0.12f, h); road.lineTo(w * 0.47f, h * 0.48f); road.lineTo(w * 0.53f, h * 0.48f); road.lineTo(w * 0.88f, h); road.close()
            p.reset(); p.isAntiAlias = true
            p.shader = LinearGradient(0f, h * 0.48f, 0f, h, Look.mix(t.bg, t.acc, 0.22), Look.mix(t.bg, t.fg, 0.04), Shader.TileMode.CLAMP)
            c.drawPath(road, p)
            p.shader = null
            band(h * 0.5f, 30)
            // Фонари вдоль аллеи: ближе — крупнее и ярче, и у каждого отблеск на мокром.
            for (i in 0 until 5) {
                val k = i / 4f
                for (side in listOf(-1f, 1f)) {
                    val x = w * (0.5f + side * (0.36f - k * 0.3f))
                    val y = h * (0.5f + (1f - k) * 0.12f)
                    glow(x, y, u * (14f - k * 9f), alpha(t.acc, 140 - i * 18))
                    soft(x, y, u * (0.9f - k * 0.5f), alpha(Look.mix(t.acc, t.fg, 0.6), 240), 0f)
                    p.reset(); p.isAntiAlias = true
                    p.color = alpha(t.acc, 50 - i * 8)
                    p.maskFilter = BlurMaskFilter(u * 1.2f, BlurMaskFilter.Blur.NORMAL)
                    c.drawRect(x - u * 0.8f, y + u * 3f, x + u * 0.8f, y + u * (3f + 16f * (1f - k)), p)
                    p.maskFilter = null
                }
            }
            val bark = Look.mix(t.bg, 0xFF000000.toInt(), 0.45)
            // Деревья у краёв, кроной вверх, а не к середине.
            tree(w * 0.02f, h * 1.0f, -93f, h * 0.3f, u * 3.6f, 8, bark, spread = 22f)
            tree(w * 0.98f, h * 1.0f, -87f, h * 0.28f, u * 3.4f, 8, bark, spread = 22f)
            repeat(60) {
                val x = r(0f, w); val y = r(0.08f, 0.95f) * h
                c.save(); c.rotate(r(0f, 180f), x, y)
                p.reset(); p.isAntiAlias = true
                p.color = alpha(Look.mix(t.acc, 0xFF7A3B12.toInt(), r(0f, 0.6f).toDouble()), rnd.nextInt(110) + 80)
                c.drawOval(x - u * 0.9f, y - u * 0.4f, x + u * 0.9f, y + u * 0.4f, p)
                c.restore()
            }
            fade(0.66f, 200)
        }

        /** Лилии: тёмное вино, крупный цветок в углу и второй, размытый, ниже. */
        fun lilies() {
            p.reset()
            p.shader = RadialGradient(w * 0.75f, h * 0.24f, max(w, h) * 0.75f,
                Look.mix(t.bg, t.acc, 0.24), t.bg, Shader.TileMode.CLAMP)
            c.drawRect(0f, 0f, w, h, p)
            p.shader = null
            lily(w * 0.78f, h * 0.24f, u * 30f, 0f)
            lily(w * 0.08f, h * 0.7f, u * 22f, u * 2.2f)
            repeat(8) {
                petal(r(0f, w), r(0.1f, 0.9f) * h, r(0f, 360f), r(6f, 12f) * u, r(1.6f, 3f) * u,
                    alpha(t.acc, 70), alpha(Look.mix(t.acc, t.fg, 0.4), 40), u * 1.4f)
            }
            fade(0.6f)
        }

        private fun lily(cx: Float, cy: Float, len: Float, blur: Float) {
            // Ореол за цветком: на тёмном вине он и даёт свет, как на снимке.
            if (blur == 0f) glow(cx, cy, len * 1.6f, alpha(t.acc, 70))
            val pale = Look.mix(t.fg, t.acc, 0.35)
            val deep = Look.mix(t.acc, t.bg, 0.15)
            val start = r(0f, 60f)
            // Задний ярус темнее и шире, передний — светлее: так цветок объёмный, а не звезда.
            for (i in 0 until 3) {
                petal(cx, cy, start + 60f + i * 120f + r(-10f, 10f), len * r(0.9f, 1.05f), len * 0.36f,
                    Look.mix(pale, t.bg, 0.25), Look.mix(deep, t.bg, 0.2), blur)
            }
            for (i in 0 until 3) {
                petal(cx, cy, start + i * 120f + r(-10f, 10f), len * r(0.95f, 1.1f), len * 0.32f, pale, deep, blur)
            }
            if (blur > 0f) return
            // Крапинки у основания лепестков.
            repeat(26) {
                val a = Math.toRadians(r(0f, 360f).toDouble())
                val d = len * r(0.12f, 0.42f)
                soft(cx + cos(a).toFloat() * d, cy + sin(a).toFloat() * d, u * r(0.15f, 0.35f), alpha(Look.mix(t.acc, 0xFF4A0E22.toInt(), 0.6), 200), 0f)
            }
            // Тычинки: тонкие линии с тёмными пыльниками.
            for (i in 0 until 6) {
                val a = Math.toRadians((start + 30 + i * 60 + r(-12f, 12f)).toDouble())
                val ex = cx + cos(a).toFloat() * len * 0.55f
                val ey = cy + sin(a).toFloat() * len * 0.55f
                p.reset(); p.isAntiAlias = true
                p.style = Paint.Style.STROKE
                p.strokeWidth = u * 0.25f
                p.color = alpha(Look.mix(pale, t.fg, 0.4), 220)
                c.drawLine(cx, cy, ex, ey, p)
                soft(ex, ey, u * 0.75f, Look.mix(t.bg, 0xFF5A1E14.toInt(), 0.6), 0f)
            }
        }

        /** Пудра: светлый, мягкие размытые лепестки по краям. */
        fun powder() {
            sky(Look.mix(t.bg, t.acc, 0.1), t.bg, Look.mix(t.bg, t.fg, 0.03), 0.5f)
            val pink = Look.mix(t.acc, t.bg, 0.25)
            for (i in 0 until 5) petal(w * 1.02f, h * 0.12f, 120f + i * 22f, u * r(26f, 38f), u * r(7f, 10f),
                alpha(pink, 120), alpha(Look.mix(pink, t.bg, 0.5), 60), u * 1.6f)
            for (i in 0 until 5) petal(-w * 0.02f, h * 0.88f, -60f + i * 22f, u * r(24f, 34f), u * r(6f, 9f),
                alpha(pink, 110), alpha(Look.mix(pink, t.bg, 0.5), 50), u * 1.6f)
            lily(w * 0.82f, h * 0.78f, u * 26f, u * 2.6f)
            repeat(26) {
                petal(r(0f, w), r(0f, h), r(0f, 360f), r(3f, 6f) * u, r(1.2f, 2f) * u,
                    alpha(pink, rnd.nextInt(70) + 50), alpha(Look.mix(pink, t.bg, 0.4), 40), u * 0.4f)
            }
            repeat(14) { soft(r(0f, w), r(0f, h), r(0.6f, 1.8f) * u, alpha(pink, rnd.nextInt(60) + 30), u * 0.6f) }
        }
    }
}
