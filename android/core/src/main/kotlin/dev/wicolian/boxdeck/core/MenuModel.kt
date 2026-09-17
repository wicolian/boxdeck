package dev.wicolian.boxdeck.core

import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import kotlin.math.roundToInt

fun buildMenuModel(boxes: List<BoxSnapshot>): MenuModel {
    var icon = "healthy"
    val menuBoxes = boxes.map { box ->
        val title = if (box.discovered || box.tag == "tailnet") "${box.name} [tailnet]" else box.name
        if (!box.ok) {
            icon = "rust"
            return@map MenuBox(title, box.url, listOf(MenuLine(unreachableTitle(box.since))))
        }
        val health = box.health
        val lines = mutableListOf(
            MenuLine("cpu ${health.cpu.roundToInt()}% mem ${formatGb(health.memUsed)}/${formatGb(health.memTotal)} GB load ${"%.1f".format(health.load1)}"),
            MenuLine(agentTitle(box), "agents", "${box.url.trimEnd('/')}/#/agents"),
            MenuLine("ports: ${box.ports} open")
        )
        listOf("claude", "codex").forEach { provider ->
            val usage = box.usage.quota[provider]
            lines += MenuLine(quotaTitle(provider, usage), "usage", "${box.url.trimEnd('/')}/#/usage")
            if (quotaAttention(usage) && icon != "rust") icon = "attention"
        }
        if (needsYou(box) && icon != "rust") icon = "attention"
        MenuBox(title, box.url, lines)
    }
    return MenuModel(menuBoxes, icon, tooltip(boxes))
}

fun needsYou(box: BoxSnapshot): Boolean = box.state?.agents.orEmpty().any { needsYouStatus(it.effectiveStatus) }

fun needsYouStatus(status: String): Boolean = status.trim().lowercase().replace('-', '_') in setOf("needs_you", "waiting", "blocked")

fun quotaAttention(quota: Quota?): Boolean = quota?.let { (it.fiveHour?.pct ?: 0.0) > 90 || (it.sevenDay?.pct ?: 0.0) > 90 } == true

fun quotaTitle(provider: String, quota: Quota?): String {
    if (quota == null) return "$provider api key"
    val parts = mutableListOf(provider)
    quota.fiveHour?.let { parts += "5h"; parts += "${it.pct.roundToInt()}%" }
    quota.sevenDay?.let { parts += "7d"; parts += "${it.pct.roundToInt()}%" }
    return if (parts.size == 1) "$provider quota unavailable" else parts.joinToString(" ")
}

fun agentTitle(box: BoxSnapshot): String {
    val agents = box.state?.agents.orEmpty()
    val waiting = agents.count { needsYouStatus(it.effectiveStatus) }
    val working = if (agents.isEmpty()) box.agents else agents.size - waiting
    return "agents: $working working, $waiting needs you"
}

fun tooltip(boxes: List<BoxSnapshot>): String = if (boxes.isEmpty()) "boxdeck: no boxes" else boxes.joinToString(" | ") { if (!it.ok) "${it.name} down" else "${it.name} ${it.agents} agents ${it.ports} ports" }

private fun formatGb(bytes: Double): String = if (bytes <= 0) "0" else "%.1f".format(bytes / (1024 * 1024 * 1024)).trimEnd('0').trimEnd('.')

private fun unreachableTitle(since: String): String {
    if (since.isBlank()) return "unreachable"
    val parsed = runCatching { OffsetDateTime.parse(since).atZoneSameInstant(ZoneId.systemDefault()).format(DateTimeFormatter.ofPattern("HH:mm")) }.getOrNull()
    return "unreachable since ${parsed ?: since}"
}
