package dev.wicolian.boxdeck

import android.content.Context
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import dev.wicolian.boxdeck.core.Alert
import dev.wicolian.boxdeck.core.AlertAction
import dev.wicolian.boxdeck.core.Agent
import dev.wicolian.boxdeck.core.AppView
import dev.wicolian.boxdeck.core.BoxSnapshot
import dev.wicolian.boxdeck.core.BoxUsage
import dev.wicolian.boxdeck.core.BoxdeckClient
import dev.wicolian.boxdeck.core.FleetDevice
import dev.wicolian.boxdeck.core.Health
import dev.wicolian.boxdeck.core.Port
import dev.wicolian.boxdeck.core.ServerEvent
import dev.wicolian.boxdeck.core.State
import dev.wicolian.boxdeck.core.TailnetPeer
import dev.wicolian.boxdeck.core.UsageResponse
import dev.wicolian.boxdeck.core.needsYouStatus
import dev.wicolian.boxdeck.notifications.AlertNotifications
import kotlinx.coroutines.CloseableCoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.supervisorScope
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import java.io.Closeable

enum class PhoneTab(val label: String) {
    NEEDS("Needs you"), BOXES("Boxes"), USAGE("Usage"), SETTINGS("Settings")
}

data class AlertItem(val alert: Alert, val boxUrl: String)

data class DetailState(
    val loading: Boolean = false,
    val state: State = State(),
    val ports: List<Port> = emptyList(),
    val agents: List<Agent> = emptyList(),
    val tails: Map<String, String> = emptyMap(),
    val usage: UsageResponse = UsageResponse(),
    val apps: List<AppView> = emptyList(),
    val error: String = ""
)

data class PhoneUiState(
    val tab: PhoneTab = PhoneTab.NEEDS,
    val fleet: List<FleetDevice> = emptyList(),
    val alerts: List<AlertItem> = emptyList(),
    val usage: UsageResponse = UsageResponse(),
    val detail: DetailState = DetailState(),
    val selectedDevice: FleetDevice? = null,
    val refreshing: Boolean = false,
    val error: String = "",
    val notifications: Boolean = false,
    val quietFrom: String = "23:00",
    val quietTo: String = "08:00",
    val disarmed: Boolean = false
)

class MainViewModel(private val appContext: Context, private val store: BoxStore) : ViewModel() {
    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false }
    private val preferences = appContext.getSharedPreferences("boxdeck-settings", Context.MODE_PRIVATE)
    private val _ui = MutableStateFlow(
        PhoneUiState(
            notifications = preferences.getBoolean("notifications", false),
            quietFrom = preferences.getString("quiet_from", "23:00") ?: "23:00",
            quietTo = preferences.getString("quiet_to", "08:00") ?: "08:00",
            disarmed = preferences.getBoolean("disarmed", false)
        )
    )
    val ui: StateFlow<PhoneUiState> = _ui.asStateFlow()
    var onNewAlert: ((String, Alert) -> Unit)? = null
    private var events: Closeable? = null
    private var refreshJob: Job? = null

    init {
        viewModelScope.launch { store.boxes.collect { refresh() } }
    }

    fun setTab(tab: PhoneTab) { _ui.value = _ui.value.copy(tab = tab) }

    fun refresh() {
        if (refreshJob?.isActive == true) return
        refreshJob = viewModelScope.launch {
            _ui.value = _ui.value.copy(refreshing = true, error = "")
            runCatching { loadFleet() }.onSuccess { data ->
                _ui.value = _ui.value.copy(fleet = data.devices, alerts = data.alerts, usage = data.usage, refreshing = false)
            }.onFailure { error ->
                _ui.value = _ui.value.copy(refreshing = false, error = error.message ?: "Could not refresh")
            }
        }
    }

    private suspend fun loadFleet(): FleetLoad = supervisorScope {
        val pairs = store.clients()
        val deckCards = pairs.map { (_, client) -> async { runCatching { client.boxes() }.getOrDefault(emptyList()) } }.awaitAll().flatten()
        val peerSource = pairs.firstNotNullOfOrNull { (_, client) -> runCatching { client.peers() }.getOrNull() }
        val peerList = peerSource.orEmpty()
        val states = pairs.associate { (box, client) -> box.url.trimEnd('/') to async { runCatching { client.state() }.getOrNull() } }.mapValues { it.value.await() }
        val usages = pairs.associate { (box, client) -> box.url.trimEnd('/') to async { runCatching { client.usage(30) }.getOrNull() } }.mapValues { it.value.await() }
        val devices = mergeFleet(store.boxes.value, deckCards, peerList, states, usages)
        val alertLists = pairs.map { (box, client) -> async { box.url to runCatching { client.alerts().alerts }.getOrDefault(emptyList()) } }.awaitAll()
        val alerts = alertLists.flatMap { (url, items) -> items.map { AlertItem(it, url) } }.sortedByDescending { it.alert.at }
        val usage = pairs.firstNotNullOfOrNull { (box, _) -> usages[box.url.trimEnd('/')] } ?: UsageResponse()
        FleetLoad(devices, alerts, usage)
    }

    private fun mergeFleet(
        configured: List<SavedBox>,
        cards: List<BoxSnapshot>,
        peers: List<TailnetPeer>,
        states: Map<String, State?>,
        usages: Map<String, UsageResponse?>
    ): List<FleetDevice> {
        val deckMap = linkedMapOf<String, BoxSnapshot>()
        cards.forEach { card ->
            val key = card.url.trimEnd('/').ifBlank { card.name }
            val configuredBox = configured.firstOrNull { it.url.trimEnd('/') == key || it.name == card.name }
            val state = states[key] ?: configuredBox?.let { states[it.url.trimEnd('/')] }
            val usage = usages[key]
            deckMap[key] = card.copy(
                state = state,
                health = state?.health ?: card.health,
                agents = state?.agents?.size ?: card.agents,
                ports = state?.ports?.size ?: card.ports,
                usage = usage?.let { response -> BoxUsage(response.providers.mapValues { it.value.today }, response.providers.mapValues { it.value.quota }) } ?: card.usage
            )
        }
        configured.forEach { box ->
            val key = box.url.trimEnd('/')
            if (key !in deckMap) {
                val state = states[key]
                val usage = usages[key]
                deckMap[key] = BoxSnapshot(box.name, key, ok = state != null, health = state?.health ?: Health(), agents = state?.agents?.size ?: 0, ports = state?.ports?.size ?: 0, state = state, usage = usage?.let { response -> BoxUsage(response.providers.mapValues { it.value.today }, response.providers.mapValues { it.value.quota }) } ?: BoxUsage())
            }
        }
        peers.filter { it.boxdeck }.forEach { peer ->
            val key = peer.url.trimEnd('/').ifBlank { "peer:${peer.name}" }
            if (peer.url.isNotBlank() && deckMap.keys.any { it == key }) return@forEach
            deckMap[key] = BoxSnapshot(peer.name, peer.url, ok = peer.online, discovered = true, tag = "tailnet", health = Health(), agents = 0, ports = 0)
        }
        val devices = deckMap.values.map { FleetDevice(it.name, it.url, deck = it) }.toMutableList()
        peers.filterNot { it.boxdeck }.forEach { peer -> devices += FleetDevice(peer.name, peer.url, peer = peer) }
        return devices.sortedWith(compareBy<FleetDevice>({ if (it.deck?.local == true) 0 else if (it.isDeck) 1 else if (it.online) 2 else 3 }, { it.name.lowercase() }))
    }

    fun selectDevice(device: FleetDevice) {
        _ui.value = _ui.value.copy(selectedDevice = device, tab = PhoneTab.BOXES)
        if (device.isDeck) loadDetail(device)
    }

    fun clearSelectedDevice() { _ui.value = _ui.value.copy(selectedDevice = null, detail = DetailState()) }

    fun loadDetail(device: FleetDevice) {
        val pair = store.clients().firstOrNull { it.first.url.trimEnd('/') == device.url.trimEnd('/') } ?: return
        viewModelScope.launch {
            _ui.value = _ui.value.copy(detail = DetailState(loading = true))
            supervisorScope {
                val state = async { runCatching { pair.second.state() }.getOrDefault(State()) }
                val ports = async { runCatching { pair.second.ports() }.getOrDefault(emptyList()) }
                val agents = async { runCatching { pair.second.agents() }.getOrDefault(emptyList()) }
                val usage = async { runCatching { pair.second.usage(30) }.getOrDefault(UsageResponse()) }
                val apps = async { runCatching { pair.second.apps() }.getOrDefault(emptyList()) }
                val agentRows = agents.await()
                val tails = agentRows.mapNotNull { row ->
                    val pane = row.pane.ifBlank { row.paneId }
                    if (pane.isBlank()) null else pane to runCatching { pair.second.herdRead(pane, 20).text }.getOrDefault("")
                }.toMap()
                _ui.value = _ui.value.copy(detail = DetailState(false, state.await(), ports.await(), agentRows, tails, usage.await(), apps.await()))
            }
        }
    }

    fun prompt(device: FleetDevice, pane: String, text: String) {
        clientFor(device.url)?.let { client -> viewModelScope.launch { runCatching { client.herdPrompt(pane, text) }; loadDetail(device) } }
    }

    fun interrupt(device: FleetDevice, pane: String) {
        clientFor(device.url)?.let { client -> viewModelScope.launch { runCatching { client.herdInterrupt(pane) }; loadDetail(device) } }
    }

    fun performAction(url: String, action: AlertAction) {
        val client = clientFor(url) ?: return
        viewModelScope.launch {
            runCatching { client.rawPost(action.path, action.body) }
            refresh()
        }
    }

    fun ack(item: AlertItem) = performAction(item.boxUrl, AlertAction("Ack", path = "/api/alerts/${item.alert.id}/ack"))
    fun snooze(item: AlertItem) = performAction(item.boxUrl, AlertAction("Snooze", path = "/api/alerts/${item.alert.id}/snooze", body = json.parseToJsonElement("{\"until\":\"2h\"}").jsonObject))
    fun resolve(item: AlertItem) = performAction(item.boxUrl, AlertAction("Resolve", path = "/api/alerts/${item.alert.id}/resolve"))

    fun appAction(device: FleetDevice, app: AppView, action: String) {
        clientFor(device.url)?.let { client -> viewModelScope.launch { runCatching { client.appAction(app.id, action) }; loadDetail(device) } }
    }

    fun updateSettings(notifications: Boolean, from: String, to: String, disarmed: Boolean) {
        preferences.edit().putBoolean("notifications", notifications).putString("quiet_from", from).putString("quiet_to", to).putBoolean("disarmed", disarmed).apply()
        _ui.value = _ui.value.copy(notifications = notifications, quietFrom = from, quietTo = to, disarmed = disarmed)
    }

    fun addBox(name: String, url: String, token: String) = store.upsert(name, url, token)
    fun removeBox(url: String) = store.remove(url)

    fun handleDeepLink(uri: android.net.Uri?) {
        if (uri?.scheme != "boxdeck") return
        when (uri.host) {
            "add" -> {
                val url = uri.getQueryParameter("url").orEmpty()
                val token = uri.getQueryParameter("token").orEmpty()
                if (url.isNotBlank()) addBox(url.substringAfterLast('/').ifBlank { "box" }, url, token)
            }
            "box" -> {
                val name = uri.pathSegments.firstOrNull().orEmpty()
                _ui.value.fleet.firstOrNull { it.name == name }?.let(::selectDevice)
            }
            "alert" -> setTab(PhoneTab.NEEDS)
        }
    }

    fun startEvents() {
        events?.close()
        val pair = store.clients().firstOrNull() ?: return
        events = pair.second.events(listener = object : BoxdeckClient.EventsListener {
            override fun onEvent(event: ServerEvent) {
                val alertJson = event.data["alert"]?.jsonObject ?: event.data
                val alert = runCatching { json.decodeFromJsonElement(Alert.serializer(), alertJson) }.getOrNull() ?: return
                onNewAlert?.invoke(pair.first.url, alert)
                refresh()
            }
        })
    }

    private fun clientFor(url: String): BoxdeckClient? = store.clients().firstOrNull { it.first.url.trimEnd('/') == url.trimEnd('/') }?.second

    override fun onCleared() {
        events?.close()
        super.onCleared()
    }

    private data class FleetLoad(val devices: List<FleetDevice>, val alerts: List<AlertItem>, val usage: UsageResponse)

    companion object {
        fun factory(context: Context, store: BoxStore): ViewModelProvider.Factory = object : ViewModelProvider.Factory {
            @Suppress("UNCHECKED_CAST")
            override fun <T : ViewModel> create(modelClass: Class<T>): T = MainViewModel(context.applicationContext, store) as T
        }
    }
}
