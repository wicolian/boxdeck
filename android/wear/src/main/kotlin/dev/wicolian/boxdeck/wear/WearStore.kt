package dev.wicolian.boxdeck.wear

import android.content.Context
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import dev.wicolian.boxdeck.core.Alert
import dev.wicolian.boxdeck.core.BoxdeckClient
import dev.wicolian.boxdeck.core.State
import dev.wicolian.boxdeck.core.UsageResponse
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

@Serializable
data class WearBox(val name: String, val url: String, val token: String)

data class WearSnapshot(
    val boxes: List<WearBox> = emptyList(),
    val alerts: List<Pair<WearBox, Alert>> = emptyList(),
    val states: Map<String, State> = emptyMap(),
    val usage: Map<String, UsageResponse> = emptyMap()
)

class WearStore(context: Context) {
    private val json = Json { ignoreUnknownKeys = true }
    private val preferences = run {
        val key = MasterKey.Builder(context).setKeyScheme(MasterKey.KeyScheme.AES256_GCM).build()
        EncryptedSharedPreferences.create(context, "boxdeck-wear", key, EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV, EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM)
    }

    fun boxes(): List<WearBox> = runCatching { json.decodeFromString<List<WearBox>>(preferences.getString("boxes", "[]") ?: "[]") }.getOrDefault(emptyList())

    fun replaceBoxes(value: List<WearBox>) {
        preferences.edit().putString("boxes", json.encodeToString(value.distinctBy { it.url.trimEnd('/') })).apply()
    }

    suspend fun refresh(): WearSnapshot = withContext(Dispatchers.IO) {
        val boxes = boxes()
        val states = boxes.associate { box -> box.url to runCatching { BoxdeckClient(box.url, box.token).state() }.getOrNull() }.filterValues { it != null }.mapValues { it.value!! }
        val usage = boxes.associate { box -> box.url to runCatching { BoxdeckClient(box.url, box.token).usage(30) }.getOrNull() }.filterValues { it != null }.mapValues { it.value!! }
        val alerts = boxes.flatMap { box -> runCatching { BoxdeckClient(box.url, box.token).alerts().map { box to it } }.getOrDefault(emptyList()) }
        WearSnapshot(boxes, alerts, states, usage)
    }
}
