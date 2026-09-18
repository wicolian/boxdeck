package dev.wicolian.boxdeck.core

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.KSerializer
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.sse.EventSource
import okhttp3.sse.EventSourceListener
import okhttp3.sse.EventSources
import java.io.Closeable
import java.io.IOException
import java.util.concurrent.TimeUnit

class BoxdeckClient(
    baseUrl: String,
    private val token: String,
    private val httpClient: OkHttpClient = defaultHttpClient()
) {
    val baseUrl: String = baseUrl.trimEnd('/')
    private val json = Json { ignoreUnknownKeys = true; coerceInputValues = true; explicitNulls = false }
    private val mediaType = "application/json; charset=utf-8".toMediaType()

    suspend fun health(): HealthResponse = get("/api/health", HealthResponse.serializer(), authenticated = false)
    suspend fun state(): State = get("/api/state", State.serializer())
    suspend fun ports(): List<Port> = get("/api/ports", ListSerializerHolder.ports)
    suspend fun processes(sort: String = "cpu", limit: Int = 20): List<ProcessInfo> = get("/api/procs?sort=$sort&n=$limit", ListSerializerHolder.processes)
    suspend fun agents(): List<Agent> = get("/api/agents", ListSerializerHolder.agents)
    suspend fun usage(days: Int = 30): UsageResponse = get("/api/usage?days=${days.coerceIn(1, 30)}", UsageResponse.serializer())
    suspend fun usageAll(days: Int = 30): UsageAllResponse = get("/api/usage/all?days=${days.coerceIn(1, 30)}", UsageAllResponse.serializer())
    suspend fun boxes(): List<BoxSnapshot> = get("/api/boxes", ListSerializerHolder.boxes)
    suspend fun peers(): List<TailnetPeer> = get("/api/net/peers", ListSerializerHolder.peers)
    suspend fun apps(): List<AppView> = get("/api/apps", ListSerializerHolder.apps)
    suspend fun appAction(id: String, action: String): AppView = post("/api/apps/${encodePath(id)}/$action", null, AppView.serializer())
    suspend fun appLog(id: String, lines: Int = 200): JsonObject = get("/api/apps/${encodePath(id)}/log?lines=${lines.coerceIn(1, 1000)}", JsonObject.serializer())
    suspend fun herdRead(pane: String, lines: Int = 80): HerdRead = get("/api/herd/read?pane=${encodeQuery(pane)}&lines=${lines.coerceIn(1, 400)}", HerdRead.serializer())
    suspend fun herdKeys(pane: String, keys: String): JsonObject = post("/api/herd/keys", jsonObjectOf("pane" to JsonPrimitive(pane), "keys" to JsonPrimitive(keys)), JsonObject.serializer())
    suspend fun herdPrompt(pane: String, text: String): JsonObject = post("/api/herd/prompt", jsonObjectOf("pane" to JsonPrimitive(pane), "text" to JsonPrimitive(text)), JsonObject.serializer())
    suspend fun herdInterrupt(pane: String): JsonObject = post("/api/herd/interrupt", jsonObjectOf("pane" to JsonPrimitive(pane)), JsonObject.serializer())
    suspend fun herdrFocus(paneId: String): JsonObject = post("/api/herdr/focus", jsonObjectOf("pane_id" to JsonPrimitive(paneId)), JsonObject.serializer())
    suspend fun procKill(pid: Int, signal: Int = 15): JsonObject = post("/api/proc/kill", jsonObjectOf("pid" to JsonPrimitive(pid), "signal" to JsonPrimitive(signal)), JsonObject.serializer())

    suspend fun alerts(state: String = "open", since: String? = null): List<Alert> = get("/api/alerts?state=${encodeQuery(state)}${since?.let { "&since=${encodeQuery(it)}" }.orEmpty()}", ListSerializerHolder.alerts)
    suspend fun alert(id: String): Alert = get("/api/alerts/${encodePath(id)}", Alert.serializer())
    suspend fun createAlert(alert: JsonObject): Alert = post("/api/alerts", alert, Alert.serializer())
    suspend fun ackAlert(id: String): Alert = post("/api/alerts/${encodePath(id)}/ack", null, Alert.serializer())
    suspend fun snoozeAlert(id: String, until: String): Alert = post("/api/alerts/${encodePath(id)}/snooze", jsonObjectOf("until" to JsonPrimitive(until)), Alert.serializer())
    suspend fun resolveAlert(id: String): Alert = post("/api/alerts/${encodePath(id)}/resolve", null, Alert.serializer())
    suspend fun snoozeAll(until: String): JsonObject = post("/api/alerts/snooze-all", jsonObjectOf("until" to JsonPrimitive(until)), JsonObject.serializer())
    suspend fun disarm(on: Boolean): JsonObject = post("/api/alerts/disarm", jsonObjectOf("on" to JsonPrimitive(on)), JsonObject.serializer())
    suspend fun alertRules(): AlertRulesResponse = get("/api/alerts/rules", AlertRulesResponse.serializer())
    suspend fun updateAlertRules(rules: JsonObject): AlertRulesResponse = put("/api/alerts/rules", rules, AlertRulesResponse.serializer())
    suspend fun testSink(name: String): JsonObject = get("/api/alerts/sinks/test?name=${encodeQuery(name)}", JsonObject.serializer())

    fun events(lastEventId: String? = null, listener: EventsListener): Closeable {
        val request = requestBuilder("/api/events", authenticated = true).apply {
            if (!lastEventId.isNullOrBlank()) header("Last-Event-ID", lastEventId)
        }.build()
        val source = EventSources.createFactory(httpClient).newEventSource(request, object : EventSourceListener() {
            override fun onOpen(eventSource: EventSource, response: okhttp3.Response) = listener.onOpen()
            override fun onEvent(eventSource: EventSource, id: String?, type: String?, data: String) {
                val parsed = runCatching { json.parseToJsonElement(data).jsonObject }.getOrDefault(buildJsonObject { })
                listener.onEvent(ServerEvent(id.orEmpty(), type.orEmpty(), parsed))
            }
            override fun onClosed(eventSource: EventSource) = listener.onClosed(null)
            override fun onFailure(eventSource: EventSource, t: Throwable?, response: okhttp3.Response?) = listener.onClosed(t ?: IOException("event stream failed"))
        })
        return Closeable { source.cancel() }
    }

    suspend fun rawPost(path: String, body: JsonObject): JsonObject = post(path, body, JsonObject.serializer())

    private suspend inline fun <reified T> get(path: String, serializer: KSerializer<T>, authenticated: Boolean = true): T = withContext(Dispatchers.IO) {
        execute(Request.Builder().url(baseUrl + path).get().build(), serializer, authenticated)
    }

    private suspend inline fun <reified T> post(path: String, body: JsonElement?, serializer: KSerializer<T>): T = withContext(Dispatchers.IO) {
        val request = requestBuilder(path, authenticated = true).post((body ?: JsonObject(emptyMap())).toString().toRequestBody(mediaType)).build()
        execute(request, serializer, true)
    }

    private suspend inline fun <reified T> put(path: String, body: JsonElement, serializer: KSerializer<T>): T = withContext(Dispatchers.IO) {
        val request = requestBuilder(path, authenticated = true).put(body.toString().toRequestBody(mediaType)).build()
        execute(request, serializer, true)
    }

    private inline fun <reified T> execute(request: Request, serializer: KSerializer<T>, authenticated: Boolean): T {
        val prepared = request.newBuilder().apply {
            if (authenticated && token.isNotBlank()) header("Authorization", "Bearer $token")
        }.build()
        httpClient.newCall(prepared).execute().use { response ->
            val raw = response.body?.string().orEmpty()
            if (!response.isSuccessful) throw BoxdeckHttpException(response.code, request.url.encodedPath, raw.ifBlank { "HTTP ${response.code}" })
            return json.decodeFromString(serializer, raw.ifBlank { "{}" })
        }
    }

    private fun requestBuilder(path: String, authenticated: Boolean): Request.Builder = Request.Builder().url(baseUrl + path).apply {
        if (authenticated && token.isNotBlank()) header("Authorization", "Bearer $token")
    }

    private object ListSerializerHolder {
        val ports = kotlinx.serialization.builtins.ListSerializer(Port.serializer())
        val processes = kotlinx.serialization.builtins.ListSerializer(ProcessInfo.serializer())
        val agents = kotlinx.serialization.builtins.ListSerializer(Agent.serializer())
        val boxes = kotlinx.serialization.builtins.ListSerializer(BoxSnapshot.serializer())
        val peers = kotlinx.serialization.builtins.ListSerializer(TailnetPeer.serializer())
        val apps = kotlinx.serialization.builtins.ListSerializer(AppView.serializer())
        val alerts = kotlinx.serialization.builtins.ListSerializer(Alert.serializer())
    }

    interface EventsListener {
        fun onOpen() {}
        fun onEvent(event: ServerEvent)
        fun onClosed(error: Throwable?) {}
    }

    companion object {
        fun defaultHttpClient(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(5, TimeUnit.SECONDS)
            .readTimeout(0, TimeUnit.MILLISECONDS)
            .callTimeout(30, TimeUnit.SECONDS)
            .build()

        private fun encodeQuery(value: String): String = java.net.URLEncoder.encode(value, Charsets.UTF_8)
        private fun encodePath(value: String): String = java.net.URLEncoder.encode(value, Charsets.UTF_8).replace("+", "%20")
        private fun jsonObjectOf(vararg values: Pair<String, JsonElement>): JsonObject = buildJsonObject { values.forEach { put(it.first, it.second) } }
    }
}
