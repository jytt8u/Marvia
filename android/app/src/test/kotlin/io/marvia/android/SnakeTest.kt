package io.marvia.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.random.Random

class SnakeTest {
    @Test fun theSnakeGrowsOnlyWhenItEats() {
        val board = SnakeBoard(10, 10, Random(1))
        val start = board.body.size
        repeat(3) { board.step() }
        assertEquals("без еды змея не растёт", start, board.body.size)
        assertFalse(board.over)
    }

    @Test fun aSwipeBackwardsDoesNotKillTheSnakeInOneMove() {
        // Змея ползёт вправо; свайп влево в тот же ход был бы самоубийством.
        val board = SnakeBoard(10, 10, Random(2))
        board.turn(SnakeBoard.Dir.LEFT)
        board.step()
        assertFalse(board.over)
        assertEquals(SnakeBoard.Dir.RIGHT, board.dir)
    }

    @Test fun wallsWrapAroundInsteadOfEndingTheGame() {
        val board = SnakeBoard(6, 6, Random(3))
        repeat(12) { board.step() }
        assertFalse("стена не должна убивать", board.over)
    }

    @Test fun bitingItselfEndsTheGame() {
        val board = SnakeBoard(12, 12, Random(4))
        // Отрастить змею до пяти клеток, кормя её прямо перед головой.
        while (board.body.size < 5) {
            val head = board.body.first()
            val field = SnakeBoard::class.java.getDeclaredField("food").apply { isAccessible = true }
            field.setInt(board, (head % 12 + 1) % 12 + head / 12 * 12)
            board.step()
        }
        board.turn(SnakeBoard.Dir.DOWN); board.step()
        board.turn(SnakeBoard.Dir.LEFT); board.step()
        board.turn(SnakeBoard.Dir.UP); board.step()
        assertTrue("змея укусила себя, а игра идёт", board.over)
    }

    @Test fun thirtyQuickTapsOpenTheGameAndSlowOnesDoNot() {
        var t = 100_000L
        repeat(SnakeGame.TAPS - 1) { assertFalse(SnakeGame.tap(t)); t += 200 }
        assertTrue(SnakeGame.tap(t))
        t += 10_000
        repeat(SnakeGame.TAPS) { assertFalse(SnakeGame.tap(t)); t += SnakeGame.TAP_GAP_MS + 1 }
    }
}
