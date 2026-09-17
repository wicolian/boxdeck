package dev.wicolian.boxdeck

import android.content.Context
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import dev.wicolian.boxdeck.core.BoxdeckClient
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

@Serializable
data class SavedBox(val name: String, val url: String, val token: String)

class BoxStore(context: Context) {
    private val json = Json { ignoreUnknownKeys = true }
    private val preferences = run {
        val key = MasterKey.Builder(context)
            .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
            .build()
        EncryptedSharedPreferences.create(
            context,
            "boxdeck-secure",
            key,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM
        )
    }
    private val _boxes = MutableStateFlow(load())
    val boxes: StateFlow<List<SavedBox>> = _boxes.asStateFlow()

    fun clients(): List<Pair<SavedBox, BoxdeckClient>> = _boxes.value.map { it to BoxdeckClient(it.url, it.token) }

    fun upsert(name: String, url: String, token: String) {
        val normalized = url.trim().trimEnd('/')
        if (normalized.isBlank() || !normalized.startsWith("http")) return
        val next = _boxes.value.filterNot { it.url.trimEnd('/') == normalized } + SavedBox(name.trim().ifBlank { normalized }, normalized, token.trim())
        save(next)
    }

    fun remove(url: String) = save(_boxes.value.filterNot { it.url.trimEnd('/') == url.trimEnd('/') })

    private fun load(): List<SavedBox> = runCatching { json.decodeFromString<List<SavedBox>>(preferences.getString(KEY_BOXES, "[]") ?: "[]") }.getOrDefault(emptyList())

    private fun save(value: List<SavedBox>) {
        val sorted = value.distinctBy { it.url.trimEnd('/') }.sortedBy { it.name.lowercase() }
        preferences.edit().putString(KEY_BOXES, json.encodeToString(sorted)).apply()
        _boxes.value = sorted
    }

    companion object {
        private const val KEY_BOXES = "boxes"
    }
}
