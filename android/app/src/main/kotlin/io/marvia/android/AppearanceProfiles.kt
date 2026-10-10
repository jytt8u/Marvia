package io.marvia.android

enum class AppearanceStyle(val key: String) { CLASSIC("classic"), SIGNAL("signal") }

/**
 * Образы — те же двенадцать, что в окне на компьютере, с теми же именами и
 * характером: человек, сменивший телефон на ПК, узнаёт своё оформление.
 * Каждый задаёт палитру и форму общей темы, узор фона, шрифт и композицию
 * главной (обычная или точечная). Сохранённые настройки не заменяются.
 */
object AppearanceProfiles {
    /** Цвета миниатюры: фон сверху и снизу и два пятна света поверх. */
    data class Preview(val top: Int, val bottom: Int, val blobA: Int = 0, val blobB: Int = 0)

    data class Profile(
        val id: String, val name: Int, val description: Int, val choice: Look.Choice, val pattern: String,
        val style: AppearanceStyle = AppearanceStyle.CLASSIC, val font: String = Store.FONT_ONEST,
        val preview: Preview,
    )

    private fun rgb(v: Long) = v.toInt()

    val classic = Profile("graphite", R.string.vibe_graphite, R.string.vibe_graphite_sub,
        Look.Choice(), Store.PATTERN_DOTS,
        preview = Preview(rgb(0xFF3F4449), rgb(0xFF0B0C0E)))

    val all = listOf(
        classic,
        // В духе «жидкого стекла»: яркое под прозрачными панелями.
        Profile("glass", R.string.vibe_glass, R.string.vibe_glass_sub,
            Look.Choice(preset = "midnight", accent = rgb(0xFF5AA9FF), kind = "aurora", dir = "ne", depth = 0.7,
                tint = rgb(0xFFFF8A3D), radius = "pill", density = "roomy", btn = "glass", glow = "mid", card = "flat"),
            Store.PATTERN_NONE, font = "manrope",
            preview = Preview(rgb(0xFF0A0E1C), rgb(0xFF070A14), rgb(0xFF3B82FF), rgb(0xFFFF8A3D))),
        // В духе Material 3 Expressive: смелый цвет и тональные карточки.
        Profile("expressive", R.string.vibe_expressive, R.string.vibe_expressive_sub,
            Look.Choice(preset = "daylight", accent = rgb(0xFF6D4AFF), kind = "linear", dir = "nw", depth = 0.3,
                tint = rgb(0xFFFFB4A2), radius = "pill", density = "roomy", btn = "solid", glow = "soft", card = "flat"),
            "grid", font = "manrope",
            preview = Preview(rgb(0xFFEFEAFF), rgb(0xFFFFE4DC), rgb(0x556D4AFF), rgb(0x55FF8C6E))),
        // Точечное табло: та самая композиция с точечной надписью и кольцом.
        Profile("dot", R.string.vibe_dot, R.string.vibe_dot_sub,
            Look.Choice(preset = "oled", accent = rgb(0xFFFF3B30), kind = "flat", dir = "n", depth = 0.35,
                radius = "soft", density = "normal", btn = "ring", glow = "none", card = "outline"),
            Store.PATTERN_DOTS, AppearanceStyle.SIGNAL,
            preview = Preview(rgb(0xFF000000), rgb(0xFF000000))),
        Profile("paper", R.string.vibe_paper, R.string.vibe_paper_sub,
            Look.Choice(preset = "paper", accent = rgb(0xFFD6362A), kind = "flat", dir = "n", depth = 0.35,
                radius = "soft", density = "roomy", btn = "ring", glow = "none", card = "outline"),
            Store.PATTERN_DOTS, AppearanceStyle.SIGNAL,
            preview = Preview(rgb(0xFFF6F3EC), rgb(0xFFEFEADF))),
        // Жидкий металл и голография.
        Profile("chrome", R.string.vibe_chrome, R.string.vibe_chrome_sub,
            Look.Choice(preset = "steel", accent = rgb(0xFFDFE7F0), kind = "radial", dir = "n", depth = 0.85,
                tint = rgb(0xFFA78BFA), radius = "round", density = "normal", btn = "solid", glow = "mid", card = "outline"),
            "rings", font = "geologica",
            preview = Preview(rgb(0xFF3A3F47), rgb(0xFF0B0C10), rgb(0x66A78BFA), rgb(0x555EEAD4))),
        // Необрутализм: рамки, жёсткие тени, острые углы.
        Profile("brutal", R.string.vibe_brutal, R.string.vibe_brutal_sub,
            Look.Choice(preset = "linen", accent = rgb(0xFFFF5A1F), kind = "flat", dir = "n", depth = 0.35,
                radius = "sharp", density = "normal", btn = "solid", glow = "none", card = "outline"),
            "grid", font = "geologica",
            preview = Preview(rgb(0xFFF3EEDF), rgb(0xFFEDE6D2))),
        // Пластилин: мягкий объём в пастели.
        Profile("clay", R.string.vibe_clay, R.string.vibe_clay_sub,
            Look.Choice(preset = "linen", accent = rgb(0xFFFF6FA8), kind = "linear", dir = "nw", depth = 0.3,
                tint = rgb(0xFF9AD0FF), radius = "pill", density = "roomy", btn = "solid", glow = "soft", card = "flat"),
            Store.PATTERN_NONE, font = "manrope",
            preview = Preview(rgb(0xFFFFE3EF), rgb(0xFFDFF0FF), rgb(0x66FFFFFF), 0)),
        // Плёнка: тёплый полумрак и неспешность.
        Profile("film", R.string.vibe_film, R.string.vibe_film_sub,
            Look.Choice(preset = "sand", accent = rgb(0xFFE8A54B), kind = "radial", dir = "c", depth = 0.78,
                radius = "soft", density = "roomy", btn = "ring", glow = "soft", card = "flat"),
            Store.PATTERN_NONE, font = "geologica",
            preview = Preview(rgb(0xFF3D2F22), rgb(0xFF120D08))),
        Profile("aurora", R.string.vibe_aurora, R.string.vibe_aurora_sub,
            Look.Choice(preset = "plum", accent = rgb(0xFFA98BFF), kind = "aurora", dir = "ne", depth = 0.8,
                radius = "round", density = "roomy", btn = "glass", glow = "mid", card = "flat"),
            Store.PATTERN_NONE, font = "manrope",
            preview = Preview(rgb(0xFF140B24), rgb(0xFF0B0714), rgb(0x99A98BFF), rgb(0x6640C8FF))),
        Profile("terminal", R.string.vibe_terminal, R.string.vibe_terminal_sub,
            Look.Choice(preset = "emerald", accent = rgb(0xFF3DFF8F), kind = "radial", dir = "c", depth = 0.85,
                radius = "sharp", density = "compact", btn = "bare", glow = "mid", card = "outline"),
            "lines",
            preview = Preview(rgb(0xFF0B2A1C), rgb(0xFF030A07))),
        // День: светлый и спокойный, без узора.
        Profile("day", R.string.vibe_day, R.string.vibe_day_sub,
            Look.Choice(preset = "daylight", accent = rgb(0xFF0F766E), kind = "flat", dir = "n", depth = 0.35,
                radius = "round", density = "roomy", btn = "ring", glow = "none", card = "outline"),
            Store.PATTERN_NONE,
            preview = Preview(rgb(0xFFF7F9FA), rgb(0xFFE9EEF0))),
    )

    fun selected(store: Store): Profile? = all.firstOrNull {
        Look.encode(store.look) == Look.encode(it.choice) && store.pattern == it.pattern &&
            store.appearanceStyle == it.style && store.font == it.font
    }

    fun apply(store: Store, profile: Profile) {
        store.look = profile.choice
        store.pattern = profile.pattern
        store.appearanceStyle = profile.style
        store.font = profile.font
        store.vibe = profile.id
    }

    /**
     * Чьи детали рисовать. Образ держится, пока основа та же: другой акцент
     * или кнопка — это подстройка «Хрома», а другая палитра — уже другая
     * тема, и чёрные тени «Брутала» на ней были бы чужими. Установки до
     * появления образов узнаются по точному совпадению с образом.
     */
    fun detail(store: Store): String = detail(store.vibe, store.look) ?: selected(store)?.id.orEmpty()

    fun detail(saved: String, look: Look.Choice): String? =
        all.firstOrNull { it.id == saved && it.choice.preset == look.preset }?.id
}
