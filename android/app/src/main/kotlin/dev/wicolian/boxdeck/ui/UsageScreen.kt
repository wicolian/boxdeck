package dev.wicolian.boxdeck.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import dev.wicolian.boxdeck.core.UsageProvider
import dev.wicolian.boxdeck.core.UsageResponse

@Composable
fun UsageScreen(usage: UsageResponse) {
    if (usage.providers.isEmpty()) {
        EmptyState("Usage is unavailable.")
        return
    }
    LazyColumn(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        items(usage.providers.toList(), key = { it.first }) { (provider, value) -> UsageProviderCard(provider, value) }
    }
}

@Composable
fun UsageProviderCard(provider: String, value: UsageProvider) {
    SectionCard(provider) {
        Text("today ${value.today.tokens.total} tokens / $${"%.4f".format(value.today.costUsd)}", fontFamily = FontFamily.Monospace)
        value.quota?.fiveHour?.let { quota ->
            Text("5h ${quota.pct.toInt()}%")
            LinearProgressIndicator({ (quota.pct / 100).toFloat().coerceIn(0f, 1f) }, Modifier.fillMaxWidth())
        }
        value.quota?.sevenDay?.let { quota ->
            Text("7d ${quota.pct.toInt()}%")
            LinearProgressIndicator({ (quota.pct / 100).toFloat().coerceIn(0f, 1f) }, Modifier.fillMaxWidth())
        }
        Text("${value.days.size} days / ${value.sessions} sessions")
    }
}
