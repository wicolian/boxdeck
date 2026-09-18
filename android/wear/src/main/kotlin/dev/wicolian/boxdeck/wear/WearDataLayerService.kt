package dev.wicolian.boxdeck.wear

import com.google.android.gms.wearable.MessageEvent
import com.google.android.gms.wearable.WearableListenerService
import kotlinx.serialization.json.Json

class WearDataLayerService : WearableListenerService() {
    private val json = Json { ignoreUnknownKeys = true }
    override fun onMessageReceived(event: MessageEvent) {
        if (event.path == "/boxdeck/boxes") {
            runCatching { json.decodeFromString<List<WearBox>>(event.data.toString(Charsets.UTF_8)) }.getOrNull()?.let { WearStore(applicationContext).replaceBoxes(it) }
        }
    }
}
