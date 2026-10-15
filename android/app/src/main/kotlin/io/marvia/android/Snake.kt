package io.marvia.android

import android.annotation.SuppressLint
import android.app.Dialog
import android.content.Context
import android.graphics.Canvas
import android.graphics.RectF
import android.graphics.Typeface
import android.os.SystemClock
import android.view.Gravity
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import android.view.Window
import android.widget.FrameLayout
import android.widget.TextView
import kotlin.math.abs
import kotlin.random.Random
import android.graphics.Paint as GPaint

/**
 * Змейка — пасхалка: тридцать нажатий на знак в шапке главной.
 *
 * Своя игра, а не чужая: поле, змея и еда рисуются цветами темы, которую
 * человек выбрал, — у графита серая змея, у «Бумаги» чернильная. Рекорд
 * хранится на телефоне и никуда не уходит.
 */
class SnakeBoard(val cols: Int, val rows: Int, private val random: Random = Random.Default) {
    enum class Dir(val dx: Int, val dy: Int) { UP(0, -1), DOWN(0, 1), LEFT(-1, 0), RIGHT(1, 0) }

    /** Тело: первая клетка — голова. Клетка — x + y * cols. */
    val body = ArrayDeque<Int>()
    var food = -1
        private set
    var dir = Dir.RIGHT
        private set
    private var queued: Dir? = null
    var score = 0
        private set
    var over = false
        private set

    init { reset() }

    fun reset() {
        body.clear()
        val y = rows / 2
        for (x in 3 downTo 1) body.addLast(x + y * cols)
        dir = Dir.RIGHT
        queued = null
        score = 0
        over = false
        placeFood()
    }

    /** turn — поворот к следующему шагу; разворот назад игнорируется, иначе змея съела бы себя одним свайпом. */
    fun turn(next: Dir) {
        val base = queued ?: dir
        if (next.dx == -base.dx && next.dy == -base.dy) return
        queued = next
    }

    /** step — один ход. Стены не убивают: змея выходит с другой стороны — так легче на маленьком экране. */
    fun step() {
        if (over) return
        queued?.let { dir = it }
        queued = null
        val head = body.first()
        val x = (head % cols + dir.dx + cols) % cols
        val y = (head / cols + dir.dy + rows) % rows
        val next = x + y * cols
        val eats = next == food
        // Хвост уходит в тот же ход, поэтому шаг в клетку хвоста — не столкновение.
        val tail = if (eats) -1 else body.last()
        if (next in body && next != tail) {
            over = true
            return
        }
        body.addFirst(next)
        if (eats) {
            score++
            placeFood()
        } else {
            body.removeLast()
        }
    }

    private fun placeFood() {
        val free = (0 until cols * rows).filter { it !in body }
        food = if (free.isEmpty()) -1 else free[random.nextInt(free.size)]
        if (food < 0) over = true
    }

    /** Задержка между ходами: быстрее с каждым кусочком, но не быстрее, чем успевает палец. */
    fun delayMs(): Long = (170 - score * 4).coerceAtLeast(70).toLong()
}

@SuppressLint("ViewConstructor")
class SnakeView(context: Context, private val theme: Theme, private val store: Store) : View(context) {
    private val board = SnakeBoard(COLS, ROWS)
    private val brush = GPaint(GPaint.ANTI_ALIAS_FLAG)
    private val text = GPaint(GPaint.ANTI_ALIAS_FLAG).apply {
        textAlign = GPaint.Align.CENTER
        typeface = Typeface.create(Typeface.DEFAULT, Typeface.BOLD)
    }
    private val cell = RectF()
    private var running = false
    private var downX = 0f
    private var downY = 0f
    var onScore: (Int, Int) -> Unit = { _, _ -> }

    private val tick = object : Runnable {
        override fun run() {
            if (!running) return
            board.step()
            if (board.over) {
                running = false
                if (board.score > store.snakeBest) store.snakeBest = board.score
            }
            onScore(board.score, store.snakeBest)
            invalidate()
            if (running) postDelayed(this, board.delayMs())
        }
    }

    init {
        isClickable = true
        contentDescription = context.getString(R.string.snake_title)
    }

    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        val w = MeasureSpec.getSize(widthMeasureSpec)
        setMeasuredDimension(w, w * ROWS / COLS)
    }

    override fun onDraw(canvas: Canvas) {
        val size = width.toFloat() / COLS
        val r = minOf(theme.r.toFloat(), size * 0.45f)
        brush.color = theme.surf
        canvas.drawRoundRect(0f, 0f, width.toFloat(), height.toFloat(), r * 2, r * 2, brush)
        brush.color = theme.line
        for (i in 0 until COLS * ROWS) {
            if ((i % COLS + i / COLS) % 2 == 0) continue
            rect(i, size, 0.46f)
            canvas.drawCircle(cell.centerX(), cell.centerY(), size * 0.05f, brush)
        }
        if (board.food >= 0) {
            brush.color = theme.warn
            rect(board.food, size, 0.18f)
            canvas.drawOval(cell, brush)
        }
        board.body.forEachIndexed { index, c ->
            // Голова — акцентом, хвост светлеет к концу: видно, куда ползёт.
            val fade = 1f - index.toFloat() / (board.body.size + 4)
            brush.color = blend(theme.accSoft, theme.acc, fade)
            rect(c, size, 0.08f)
            canvas.drawRoundRect(cell, r * 0.6f, r * 0.6f, brush)
        }
        if (!running) {
            brush.color = (theme.bg and 0x00ffffff) or (0xc0 shl 24)
            canvas.drawRoundRect(0f, 0f, width.toFloat(), height.toFloat(), r * 2, r * 2, brush)
            text.color = theme.fg
            text.textSize = size * 1.1f
            val title = if (board.over) context.getString(R.string.snake_over, board.score) else context.getString(R.string.snake_title)
            canvas.drawText(title, width / 2f, height * 0.3f, text)
            text.color = theme.dim
            text.textSize = size * 0.62f
            canvas.drawText(context.getString(R.string.snake_hint), width / 2f, height * 0.3f + size * 1.3f, text)
        }
    }

    private fun rect(c: Int, size: Float, inset: Float) {
        val x = (c % COLS) * size
        val y = (c / COLS) * size
        cell.set(x + size * inset, y + size * inset, x + size * (1 - inset), y + size * (1 - inset))
    }

    @SuppressLint("ClickableViewAccessibility")
    override fun onTouchEvent(event: MotionEvent): Boolean {
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> { downX = event.x; downY = event.y }
            MotionEvent.ACTION_UP -> {
                val dx = event.x - downX
                val dy = event.y - downY
                val swipe = 24 * resources.displayMetrics.density
                if (abs(dx) < swipe && abs(dy) < swipe) {
                    // Касание — пауза и продолжение; после проигрыша — новая игра.
                    if (board.over) board.reset()
                    running = !running
                    if (running) postDelayed(tick, board.delayMs()) else removeCallbacks(tick)
                    invalidate()
                    performClick()
                } else if (abs(dx) > abs(dy)) {
                    board.turn(if (dx > 0) SnakeBoard.Dir.RIGHT else SnakeBoard.Dir.LEFT)
                } else {
                    board.turn(if (dy > 0) SnakeBoard.Dir.DOWN else SnakeBoard.Dir.UP)
                }
            }
        }
        return true
    }

    override fun performClick(): Boolean = super.performClick()

    override fun onDetachedFromWindow() {
        running = false
        removeCallbacks(tick)
        super.onDetachedFromWindow()
    }

    private fun blend(a: Int, b: Int, t: Float): Int {
        fun ch(shift: Int) = (((a shr shift) and 0xff) * (1 - t) + ((b shr shift) and 0xff) * t).toInt() and 0xff
        return (0xff shl 24) or (ch(16) shl 16) or (ch(8) shl 8) or ch(0)
    }

    companion object {
        const val COLS = 16
        const val ROWS = 22
    }
}

/** SnakeGame — окно игры поверх приложения: счёт, рекорд, поле и «закрыть». */
object SnakeGame {
    /** Сколько нажатий на знак открывают игру и сколько можно медлить между ними. */
    const val TAPS = 30
    const val TAP_GAP_MS = 1500L

    private var taps = 0
    private var lastTap = 0L

    /** tap — ещё одно нажатие на знак; true — пора открыть игру. */
    fun tap(now: Long = SystemClock.uptimeMillis()): Boolean {
        taps = if (now - lastTap > TAP_GAP_MS) 1 else taps + 1
        lastTap = now
        if (taps >= TAPS) {
            taps = 0
            return true
        }
        return false
    }

    fun show(context: Context, theme: Theme, store: Store) {
        val dp = context.resources.displayMetrics.density
        val dialog = Dialog(context)
        dialog.requestWindowFeature(Window.FEATURE_NO_TITLE)
        val score = TextView(context).apply {
            setTextColor(theme.fg)
            textSize = 16f
            typeface = Typeface.create(Typeface.MONOSPACE, Typeface.BOLD)
        }
        val close = TextView(context).apply {
            setText(R.string.snake_close)
            setTextColor(theme.acc)
            textSize = 15f
            setPadding((12 * dp).toInt(), (8 * dp).toInt(), (12 * dp).toInt(), (8 * dp).toInt())
            isClickable = true
            setOnClickListener { dialog.dismiss() }
        }
        val game = SnakeView(context, theme, store)
        fun showScore(now: Int, best: Int) {
            score.text = context.getString(R.string.snake_score, now, best)
        }
        game.onScore = ::showScore
        showScore(0, store.snakeBest)
        val head = FrameLayout(context).apply {
            addView(score, FrameLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT, Gravity.START or Gravity.CENTER_VERTICAL))
            addView(close, FrameLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT, Gravity.END or Gravity.CENTER_VERTICAL))
        }
        val root = android.widget.LinearLayout(context).apply {
            orientation = android.widget.LinearLayout.VERTICAL
            setPadding((16 * dp).toInt(), (12 * dp).toInt(), (16 * dp).toInt(), (16 * dp).toInt())
            background = Paint.rounded(theme.bg, theme.r + 6, dp)
            addView(head, android.widget.LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT))
            addView(game, android.widget.LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT).apply { topMargin = (10 * dp).toInt() })
        }
        dialog.setContentView(root)
        dialog.window?.setBackgroundDrawableResource(android.R.color.transparent)
        dialog.window?.setLayout((context.resources.displayMetrics.widthPixels * 0.94).toInt(), ViewGroup.LayoutParams.WRAP_CONTENT)
        dialog.show()
    }
}
