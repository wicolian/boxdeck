package dev.wicolian.boxdeck.notifications

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import dev.wicolian.boxdeck.BoxdeckApplication
import dev.wicolian.boxdeck.MainActivity
import dev.wicolian.boxdeck.R
import dev.wicolian.boxdeck.core.Alert
import dev.wicolian.boxdeck.core.AlertAction
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject

object NotificationChannels {
    const val ALERTS = "alerts"

    fun create(context: Context) {
        val manager = context.getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel(ALERTS, context.getString(R.string.notification_channel_alerts), NotificationManager.IMPORTANCE_HIGH))
    }
}

object AlertNotifications {
    fun show(context: Context, boxUrl: String, alert: Alert) {
        NotificationChannels.create(context)
        val builder = NotificationCompat.Builder(context, NotificationChannels.ALERTS)
            .setSmallIcon(android.R.drawable.ic_dialog_alert)
            .setContentTitle(alert.title.ifBlank { "Boxdeck alert" })
            .setContentText(alert.body.ifBlank { alert.box })
            .setStyle(NotificationCompat.BigTextStyle().bigText(alert.body))
            .setPriority(priority(alert.severity))
            .setAutoCancel(true)
            .setContentIntent(openIntent(context, boxUrl, alert.id))
        alert.actions.take(3).forEachIndexed { index, action ->
            val pendingIntent = actionIntent(context, boxUrl, action, alert.id, index)
            builder.addAction(NotificationCompat.Action.Builder(android.R.drawable.ic_menu_send, action.label.ifBlank { "Act" }, pendingIntent).build())
        }
        runCatching { NotificationManagerCompat.from(context).notify(alert.id.hashCode(), builder.build()) }
    }

    private fun openIntent(context: Context, boxUrl: String, alertId: String): PendingIntent = PendingIntent.getActivity(
        context,
        alertId.hashCode(),
        Intent(context, MainActivity::class.java).setData(android.net.Uri.parse("boxdeck://alert/$alertId")).putExtra("box_url", boxUrl),
        PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
    )

    private fun actionIntent(context: Context, boxUrl: String, action: AlertAction, alertId: String, index: Int): PendingIntent = PendingIntent.getBroadcast(
        context,
        (alertId + action.label + index).hashCode(),
        Intent(context, NotificationActionReceiver::class.java)
            .putExtra(EXTRA_URL, boxUrl)
            .putExtra(EXTRA_PATH, action.path)
            .putExtra(EXTRA_BODY, action.body.toString()),
        PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
    )

    private fun priority(severity: String): Int = when (severity.lowercase()) {
        "critical" -> NotificationCompat.PRIORITY_MAX
        "warning" -> NotificationCompat.PRIORITY_HIGH
        else -> NotificationCompat.PRIORITY_DEFAULT
    }
}

class NotificationActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val pending = goAsync()
        CoroutineScope(Dispatchers.IO).launch {
            try {
                val app = context.applicationContext as BoxdeckApplication
                val url = intent.getStringExtra(EXTRA_URL).orEmpty()
                val path = intent.getStringExtra(EXTRA_PATH).orEmpty()
                val body = Json.parseToJsonElement(intent.getStringExtra(EXTRA_BODY).orEmpty()).let { it as? JsonObject }
                app.boxStore.clients().firstOrNull { it.first.url.trimEnd('/') == url.trimEnd('/') }?.second?.rawPost(path, body ?: JsonObject(emptyMap()))
            } finally {
                pending.finish()
            }
        }
    }
}

const val EXTRA_URL = "boxdeck.url"
const val EXTRA_PATH = "boxdeck.path"
const val EXTRA_BODY = "boxdeck.body"
