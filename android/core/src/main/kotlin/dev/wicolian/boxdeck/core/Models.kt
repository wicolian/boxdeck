package dev.wicolian.boxdeck.core

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject

@Serializable
data class Health(
    val cpu: Double = 0.0,
    val cores: Int = 0,
    val load1: Double = 0.0,
    val load5: Double = 0.0,
    val load15: Double = 0.0,
    val memTotal: Double = 0.0,
    val memUsed: Double = 0.0,
    val memAvail: Double = 0.0,
    val swapTotal: Double = 0.0,
    val swapUsed: Double = 0.0,
    val swapFree: Double = 0.0,
    val diskPct: Double = 0.0,
    val host: String = ""
)

@Serializable
data class Agent(
    val kind: String = "",
    @SerialName("agent_status") val agentStatus: String = "",
    val status: String = "",
    val pane: String = "",
    @SerialName("pane_id") val paneId: String = "",
    val title: String = "",
    val model: String = "",
    val folder: String = "",
    val tail: String = ""
) {
    val effectiveStatus: String get() = status.ifBlank { agentStatus }
}

@Serializable
data class Port(
    val port: Int = 0,
    val addr: String = "",
    val proc: String = "",
    val pid: Int = 0,
    val cwd: String = "",
    val label: String = "",
    val title: String = "",
    val url: String = ""
)

@Serializable
data class ProcessInfo(
    val pid: Int = 0,
    val ppid: Int = 0,
    val secs: Int = 0,
    val cpu: Double = 0.0,
    val rss: Long = 0,
    val args: String = ""
)

@Serializable
data class State(
    val host: String = "",
    val health: Health = Health(),
    val agents: List<Agent> = emptyList(),
    val ports: List<Port> = emptyList(),
    val tmux: List<JsonObject> = emptyList(),
    val reports: List<JsonObject> = emptyList()
)

@Serializable
data class TokenTotals(
    val `in`: Long = 0,
    val cachedIn: Long = 0,
    val cacheWrite: Long = 0,
    val out: Long = 0
) {
    val total: Long get() = `in` + cachedIn + cacheWrite + out
}

@Serializable
data class UsageSummary(
    val tokens: TokenTotals = TokenTotals(),
    val costUsd: Double = 0.0
)

@Serializable
data class QuotaWindow(
    val pct: Double = 0.0,
    val resetsAt: String = ""
)

@Serializable
data class Quota(
    val fiveHour: QuotaWindow? = null,
    val sevenDay: QuotaWindow? = null
)

@Serializable
data class UsageModel(
    val tokens: TokenTotals = TokenTotals(),
    val costUsd: Double = 0.0,
    val priceSet: Boolean = false
)

@Serializable
data class UsageDay(
    val day: String = "",
    val tokens: TokenTotals = TokenTotals(),
    val costUsd: Double = 0.0,
    val models: Map<String, UsageModel> = emptyMap()
)

@Serializable
data class UsageProvider(
    val quota: Quota? = null,
    val today: UsageSummary = UsageSummary(),
    val localDay: String = "",
    val days: List<UsageDay> = emptyList(),
    val sessions: Int = 0
)

@Serializable
data class UsageResponse(
    val providers: Map<String, UsageProvider> = emptyMap(),
    val device: String = "",
    val updatedAt: String = ""
)

@Serializable
data class BoxUsage(
    val today: Map<String, UsageSummary> = emptyMap(),
    val quota: Map<String, Quota?> = emptyMap()
)

@Serializable
data class BoxSnapshot(
    val name: String = "",
    val url: String = "",
    val local: Boolean = false,
    val ok: Boolean = false,
    val since: String = "",
    val discovered: Boolean = false,
    val tag: String = "",
    val health: Health = Health(),
    val agents: Int = 0,
    val ports: Int = 0,
    val usage: BoxUsage = BoxUsage(),
    val state: State? = null
)

@Serializable
data class UsageAllResponse(
    val providers: Map<String, UsageProvider> = emptyMap(),
    val boxes: List<UsageAllBox> = emptyList(),
    val updatedAt: String = ""
)

@Serializable
data class UsageAllBox(
    val name: String = "",
    val url: String = "",
    val local: Boolean = false,
    val ok: Boolean = false,
    val discovered: Boolean = false,
    val providers: Map<String, UsageProvider> = emptyMap(),
    val error: String = ""
)

@Serializable
data class AppView(
    val id: String = "",
    val name: String = "",
    val tag: String = "",
    val status: String = "",
    val detected: Boolean = false,
    val running: Boolean = false,
    val pid: Int = 0,
    val port: Int = 0,
    val url: String = "",
    val health: Boolean = false,
    val log: List<String> = emptyList(),
    val installHint: String = "",
    val installURL: String = "",
    val docs: String = "",
    val openPath: String = "",
    val embed: Boolean = false,
    val canStart: Boolean = false,
    val canStop: Boolean = false,
    val message: String = "",
    val exitCode: Int? = null
)

@Serializable
data class TailnetPeer(
    val name: String = "",
    val dnsName: String = "",
    val os: String = "",
    val online: Boolean = false,
    val ips: List<String> = emptyList(),
    val url: String = "",
    val boxdeck: Boolean = false,
    val version: String = "",
    val lastSeen: String = "",
    val message: String = ""
)

data class FleetDevice(
    val name: String,
    val url: String = "",
    val deck: BoxSnapshot? = null,
    val peer: TailnetPeer? = null
) {
    val isDeck: Boolean get() = deck != null
    val online: Boolean get() = deck?.ok ?: (peer?.online == true)
    val os: String get() = peer?.os.orEmpty()
    val lastSeen: String get() = peer?.lastSeen.orEmpty()
}

@Serializable
data class AlertAction(
    val label: String = "",
    val method: String = "POST",
    val path: String = "",
    val body: JsonObject = buildJsonObject { }
)

@Serializable
data class Alert(
    val id: String = "",
    val box: String = "",
    val rule: String = "",
    val severity: String = "info",
    val title: String = "",
    val body: String = "",
    val at: String = "",
    val state: String = "open",
    val link: String = "",
    val actions: List<AlertAction> = emptyList(),
    val count: Int = 1,
    val snoozedUntil: String = ""
)

@Serializable
data class AlertPage(
    val alerts: List<Alert> = emptyList(),
    val disarmed: Boolean = false
)

@Serializable
data class AlertRule(
    val id: String = "",
    val enabled: Boolean = true,
    val threshold: Double? = null,
    val minutes: Int? = null
)

@Serializable
data class HerdRead(
    val pane: String = "",
    val text: String = "",
    val lines: Int = 0,
    val source: String = "",
    val needsYou: Boolean = false
)

@Serializable
data class HealthResponse(
    val ok: Boolean = false,
    val version: String = ""
)

@Serializable
data class ServerEvent(
    val id: String = "",
    val type: String = "",
    val data: JsonObject = buildJsonObject { }
)

data class MenuLine(val title: String, val action: String = "", val url: String = "")
data class MenuBox(val title: String, val url: String, val lines: List<MenuLine>)
data class MenuModel(
    val boxes: List<MenuBox>,
    val iconState: String,
    val tooltip: String,
    val generatedAt: Long = System.currentTimeMillis()
)

class BoxdeckHttpException(val status: Int, val endpoint: String, message: String) : RuntimeException(message)
