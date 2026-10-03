package io.marvia.android

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import com.google.zxing.BarcodeFormat
import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.NotFoundException
import com.google.zxing.RGBLuminanceSource
import com.google.zxing.ReaderException
import com.google.zxing.common.GlobalHistogramBinarizer
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeReader

/**
 * QrImport — ключ с картинки QR-кода.
 *
 * Продавцы часто присылают ключ не строкой, а QR-кодом — скриншотом в чат, —
 * и покупатель переписывал бы его руками, если бы было с чего. Распознаём с
 * картинки из галереи: для этого не нужно разрешение на камеру, а скриншот —
 * самый частый случай.
 *
 * ZXing core, а не ML Kit: тот требует сервисов Google, а приложение ставят и
 * на телефоны без них, и раздают мимо магазина.
 */
object QrImport {
    /**
     * Самая длинная сторона, до которой ужимаем картинку. Скриншот в 4000
     * точек распознаётся не лучше, чем в 1600, а памяти и времени требует
     * вшестеро больше.
     */
    private const val MAX_SIDE = 1600

    /** Ключ с картинки; null — QR-кода на ней нет или он не прочитался. */
    fun fromUri(context: Context, uri: Uri): String? {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        context.contentResolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) }
        if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return null

        var sample = 1
        while (maxOf(bounds.outWidth, bounds.outHeight) / (sample * 2) >= MAX_SIDE) sample *= 2
        val opts = BitmapFactory.Options().apply { inSampleSize = sample }
        val bitmap = context.contentResolver.openInputStream(uri)?.use {
            BitmapFactory.decodeStream(it, null, opts)
        } ?: return null
        return try {
            fromBitmap(bitmap)
        } finally {
            bitmap.recycle()
        }
    }

    private fun fromBitmap(bitmap: Bitmap): String? {
        val pixels = IntArray(bitmap.width * bitmap.height)
        bitmap.getPixels(pixels, 0, bitmap.width, 0, 0, bitmap.width, bitmap.height)
        return decode(bitmap.width, bitmap.height, pixels)
    }

    /**
     * Распознаёт QR-код в пикселях ARGB. Отдельно от Bitmap, чтобы проверять
     * его обычным тестом, без телефона.
     */
    fun decode(width: Int, height: Int, pixels: IntArray): String? {
        // Сначала как есть, потом мельче. ZXing не находит крупный и очень
        // чёткий код: при модуле больше трёх точек он теряет искатели, хотя
        // тот же код вдвое мельче читает уверенно. Скриншот QR с компьютера —
        // ровно такой случай, поэтому уменьшаем, как это делают сканеры.
        var w = width
        var h = height
        var px = pixels
        for (step in 0..2) {
            readAny(w, h, px)?.let { return it }
            if (w < 200 || h < 200) break
            px = halve(w, h, px)
            w /= 2
            h /= 2
        }
        // Последняя попытка — код без фона вокруг: вырезанный из окна или
        // присланный файлом картинки.
        return read(BinaryBitmap(HybridBinarizer(RGBLuminanceSource(width, height, pixels))),
            mapOf(DecodeHintType.PURE_BARCODE to true))
    }

    private fun readAny(width: Int, height: Int, pixels: IntArray): String? {
        val source = RGBLuminanceSource(width, height, pixels)
        val hints = mapOf(
            DecodeHintType.POSSIBLE_FORMATS to listOf(BarcodeFormat.QR_CODE),
            // Скриншот чата: код мелкий, вокруг текст и пузыри сообщений.
            DecodeHintType.TRY_HARDER to true,
        )
        // Два способа отделить чёрное от белого: локальный хорош на фото с
        // неровным светом, общий — на чистом скриншоте.
        return read(BinaryBitmap(HybridBinarizer(source)), hints)
            ?: read(BinaryBitmap(GlobalHistogramBinarizer(source)), hints)
    }

    private fun read(binary: BinaryBitmap, hints: Map<DecodeHintType, Any>): String? {
        val text = try {
            QRCodeReader().decode(binary, hints).text
        } catch (_: NotFoundException) {
            null
        } catch (_: ReaderException) {
            null
        }
        return text?.trim()?.takeIf { it.isNotEmpty() }
    }

    /** halve уменьшает вдвое, усредняя каждые четыре точки по яркости. */
    private fun halve(width: Int, height: Int, pixels: IntArray): IntArray {
        val w = width / 2
        val h = height / 2
        val out = IntArray(w * h)
        for (y in 0 until h) for (x in 0 until w) {
            var sum = 0
            for (dy in 0..1) for (dx in 0..1) {
                val p = pixels[(y * 2 + dy) * width + x * 2 + dx]
                sum += ((p shr 16 and 0xFF) + (p shr 8 and 0xFF) + (p and 0xFF)) / 3
            }
            val g = sum / 4
            out[y * w + x] = (0xFF shl 24) or (g shl 16) or (g shl 8) or g
        }
        return out
    }
}
