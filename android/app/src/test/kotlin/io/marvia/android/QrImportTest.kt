package io.marvia.android

import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.qrcode.QRCodeWriter
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class QrImportTest {
    /**
     * QR-код посреди «скриншота»: белое поле с отступом, как в чате. Пиксели
     * рисуем сами, без Bitmap, — так тест идёт без телефона.
     */
    private fun screenshot(text: String, scale: Int = 4, margin: Int = 120): Triple<Int, Int, IntArray> {
        // UTF-8 — так кодируют панели, боты и генераторы QR: имя ноды по-русски.
        val hints = mapOf(EncodeHintType.CHARACTER_SET to "UTF-8")
        val matrix = QRCodeWriter().encode(text, BarcodeFormat.QR_CODE, 0, 0, hints)
        val width = matrix.width * scale + margin * 2
        val height = matrix.height * scale + margin * 2
        val pixels = IntArray(width * height) { 0xFFF2F2F2.toInt() }
        for (y in 0 until matrix.height) for (x in 0 until matrix.width) {
            if (!matrix[x, y]) continue
            for (dy in 0 until scale) for (dx in 0 until scale) {
                pixels[(margin + y * scale + dy) * width + margin + x * scale + dx] = 0xFF101010.toInt()
            }
        }
        return Triple(width, height, pixels)
    }

    @Test
    fun keyFromQrScreenshotIsReadExactly() {
        val key = "marvia://k3j9x2mq8w1z4v7pQwErTy@panel.example.com/sub/AbCdEf0123456789#Мой доступ"
        val (w, h, px) = screenshot(key)
        assertEquals(key, QrImport.decode(w, h, px))
    }

    @Test
    fun foreignVlessLinkIsReadToo() {
        val link = "vless://6f1a6c2e-3d4b-4e5f-8a9b-0c1d2e3f4a5b@198.51.100.7:443?security=reality&pbk=abc&sid=01&sni=www.microsoft.com&type=tcp#Финляндия"
        val (w, h, px) = screenshot(link)
        assertEquals(link, QrImport.decode(w, h, px))
    }

    /** Скриншот QR с экрана компьютера: модуль в восемь точек. Такой ZXing
     *  сам не находил — читается только после уменьшения. */
    @Test
    fun largeCrispQrFromComputerScreenIsRead() {
        val link = "vless://6f1a6c2e-3d4b-4e5f-8a9b-0c1d2e3f4a5b@198.51.100.7:443?security=reality&sni=www.microsoft.com#Финляндия"
        val (w, h, px) = screenshot(link, scale = 8)
        assertEquals(link, QrImport.decode(w, h, px))
    }

    @Test
    fun pictureWithoutQrGivesNothing() {
        val px = IntArray(400 * 300) { if (it % 7 == 0) 0xFF000000.toInt() else 0xFFFFFFFF.toInt() }
        assertNull(QrImport.decode(400, 300, px))
    }
}
