package dev.wicolian.boxdeck

import android.content.Context
import com.google.android.gms.wearable.Wearable
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

@Serializable
private data class WearBoxPayload(val name: String, val url: String, val token: String)

object WearSync {
    fun publish(context: Context, boxes: List<SavedBox>) {
        val bytes = Json.encodeToString(boxes.map { WearBoxPayload(it.name, it.url, it.token) }).toByteArray()
        Wearable.getNodeClient(context).connectedNodes.addOnSuccessListener { nodes ->
            nodes.forEach { node -> Wearable.getMessageClient(context).sendMessage(node.id, "/boxdeck/boxes", bytes) }
        }
    }
}
