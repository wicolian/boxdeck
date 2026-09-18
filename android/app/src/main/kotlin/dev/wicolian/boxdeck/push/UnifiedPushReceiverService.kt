package dev.wicolian.boxdeck.push

import dev.wicolian.boxdeck.notifications.AlertNotifications
import dev.wicolian.boxdeck.core.Alert
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.unifiedpush.android.connector.PushService
import org.unifiedpush.android.connector.data.PushEndpoint
import org.unifiedpush.android.connector.data.PushMessage
import org.unifiedpush.android.connector.FailedReason

class UnifiedPushReceiverService : PushService() {
    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false }

    override fun onMessage(message: PushMessage, instance: String) {
        val payload = runCatching { json.parseToJsonElement(message.content.toString(Charsets.UTF_8)).jsonObject }.getOrNull() ?: return
        val alertJson = payload["alert"]?.jsonObject ?: payload
        val alert = runCatching { json.decodeFromJsonElement(Alert.serializer(), alertJson) }.getOrNull() ?: return
        val boxUrl = payload["boxUrl"]?.jsonPrimitive?.content.orEmpty()
        AlertNotifications.show(this, boxUrl, alert)
    }

    override fun onNewEndpoint(endpoint: PushEndpoint, instance: String) {
        getSharedPreferences("boxdeck-push", MODE_PRIVATE).edit().putString("endpoint", endpoint.url).apply()
    }

    override fun onRegistrationFailed(reason: FailedReason, instance: String) {
        getSharedPreferences("boxdeck-push", MODE_PRIVATE).edit().putString("error", reason.toString()).apply()
    }

    override fun onUnregistered(instance: String) = Unit
}
