package io.marvia.android

import android.content.Context
import android.graphics.BlurMaskFilter
import android.graphics.Canvas
import android.graphics.LinearGradient
import android.graphics.Path
import android.graphics.RadialGradient
import android.graphics.RectF
import android.graphics.Region
import android.graphics.Shader
import android.graphics.Typeface
import android.view.View
import androidx.core.graphics.ColorUtils

/**
 * Миниатюра образа — кусочек стола, как на ПК: обои образа и маленькое окно
 * в его оболочке — с его кнопками окна, боковой панелью, кнопкой
 * подключения и надписью «Подключено» его шрифтом. Так видно не палитру, а
 * то, как будет выглядеть приложение. Размеры — из CSS миниатюры на ПК
 * (.vibe-art и .mw в app.html) в её пикселях, растянутых на свою высоту.
 *
 * Рисуется один раз в растр и берётся из кэша: лента пересобирается на
 * каждое нажатие, а обои с размытием и тень окна на каждый кадр прокрутки
 * стоили бы плавности.
 */
class AppearancePreview(context: Context, private val profile: AppearanceProfiles.Profile) : View(context) {
    private val theme = AppearanceProfiles.theme(profile.choice, profile.id)
    private val status = context.getString(R.string.status_on)

    /** Надпись — шрифтом надписи образа: моноширинным у табло и у плиточных, как sfont на ПК. */
    private val mono = profile.style == AppearanceStyle.SIGNAL || profile.shell == AppearanceProfiles.TILE
    private val face: Typeface? = Paint.family(context, if (mono) Store.FONT_MONO else profile.font)
    private val caps = profile.style == AppearanceStyle.SIGNAL

    override fun onDraw(canvas: Canvas) {
        val bmp = Wallpaper.cached("mini:${profile.id}:$status", width, height) { draw(it, width.toFloat(), height.toFloat()) } ?: return
        canvas.drawBitmap(bmp, 0f, 0f, null)
    }

    private fun brush() = android.graphics.Paint(android.graphics.Paint.ANTI_ALIAS_FLAG)

    private fun draw(c: Canvas, w: Float, h: Float) {
        // Пиксель миниатюры на ПК: там она высотой 128.
        val u = h / 128f
        val scene = profile.scene
        if (scene != null) Wallpaper.draw(c, scene.wall, w, h) else art(c, w, h, u)
        window(c, w, h, u)
    }

    /** art — фон наших трёх, у которых обоев нет: свет «Графита» и точки табло, как art на ПК. */
    private fun art(c: Canvas, w: Float, h: Float, u: Float) {
        val p = brush()
        val (step, dot) = when (profile.id) {
            "dot" -> {
                c.drawColor(0xFF000000.toInt())
                7f to 0x4DFFFFFF
            }
            "paper" -> {
                c.drawColor(0xFFF6F3EC.toInt())
                7f to 0x3D14120E
            }
            else -> {
                // radial-gradient(120% 90% at 20% 0%) — эллипс в долях ширины и высоты.
                p.shader = RadialGradient(0f, 0f, 1f, intArrayOf(0xFF4A4F55.toInt(), 0xFF16181B.toInt(), 0xFF0B0C0E.toInt()), floatArrayOf(0f, .6f, 1f), Shader.TileMode.CLAMP).apply {
                    setLocalMatrix(android.graphics.Matrix().apply { setScale(w * 1.2f, h * .9f); postTranslate(w * .2f, 0f) })
                }
                c.drawRect(0f, 0f, w, h, p)
                p.shader = null
                11f to 0x29C3CCD6
            }
        }
        p.color = dot
        val s = step * u
        var y = s / 2
        while (y < h) {
            var x = s / 2
            while (x < w) { c.drawCircle(x, y, 1.3f * u, p); x += s }
            y += s
        }
    }

    private fun window(c: Canvas, w: Float, h: Float, u: Float) {
        val t = theme
        val shell = profile.shell
        val tile = shell == AppearanceProfiles.TILE
        val box = if (tile) RectF(w * .06f, h * .10f, w * .94f, h * .92f) else RectF(w * .14f, h * .16f, w * .86f, h * .88f)
        val r = when (shell) { "mac", "gnome", "ubuntu" -> 9f; "win11" -> 7f; "w98", "xp" -> 0f; else -> 6f } * u
        val p = brush()

        // Очертание окна: у XP скруглён только верх.
        val outline = Path().apply {
            if (shell == "xp") addRoundRect(box, floatArrayOf(6 * u, 6 * u, 6 * u, 6 * u, 0f, 0f, 0f, 0f), Path.Direction.CW)
            else addRoundRect(box, r, r, Path.Direction.CW)
        }

        // Тень окна: 0 10px 24px -10px и волосяная обводка, как на ПК. Тень
        // CSS под самим окном не рисуется — и здесь она вырезана, иначе
        // проступала бы сквозь стекло macOS. У плиток окна нет — есть сами
        // плитки; у 98-й тень жёсткая.
        if (!tile) {
            c.save()
            if (android.os.Build.VERSION.SDK_INT >= 26) c.clipOutPath(outline) else @Suppress("DEPRECATION") c.clipPath(outline, Region.Op.DIFFERENCE)
            if (shell == "w98") {
                p.color = 0x4D000000
                c.drawRect(box.left + 4 * u, box.top + 4 * u, box.right + 4 * u, box.bottom + 4 * u, p)
            } else {
                // Размытие 24px — сигма 12; BlurMaskFilter ждёт радиус.
                p.color = 0x8C000000.toInt()
                p.maskFilter = BlurMaskFilter((12 * u - .5f) / .57735f, BlurMaskFilter.Blur.NORMAL)
                c.drawRoundRect(box.left + 10 * u, box.top + 20 * u, box.right - 10 * u, box.bottom, r, r, p)
                p.maskFilter = null
                p.color = 0x2E000000
                c.drawRoundRect(box.left - u, box.top - u, box.right + u, box.bottom + u, r + u, r + u, p)
            }
            c.restore()
        }

        // Окно; всё, что внутри, обрезано по нему, как overflow: hidden.
        c.save()
        if (!tile) {
            p.color = when (shell) {
                "mac" -> ColorUtils.setAlphaComponent(t.bg, 209)
                "win11" -> ColorUtils.setAlphaComponent(t.bg, 230)
                "w98" -> VibeDetails.SILVER
                else -> t.bg
            }
            c.drawPath(outline, p)
            c.clipPath(outline)
        }
        if (shell == "w98") VibeDetails.Bevel(VibeDetails.SILVER, u, raised = true, title = false).apply {
            setBounds(box.left.toInt(), box.top.toInt(), box.right.toInt(), box.bottom.toInt())
        }.draw(c)
        // У 98-й содержимое отступает от фаски на 2px.
        val inner = if (shell == "w98") RectF(box.left + 2 * u, box.top + 2 * u, box.right - 2 * u, box.bottom - 2 * u) else RectF(box)
        val gap = if (tile) 3 * u else 0f
        val bar = RectF(inner.left, inner.top, inner.right, inner.top + (if (shell == "xp") 15f else 14f) * u)
        val body = RectF(inner.left, bar.bottom + gap, inner.right, inner.bottom)
        titleBar(c, bar, u)

        // Боковая панель и главная часть.
        val side = RectF(body.left, body.top, body.left + inner.width() * .26f, body.bottom)
        val main = RectF(side.right + gap, body.top, body.right, body.bottom)
        when (shell) {
            "mac" -> {
                p.color = ColorUtils.setAlphaComponent(t.fg, 31)
                c.drawRoundRect(side.left + 4 * u, side.top, side.right, side.bottom - 4 * u, 6 * u, 6 * u, p)
            }
            "xp" -> {
                p.shader = LinearGradient(0f, side.top, 0f, side.bottom, 0xFF7BA2E7.toInt(), 0xFF6375D6.toInt(), Shader.TileMode.CLAMP)
                c.drawRect(side, p)
                p.shader = null
            }
            "w98" -> {
                p.color = VibeDetails.SILVER
                c.drawRect(side, p)
                p.color = 0xFF808080.toInt()
                c.drawRect(side.right - u, side.top, side.right, side.bottom, p)
            }
            AppearanceProfiles.TILE -> {
                plate(c, side, t, u, t.line, u)
                plate(c, main, t, u, t.acc, 1.5f * u)
            }
            else -> {
                p.color = ColorUtils.setAlphaComponent(t.fg, 13)
                c.drawRect(side, p)
            }
        }
        button(c, main, u)
        c.restore()
    }

    /** plate — плитка: фон окна на просвет и рамка внутри, серая у остальных и цветная у активной. */
    private fun plate(c: Canvas, box: RectF, t: Theme, u: Float, line: Int, width: Float) {
        val p = brush()
        p.color = ColorUtils.setAlphaComponent(t.bg, 217)
        c.drawRoundRect(box, 4 * u, 4 * u, p)
        p.style = android.graphics.Paint.Style.STROKE
        p.strokeWidth = width
        p.color = line
        val half = width / 2
        c.drawRoundRect(box.left + half, box.top + half, box.right - half, box.bottom - half, 4 * u - half, 4 * u - half, p)
    }

    /** titleBar — полоска заголовка с кнопками окна той системы, чей образ. */
    private fun titleBar(c: Canvas, bar: RectF, u: Float) {
        val t = theme
        val p = brush()
        when (profile.shell) {
            "xp" -> {
                p.shader = LinearGradient(0f, bar.top, 0f, bar.bottom, intArrayOf(0xFF3D95FF.toInt(), 0xFF0053EE.toInt(), 0xFF003DD7.toInt()), floatArrayOf(0f, .4f, 1f), Shader.TileMode.CLAMP)
                c.drawRect(bar, p)
                p.shader = null
            }
            "w98" -> {
                p.shader = LinearGradient(bar.left, 0f, bar.right, 0f, VibeDetails.NAVY, 0xFF1084D0.toInt(), Shader.TileMode.CLAMP)
                c.drawRect(bar, p)
                p.shader = null
            }
            AppearanceProfiles.TILE -> {
                p.color = ColorUtils.setAlphaComponent(t.bg, 217)
                c.drawRoundRect(bar, 4 * u, 4 * u, p)
            }
        }
        // Три кнопки окна: справа, а у macOS — светофор слева.
        val cy = bar.centerY()
        val mac = profile.shell == "mac"
        val (bw, bh) = when (profile.shell) {
            "mac" -> 5f to 5f
            "xp" -> 8f to 8f
            "w98" -> 7f to 6f
            "gnome", "ubuntu", "kde" -> 6f to 6f
            "win11" -> 6f to 1f
            else -> 7f to 5f
        }
        val step = (bw + 3) * u
        val start = if (mac) bar.left + 5 * u else bar.right - 5 * u - 3 * bw * u - 6 * u
        for (i in 0..2) {
            val left = start + i * step
            val box = RectF(left, cy - bh * u / 2, left + bw * u, cy + bh * u / 2)
            val last = i == 2
            when (profile.shell) {
                "mac" -> {
                    p.color = intArrayOf(0xFFFF5F57.toInt(), 0xFFFEBC2E.toInt(), 0xFF28C840.toInt())[i]
                    c.drawOval(box, p)
                }
                "xp" -> {
                    p.color = if (last) 0xFFE0532F.toInt() else 0xFF2A6CF0.toInt()
                    c.drawRoundRect(box, 2 * u, 2 * u, p)
                    p.style = android.graphics.Paint.Style.STROKE
                    p.strokeWidth = u
                    p.color = 0xFFFFFFFF.toInt()
                    c.drawRoundRect(box.left + u / 2, box.top + u / 2, box.right - u / 2, box.bottom - u / 2, 2 * u, 2 * u, p)
                    p.style = android.graphics.Paint.Style.FILL
                }
                "w98" -> {
                    p.color = VibeDetails.SILVER
                    c.drawRect(box, p)
                    p.color = 0xFF0A0A0A.toInt()
                    c.drawRect(box.left, box.bottom - u, box.right, box.bottom, p)
                    c.drawRect(box.right - u, box.top, box.right, box.bottom, p)
                    p.color = 0xFFFFFFFF.toInt()
                    c.drawRect(box.left, box.top, box.right - u, box.top + u, p)
                    c.drawRect(box.left, box.top, box.left + u, box.bottom - u, p)
                }
                "gnome", "ubuntu", "kde" -> {
                    p.color = when {
                        last && profile.shell == "ubuntu" -> 0xFFE95420.toInt()
                        last && profile.shell == "kde" -> 0xFFDA4453.toInt()
                        else -> ColorUtils.setAlphaComponent(t.fg, 115)
                    }
                    c.drawOval(box, p)
                }
                else -> {
                    p.color = ColorUtils.setAlphaComponent(t.fg, 115)
                    c.drawRoundRect(box, if (profile.shell == "win11") 0f else u, if (profile.shell == "win11") 0f else u, p)
                }
            }
        }
    }

    /** button — кнопка подключения и надпись под ней, по центру главной части. */
    private fun button(c: Canvas, main: RectF, u: Float) {
        val t = theme
        val p = brush()
        val size = 30 * u
        val text = if (caps) status.uppercase() else status
        val label = brush().apply {
            typeface = Typeface.create(face, Typeface.BOLD)
            color = t.fg
            textSize = 10 * u
            letterSpacing = if (caps) .04f else -.01f
        }
        // Подгонка, как fitLabel на ПК: широкий шрифт заглавными на другом
        // языке иначе вылез бы за окно.
        val room = main.width() - 8 * u
        while (label.measureText(text) > room && label.textSize > 6 * u) label.textSize -= .5f * u
        val lineH = label.textSize * 1.1f
        val total = size + 4 * u + lineH
        val top = main.centerY() - total / 2
        val cx = main.centerX()
        val cy = top + size / 2
        val icon: Int
        when (profile.shell) {
            "xp" -> {
                p.shader = RadialGradient(cx - size * .15f, cy - size * .25f, size * .99f,
                    intArrayOf(0xFF8EE07A.toInt(), 0xFF3C9D2B.toInt(), 0xFF23731A.toInt()), floatArrayOf(0f, .55f, 1f), Shader.TileMode.CLAMP)
                c.drawCircle(cx, cy, size / 2, p)
                p.shader = null
                icon = 0xFFFFFFFF.toInt()
            }
            "w98" -> {
                VibeDetails.Bevel(VibeDetails.SILVER, u, raised = true, title = false).apply {
                    setBounds((cx - size / 2).toInt(), (cy - size / 2).toInt(), (cx + size / 2).toInt(), (cy + size / 2).toInt())
                }.draw(c)
                icon = 0xFF000000.toInt()
            }
            else -> {
                p.color = ColorUtils.setAlphaComponent(t.surf, 217)
                c.drawCircle(cx, cy, size / 2, p)
                p.style = android.graphics.Paint.Style.STROKE
                p.strokeWidth = 1.5f * u
                p.color = t.acc
                c.drawCircle(cx, cy, size / 2 - .75f * u, p)
                icon = t.acc
            }
        }
        // Значок питания: кольцо без верхней четверти и черта сверху.
        p.style = android.graphics.Paint.Style.STROKE
        p.strokeWidth = 1.8f * u
        p.strokeCap = android.graphics.Paint.Cap.ROUND
        p.color = icon
        val ring = 4.1f * u
        c.drawArc(cx - ring, cy - ring, cx + ring, cy + ring, -45f, 270f, false, p)
        // Черта — ::after высотой 6px с верхом на 4px выше внутреннего края кольца.
        c.drawLine(cx, cy - 7.2f * u + .9f * u, cx, cy - 1.2f * u - .9f * u, p)

        c.drawText(text, cx - label.measureText(text) / 2, top + size + 4 * u + label.textSize * .9f, label)
    }
}
