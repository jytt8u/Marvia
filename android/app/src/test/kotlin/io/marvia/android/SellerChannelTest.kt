package io.marvia.android

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SellerChannelTest {
    @Test
    fun sellerLinksOpenOnlyInBrowserOrTelegram() {
        assertTrue(SellerChannel.openable("https://t.me/seller_support"))
        assertTrue(SellerChannel.openable("tg://resolve?domain=seller_bot"))
        assertTrue(SellerChannel.openable("HTTPS://shop.example/renew"))
    }

    /** Ядро ссылки уже проверило, но кнопка — последняя точка перед системой. */
    @Test
    fun anyOtherSchemeIsNotHandedToTheSystem() {
        for (bad in listOf(
            "", "http://shop.example", "intent://scan/#Intent;scheme=zxing;end",
            "javascript:alert(1)", "file:///sdcard/x", "content://x", "market://details?id=x",
            "https://", "tg:",
        )) {
            assertFalse(bad, SellerChannel.openable(bad))
        }
    }

    @Test
    fun theSameReminderIsShownOnce() {
        val seen = mutableSetOf<String>()
        assertTrue(SellerChannel.firstTime(seen, "expiry:3:2026-10-10T00:00:00Z"))
        assertFalse(SellerChannel.firstTime(seen, "expiry:3:2026-10-10T00:00:00Z"))
        assertTrue(SellerChannel.firstTime(seen, "expiry:1:2026-10-10T00:00:00Z"))
        assertFalse(SellerChannel.firstTime(seen, ""))
    }
}
