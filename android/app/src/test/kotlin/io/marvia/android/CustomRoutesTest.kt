package io.marvia.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class CustomRoutesTest {
    @Test fun addressesSubnetsAndSitesAreUnderstoodAsPeopleTypeThem() {
        val parsed = CustomRoutes.parse(
            """
            192.168.50.10
            10.20.0.0/16, https://gosuslugi.ru/help?x=1
            # рабочий сервер
            work.example.ru:8443
            2a00:1450:4010::/48
            """.trimIndent(),
        )
        assertEquals(emptyList<String>(), parsed.bad)
        assertEquals(
            listOf(
                CustomRoutes.Entry.Net("192.168.50.10", 32, v6 = false),
                CustomRoutes.Entry.Net("10.20.0.0", 16, v6 = false),
                CustomRoutes.Entry.Host("gosuslugi.ru"),
                CustomRoutes.Entry.Host("work.example.ru"),
            ),
            parsed.entries.take(4),
        )
        assertTrue(parsed.entries[4] is CustomRoutes.Entry.Net && (parsed.entries[4] as CustomRoutes.Entry.Net).v6)
    }

    @Test fun aRuleThatWouldSwitchTheVpnOffEntirelyIsRefused() {
        // «0.0.0.0/0» в поле «мимо туннеля» выключил бы VPN целиком, а
        // значок в шторке продолжал бы говорить, что он работает.
        val parsed = CustomRoutes.parse("0.0.0.0/0\n1.0.0.0/4\n::/0\n300.1.1.1\nне-адрес")
        assertTrue(parsed.entries.isEmpty())
        assertEquals(5, parsed.bad.size)
    }

    @Test fun theListHasACeiling() {
        val text = (1..250).joinToString("\n") { "10.0.${it / 250}.${it % 250}" }
        val parsed = CustomRoutes.parse(text)
        assertEquals(CustomRoutes.MAX, parsed.entries.size)
        assertEquals(50, parsed.bad.size)
    }
}
