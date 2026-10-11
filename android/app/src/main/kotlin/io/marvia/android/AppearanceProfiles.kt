package io.marvia.android

enum class AppearanceStyle(val key: String) { CLASSIC("classic"), SIGNAL("signal") }

/**
 * Образы — те же пятнадцать, что в окне на компьютере (VIBES в app.html), с
 * теми же именами, палитрами и обоями: человек, сменивший ПК на телефон,
 * узнаёт своё оформление. Наши три — «Графит», «Точка», «Бумага» — живут на
 * общей теме; двенадцать в духе Windows, macOS и Linux приносят ещё свою
 * палитру, обои, оболочку и шрифт. Сохранённые настройки не заменяются.
 */
object AppearanceProfiles {
    /**
     * Палитра образа в духе системы — поля o с ПК: фон, карточки, текст,
     * подписи, обводка и скругление. Общая тема выводит цвета из пресета, а
     * у Windows 11 серый — свой, точный, и вывести его не из чего.
     *
     * desk — текст прямо на рабочем столе, если он другой, чем в карточках:
     * у «Windows 98» серые окна с чёрным текстом стоят на бирюзе, и подпись
     * на ней чёрным не читается — на ПК она белая, и здесь.
     */
    data class Palette(val bg: Int, val surf: Int, val fg: Int, val dim: Int, val line: Int, val r: Int, val desk: Int = 0)

    data class Profile(
        val id: String, val name: Int, val description: Int, val choice: Look.Choice, val pattern: String,
        val style: AppearanceStyle = AppearanceStyle.CLASSIC, val font: String = Store.FONT_ONEST,
        /**
         * Оболочка — то, по чему узнают саму систему: слюда Windows 11, синяя
         * панель XP, выпуклые окна 98-й, светофор macOS, карточки GNOME,
         * голубой фокус Breeze, плитки с цветной рамкой. Наши — «marvia».
         */
        val shell: String = MARVIA,
        val palette: Palette? = null,
        /** Обои и пелена над ними; null — фон общей темы. */
        val scene: Wallpaper.Scene? = null,
        /** Цветная рамка активной плитки у плиточных: от и до. */
        val tile: IntArray? = null,
    )

    const val MARVIA = "marvia"

    private fun rgb(v: Long) = v.toInt()

    /**
     * Пелена сверху вниз цветом и прозрачностью. На ПК образ системы — окно на
     * столе, обои рамкой вокруг, и его veil стал заливкой окна; у телефона
     * стола вокруг нет, экран и есть окно. Поэтому пелена здесь своя, лёгкая
     * — прежние значения ПК, при которых обои видны сквозь приложение.
     */
    private fun veil(hex: Int, vararg alphas: Double) = IntArray(alphas.size) { (Math.round(alphas[it] * 255).toInt() shl 24) or (hex and 0xFFFFFF) }

    /** Образ с палитрой системы: свет ровный, узора нет — фон дают обои. */
    private fun system(
        id: String, name: Int, sub: Int, shell: String, preset: String, accent: Long,
        radius: String, density: String, btn: String, glow: String, card: String,
        font: String, palette: Palette, scene: Wallpaper.Scene, tile: IntArray? = null,
    ) = Profile(id, name, sub,
        Look.Choice(preset = preset, accent = rgb(accent), kind = "flat", radius = radius, density = density, btn = btn, glow = glow, card = card),
        Store.PATTERN_NONE, font = font, shell = shell, palette = palette, scene = scene, tile = tile)

    val classic = Profile("graphite", R.string.vibe_graphite, R.string.vibe_graphite_sub, Look.Choice(), Store.PATTERN_DOTS)

    val all = listOf(
        classic,
        // Точечное табло: та самая композиция с точечной надписью и кольцом.
        Profile("dot", R.string.vibe_dot, R.string.vibe_dot_sub,
            Look.Choice(preset = "oled", accent = rgb(0xFFFF3B30), kind = "flat", dir = "n", depth = 0.35,
                radius = "soft", density = "normal", btn = "ring", glow = "none", card = "outline"),
            Store.PATTERN_DOTS, AppearanceStyle.SIGNAL),
        Profile("paper", R.string.vibe_paper, R.string.vibe_paper_sub,
            Look.Choice(preset = "paper", accent = rgb(0xFFD6362A), kind = "flat", dir = "n", depth = 0.35,
                radius = "soft", density = "roomy", btn = "ring", glow = "none", card = "outline"),
            Store.PATTERN_DOTS, AppearanceStyle.SIGNAL),
        // Windows 11: слюда над обоями, скругление 8, синий акцент Fluent
        // (светлый вариант — для тёмной темы).
        system("win11", R.string.vibe_win11, R.string.vibe_win11_sub, "win11", "slate", 0xFF60CDFF,
            "soft", "normal", "solid", "none", "outline", Store.FONT_SYSTEM,
            Palette(rgb(0xFF202020), rgb(0xFF2D2D2D), rgb(0xFFFFFFFF), rgb(0xFFC8C8C8), rgb(0xFF3D3D3D), 8),
            Wallpaper.Scene("bloom", veil(0x202020, .74, .74))),
        system("win11l", R.string.vibe_win11l, R.string.vibe_win11l_sub, "win11", "daylight", 0xFF005FB8,
            "soft", "normal", "solid", "none", "outline", Store.FONT_SYSTEM,
            Palette(rgb(0xFFF3F3F3), rgb(0xFFFBFBFB), rgb(0xFF1B1B1B), rgb(0xFF5D5D5D), rgb(0xFFE2E2E2), 8),
            Wallpaper.Scene("bloomLight", veil(0xF3F3F3, .70, .70))),
        // Windows XP: бежевые окна, синяя панель задач и холм под небом.
        system("xp", R.string.vibe_xp, R.string.vibe_xp_sub, "xp", "daylight", 0xFF316AC5,
            "sharp", "normal", "solid", "none", "outline", Store.FONT_SYSTEM,
            Palette(rgb(0xFFECE9D8), rgb(0xFFFFFFFF), rgb(0xFF0B1530), rgb(0xFF4A5162), rgb(0xFFA5B6D6), 4),
            Wallpaper.Scene("hill", veil(0xECE9D8, .30, .62, .30), floatArrayOf(0f, .55f, 1f))),
        // Windows 98: бирюзовый стол и серые выпуклые окна. Обоев у неё не
        // было — был цвет стола, он и есть обои.
        system("w98", R.string.vibe_w98, R.string.vibe_w98_sub, "w98", "daylight", 0xFF000080,
            "sharp", "compact", "solid", "none", "outline", Store.FONT_SYSTEM,
            Palette(rgb(0xFF008080), rgb(0xFFC0C0C0), rgb(0xFF000000), rgb(0xFF3A3A3A), rgb(0xFF808080), 0, desk = rgb(0xFFFFFFFF)),
            Wallpaper.Scene("teal")),
        // macOS: светофор, жидкое стекло, Inter вместо SF — системный шрифт
        // Apple встраивать нельзя.
        system("mac", R.string.vibe_mac, R.string.vibe_mac_sub, "mac", "midnight", 0xFF0A84FF,
            "round", "roomy", "glass", "soft", "flat", Store.FONT_INTER,
            Palette(rgb(0xFF1C1C1E), rgb(0xFF2C2C2E), rgb(0xFFF5F5F7), rgb(0xFFAEAEB2), rgb(0xFF3A3A3C), 16),
            Wallpaper.Scene("waves", veil(0x0A0A14, .30, .48))),
        system("macl", R.string.vibe_macl, R.string.vibe_macl_sub, "mac", "daylight", 0xFF007AFF,
            "round", "roomy", "glass", "none", "flat", Store.FONT_INTER,
            Palette(rgb(0xFFF5F5F7), rgb(0xFFFFFFFF), rgb(0xFF1D1D1F), rgb(0xFF5E5E63), rgb(0xFFD2D2D7), 16),
            Wallpaper.Scene("wavesLight", veil(0xFFFFFF, .22, .40))),
        // Ubuntu: Yaru — баклажан, оранжевый, свой шрифт.
        system("ubuntu", R.string.vibe_ubuntu, R.string.vibe_ubuntu_sub, "ubuntu", "ember", 0xFFE95420,
            "soft", "normal", "solid", "soft", "outline", Store.FONT_UBUNTU,
            Palette(rgb(0xFF2C2C2C), rgb(0xFF373737), rgb(0xFFFFFFFF), rgb(0xFFC6C4C1), rgb(0xFF4A4A4A), 10),
            Wallpaper.Scene("aubergine", veil(0x1C161A, .50, .70))),
        // GNOME: Adwaita светлая — карточки-списки, синий, Inter (на нём и
        // построен нынешний Adwaita Sans).
        system("gnome", R.string.vibe_gnome, R.string.vibe_gnome_sub, "gnome", "daylight", 0xFF3584E4,
            "soft", "roomy", "solid", "none", "flat", Store.FONT_INTER,
            Palette(rgb(0xFFFAFAFB), rgb(0xFFFFFFFF), rgb(0xFF2E2E32), rgb(0xFF5E5E66), rgb(0xFFE1E1E4), 12),
            Wallpaper.Scene("blobs", veil(0xFAFAFB, .66, .80))),
        // KDE Plasma: Breeze тёмная — голубой фокус, острые грани, Noto Sans.
        system("kde", R.string.vibe_kde, R.string.vibe_kde_sub, "kde", "ice", 0xFF3DAEE9,
            "sharp", "normal", "ring", "soft", "outline", Store.FONT_NOTO,
            Palette(rgb(0xFF202326), rgb(0xFF292C30), rgb(0xFFFCFCFC), rgb(0xFFA7AEB6), rgb(0xFF3F454C), 5),
            Wallpaper.Scene("shards", veil(0x141618, .45, .62))),
        // Hyprland с Catppuccin Mocha — плитки с зазорами, цветная рамка
        // активного окна, моно в баре. Пелена — та, что на ПК даёт плитка
        // с содержимым: экран телефона и есть эта плитка.
        system("hypr", R.string.vibe_hypr, R.string.vibe_hypr_sub, TILE, "plum", 0xFFCBA6F7,
            "soft", "normal", "ring", "soft", "flat", Store.FONT_INTER,
            Palette(rgb(0xFF1E1E2E), rgb(0xFF313244), rgb(0xFFCDD6F4), rgb(0xFFA6ADC8), rgb(0xFF45475A), 10),
            Wallpaper.Scene("peaks", veil(0x1E1E2E, .50, .50)), intArrayOf(rgb(0xFF89B4FA), rgb(0xFFCBA6F7))),
        system("nord", R.string.vibe_nord, R.string.vibe_nord_sub, TILE, "ice", 0xFF88C0D0,
            "soft", "normal", "ring", "none", "flat", Store.FONT_NOTO,
            Palette(rgb(0xFF2E3440), rgb(0xFF3B4252), rgb(0xFFECEFF4), rgb(0xFFB4BDCC), rgb(0xFF4C566A), 8),
            Wallpaper.Scene("arctic", veil(0x2E3440, .50, .50)), intArrayOf(rgb(0xFF8FBCBB), rgb(0xFF81A1C1))),
        system("gruvbox", R.string.vibe_gruvbox, R.string.vibe_gruvbox_sub, TILE, "amber", 0xFFFE8019,
            "sharp", "normal", "ring", "soft", "flat", Store.FONT_MONO,
            Palette(rgb(0xFF282828), rgb(0xFF3C3836), rgb(0xFFEBDBB2), rgb(0xFFBDAE93), rgb(0xFF504945), 4),
            Wallpaper.Scene("sunset", veil(0x282828, .50, .50)), intArrayOf(rgb(0xFFFABD2F), rgb(0xFFFE8019))),
    )

    const val TILE = "tile"

    fun byId(id: String): Profile? = all.firstOrNull { it.id == id }

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
     * или кнопка — это подстройка «Windows XP», а другая палитра — уже
     * другая тема, и бирюзовый стол «Windows 98» под ней был бы чужим.
     * Установки до появления образов узнаются по точному совпадению.
     */
    fun detail(store: Store): String = detail(store.vibe, store.look) ?: selected(store)?.id.orEmpty()

    fun detail(saved: String, look: Look.Choice): String? =
        all.firstOrNull { it.id == saved && it.choice.preset == look.preset }?.id

    // ------------------------------------------------------------ палитра

    fun theme(store: Store): Theme = theme(store.look, detail(store))

    /**
     * theme — тема с палитрой образа поверх общей, как overrideCss на ПК.
     *
     * Поле образа держится, пока человек не выбрал своё: другое скругление в
     * «Форме» — его скругление, а не восьмёрка Windows 11; другой свет — фон
     * светится от палитры образа, а не от пресета под ней. Иначе ручки
     * редактора на этих образах ничего бы не меняли, а нарисованная ручка —
     * обещание. Тёмность — от пресета, как на ПК: по ней системные окна
     * выбирают свои цвета, и серые окна 98-й на бирюзе — светлая тема.
     */
    fun theme(look: Look.Choice, vibe: String): Theme {
        val base = Look.theme(look)
        val c = Look.normalize(look)
        val profile = byId(vibe)?.takeIf { it.choice.preset == c.preset } ?: return base
        val p = profile.palette ?: return base
        val (lit, mid, shade) = Look.light(p.bg, base.tint, c.kind, c.depth)
        val surf2 = Look.mix(p.surf, p.fg, 0.08)
        val desk = if (p.desk != 0) emptyList() else listOf(p.bg)
        return base.copy(
            bg = p.bg, lit = lit, mid = mid, shade = shade,
            surf = p.surf, surf2 = surf2, line = p.line, fg = p.fg,
            dim = readable(p.dim, p.fg, desk + listOf(p.surf, surf2)),
            r = if (c.radius == profile.choice.radius) p.r else base.r,
        )
    }

    /**
     * readable — подпись образа, если она читается на всех своих фонах, а
     * иначе на шаг ближе к тексту. Подписи Nord и Gruvbox на ПК лежат на
     * карточках, а на телефоне — ещё и на чипах второго тона, где им не
     * хватало до 4,5:1. Сдвиг минимальный: цвет остаётся их.
     */
    private fun readable(dim: Int, fg: Int, grounds: List<Int>): Int {
        var k = 0.0
        var out = dim
        while (k < 1.0 && grounds.any { Look.ratio(out, it) < 4.5 }) {
            k += 0.05
            out = Look.mix(dim, fg, k)
        }
        return out
    }

    /**
     * scene — обои под приложением. Они — фон образа, пока свет не выбран
     * другой: выбор света в редакторе значит «хочу свой фон», и обои
     * уступают ему место, как уступают своему фото.
     */
    fun scene(store: Store): Wallpaper.Scene? {
        val profile = byId(detail(store)) ?: return null
        return profile.scene?.takeIf { Look.normalize(store.look).kind == profile.choice.kind }
    }

    // ----------------------------------------------------------- переезд

    /**
     * Прежние образы — на ближайшие новые, как VIBE_RENAMED на ПК: выбор,
     * сохранённый до обновления, не должен молча превратиться в «Графит».
     */
    val renamed = mapOf(
        "glass" to "mac", "expressive" to "gnome", "chrome" to "graphite", "brutal" to "w98", "clay" to "macl",
        "film" to "gruvbox", "aurora" to "hypr", "terminal" to "gruvbox", "day" to "win11l",
    )

    /** Палитра, на которой держался прежний образ: по ней видно, был ли он ещё в силе. */
    private val legacyPreset = mapOf(
        "glass" to "midnight", "expressive" to "daylight", "chrome" to "steel", "brutal" to "linen", "clay" to "linen",
        "film" to "sand", "aurora" to "plum", "terminal" to "emerald", "day" to "daylight",
    )

    /**
     * migrate переводит прежний образ на новый целиком, как ПК: палитра и
     * форма прежнего к новому не подходят. Если прежний уже был снят другой
     * палитрой — это выбор человека, его не трогаем и только забываем
     * прежнее имя: иначе оно случайно совпало бы с новым образом на той же
     * палитре.
     */
    fun migrate(store: Store) {
        val old = store.vibe
        val id = renamed[old] ?: return
        val next = byId(id)
        if (next != null && store.look.preset == legacyPreset[old]) apply(store, next) else store.vibe = ""
    }
}
