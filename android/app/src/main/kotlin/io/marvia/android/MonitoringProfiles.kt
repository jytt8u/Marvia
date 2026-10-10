package io.marvia.android

/** Частота измерений не меняет восстановление связи или маршруты. */
object MonitoringProfiles {
    data class Profile(val label: Int, val live: Int, val idle: Int, val ping: Int)

    val all = listOf(
        Profile(R.string.advanced_preset_battery, 10, 60, 60),
        Profile(R.string.advanced_preset_balanced, 2, 30, 30),
        Profile(R.string.advanced_preset_detailed, 2, 10, 10),
    )

    fun selected(store: Store): Int = all.indexOfFirst {
        it.live == store.liveRefreshSeconds && it.idle == store.idleRefreshSeconds && it.ping == store.pingIntervalSeconds
    }

    fun apply(store: Store, index: Int) {
        val profile = all[index]
        store.liveRefreshSeconds = profile.live
        store.idleRefreshSeconds = profile.idle
        store.pingIntervalSeconds = profile.ping
    }
}
