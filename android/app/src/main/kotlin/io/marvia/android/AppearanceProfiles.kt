package io.marvia.android

enum class AppearanceStyle(val key: String) { CLASSIC("classic"), SIGNAL("signal") }

/**
 * Образы телефона. Прежние двенадцать повторяли окно на ПК и друг друга:
 * окрашенный фон, свечение, скруглённые карточки — менялся в основном цвет.
 * Эти шесть опираются на вещи, а не на чужие системы: чертёж, обожжённая
 * керамика, карта глубин, шкала прибора, фарфор, сигнальная табличка. У
 * каждого своя палитра (Look.presets), узор, форма карточек и шрифт; вид
 * главной выбирается отдельно и от образа не зависит. На ПК образы свои.
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

    /** Мерная шкала — образ из коробки: спокойный тёмный, латунная отметка, без свечения. */
    val classic = Profile("brass", R.string.vibe_brass, R.string.vibe_brass_sub,
        Look.Choice(preset = "brass", kind = "flat", dir = "n", depth = 0.35,
            radius = "sharp", density = "compact", btn = "bare", glow = "none", card = "outline"),
        "lines",
        preview = Preview(rgb(0xFF1C2124), rgb(0xFF0D1012)))

    val all = listOf(
        classic,
        // Синька: чертёжная сетка, бирюзовая разметка на тёмно-синем.
        Profile("blueprint", R.string.vibe_blueprint, R.string.vibe_blueprint_sub,
            Look.Choice(preset = "blueprint", kind = "flat", dir = "n", depth = 0.35,
                radius = "sharp", density = "normal", btn = "ring", glow = "none", card = "outline"),
            "grid", font = "geologica",
            preview = Preview(rgb(0xFF15253B), rgb(0xFF09111D))),
        // Обожжённая глина: тёплый свет из мастерской на матовой керамике.
        Profile("kiln", R.string.vibe_kiln, R.string.vibe_kiln_sub,
            Look.Choice(preset = "kiln", kind = "radial", dir = "c", depth = 0.6,
                radius = "sharp", density = "normal", btn = "solid", glow = "soft", card = "shadow"),
            Store.PATTERN_NONE, font = "geologica",
            preview = Preview(rgb(0xFF3A2C22), rgb(0xFF140F0C), rgb(0x55F1A05D), 0)),
        // Промерная карта: изолинии глубин, морская бирюза на светлом.
        Profile("soundings", R.string.vibe_soundings, R.string.vibe_soundings_sub,
            Look.Choice(preset = "soundings", kind = "flat", dir = "n", depth = 0.35,
                radius = "round", density = "roomy", btn = "ring", glow = "soft", card = "outline"),
            "rings", font = "manrope",
            preview = Preview(rgb(0xFFEFF8F7), rgb(0xFFDCEBEA))),
        // Белый фарфор: холодный белый, крупные скругления, синяя глазурь.
        Profile("porcelain", R.string.vibe_porcelain, R.string.vibe_porcelain_sub,
            Look.Choice(preset = "porcelain", kind = "flat", dir = "n", depth = 0.35,
                radius = "pill", density = "roomy", btn = "glass", glow = "none", card = "shadow"),
            Store.PATTERN_NONE, font = "manrope",
            preview = Preview(rgb(0xFFF6F8FB), rgb(0xFFE4E8EF))),
        // Сигнальный щит: охристая табличка, прямые углы, полосы.
        Profile("signboard", R.string.vibe_signboard, R.string.vibe_signboard_sub,
            Look.Choice(preset = "signboard", kind = "flat", dir = "n", depth = 0.35,
                radius = "sharp", density = "compact", btn = "solid", glow = "none", card = "outline"),
            "lines",
            preview = Preview(rgb(0xFFFBF6E0), rgb(0xFFEFE7C6))),
    )

    /**
     * Прежние образы и ближайшие к ним новые. Кто выбирал старый образ,
     * после обновления получает похожий новый: оставить его как есть значило
     * бы держать тему, которой больше нет в выборе и которую не вернуть.
     */
    val retired: Map<String, String> = mapOf(
        "graphite" to "brass", "dot" to "brass", "terminal" to "brass",
        "glass" to "blueprint", "chrome" to "blueprint", "aurora" to "blueprint",
        "expressive" to "porcelain", "clay" to "porcelain",
        "paper" to "signboard", "brutal" to "signboard",
        "film" to "kiln", "day" to "soundings",
    )

    /**
     * migrate — один раз после обновления: прежний образ меняется на
     * ближайший новый, а свежая установка получает образ из коробки. Свою
     * тему, собранную руками без образа, не трогаем.
     */
    fun migrate(store: Store) {
        val next = retired[store.vibe]
        when {
            next != null -> apply(store, all.first { it.id == next })
            store.vibe.isEmpty() && !store.hasLook() -> apply(store, classic)
        }
    }

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
     * или кнопка — это подстройка образа, а другая палитра — уже другая
     * тема. Установки до появления образов узнаются по точному совпадению.
     */
    fun detail(store: Store): String = detail(store.vibe, store.look) ?: selected(store)?.id.orEmpty()

    fun detail(saved: String, look: Look.Choice): String? =
        all.firstOrNull { it.id == saved && it.choice.preset == look.preset }?.id
}
