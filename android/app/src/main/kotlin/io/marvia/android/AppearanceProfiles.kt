package io.marvia.android

enum class AppearanceStyle(val key: String) { CLASSIC("classic"), SIGNAL("signal") }

/**
 * Образы телефона: «Графит» из коробки и восемь картин.
 *
 * Шесть «предметных» образов прошлого выпуска (шкала, синька, глина) не
 * прижились: телефон оформляют не под прибор, а под обои. Образцы, которые
 * присылают, — снимок во весь экран, матовые плитки и палитра в один-два
 * цвета. Так устроены и эти: у каждого своя нарисованная сцена (Scenes),
 * своя палитра (Look.presets), стеклянные карточки поверх картины (Paint) и
 * шрифт. «Графит» — тот серый, с которого Marvia начиналась, без картины.
 * Вид главной выбирается отдельно и от образа не зависит. На ПК образы свои.
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

    /** Стекло поверх картины: скругление, кнопка-стекло, мягкий свет. */
    private fun glass(preset: String, glow: String = "soft") = Look.Choice(
        preset = preset, kind = "flat", dir = "n", depth = 0.35,
        radius = "round", density = "normal", btn = "glass", glow = glow, card = "flat",
    )

    /** Графит — серый Marvia из коробки: вид по умолчанию и точечный узор. */
    val classic = Profile("graphite", R.string.vibe_graphite, R.string.vibe_graphite_sub,
        Look.Choice(), Store.PATTERN_DOTS,
        preview = Preview(rgb(0xFF3F4449), rgb(0xFF0B0C0E)))

    val all = listOf(
        classic,
        Profile("summit", R.string.vibe_summit, R.string.vibe_summit_sub, glass("summit", "none"),
            Scenes.PEAKS, preview = Preview(rgb(0xFF2A2B2E), rgb(0xFF0B0B0D))),
        Profile("sakura", R.string.vibe_sakura, R.string.vibe_sakura_sub, glass("sakura"),
            Scenes.BLOSSOM, font = "manrope", preview = Preview(rgb(0xFF3A2A31), rgb(0xFF140D11))),
        Profile("champagne", R.string.vibe_champagne, R.string.vibe_champagne_sub, glass("champagne"),
            Scenes.BOKEH, font = "manrope", preview = Preview(rgb(0xFF3A3020), rgb(0xFF15110C))),
        Profile("noir", R.string.vibe_noir, R.string.vibe_noir_sub, glass("noir", "none"),
            Scenes.SKYLINE, font = "geologica", preview = Preview(rgb(0xFF3A3B3D), rgb(0xFF101112))),
        Profile("haze", R.string.vibe_haze, R.string.vibe_haze_sub, glass("haze", "none"),
            Scenes.HAZE, font = "manrope", preview = Preview(rgb(0xFF6E6A6E), rgb(0xFF2B2A2D))),
        Profile("autumn", R.string.vibe_autumn, R.string.vibe_autumn_sub, glass("autumn"),
            Scenes.AUTUMN, font = "geologica", preview = Preview(rgb(0xFF3B2A1C), rgb(0xFF18120D))),
        Profile("bordeaux", R.string.vibe_bordeaux, R.string.vibe_bordeaux_sub, glass("bordeaux"),
            Scenes.LILIES, font = "manrope", preview = Preview(rgb(0xFF4A1B2B), rgb(0xFF17070E))),
        Profile("powder", R.string.vibe_powder, R.string.vibe_powder_sub, glass("powder", "none"),
            Scenes.POWDER, font = "manrope", preview = Preview(rgb(0xFFF4E2E2), rgb(0xFFF1EBE9))),
    )

    /**
     * Прежние образы и ближайшие к ним новые. Кто выбирал старый образ,
     * после обновления получает похожий новый: оставить его как есть значило
     * бы держать тему, которой больше нет в выборе и которую не вернуть.
     * Шкала, ставшая образом из коробки вместо «Графита», возвращается в серый.
     */
    val retired: Map<String, String> = mapOf(
        "brass" to "graphite", "blueprint" to "noir", "kiln" to "autumn",
        "soundings" to "haze", "porcelain" to "powder", "signboard" to "champagne",
        "dot" to "graphite", "terminal" to "graphite", "glass" to "noir", "chrome" to "summit",
        "aurora" to "sakura", "film" to "autumn", "expressive" to "powder", "clay" to "powder",
        "paper" to "powder", "brutal" to "powder", "day" to "powder",
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
