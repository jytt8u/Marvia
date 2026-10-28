package io.marvia.android

import org.json.JSONObject

/** Нулевой лимит означает безлимит только в полученных данных подписки. */
data class AccountQuota(val limit: Long, val left: Long, val until: String) {
    companion object {
        fun read(json: String): AccountQuota? {
            val value = runCatching { JSONObject(json) }.getOrNull() ?: return null
            val limit = (value.opt("limit") as? Number)?.toLong()?.takeIf { it >= 0 } ?: return null
            val left = (value.opt("left") as? Number)?.toLong()?.coerceAtLeast(0) ?: 0
            return AccountQuota(limit, left, value.optString("until", ""))
        }
    }
}
