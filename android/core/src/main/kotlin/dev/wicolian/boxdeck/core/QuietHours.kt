package dev.wicolian.boxdeck.core

import java.time.LocalTime

object QuietHours {
    fun isQuiet(now: LocalTime, from: String, to: String): Boolean {
        val start = runCatching { LocalTime.parse(from) }.getOrNull() ?: return false
        val end = runCatching { LocalTime.parse(to) }.getOrNull() ?: return false
        if (start == end) return true
        return if (start < end) now >= start && now < end else now >= start || now < end
    }
}
