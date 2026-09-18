package dev.wicolian.boxdeck.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import dev.wicolian.boxdeck.AlertItem
import dev.wicolian.boxdeck.MainViewModel
import dev.wicolian.boxdeck.core.AlertAction

@Composable
fun AlertScreen(items: List<AlertItem>, viewModel: MainViewModel) {
    if (items.isEmpty()) {
        EmptyState("Quiet. Nothing needs you.")
        return
    }
    LazyColumn(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        items(items, key = { it.alert.id }) { item ->
            SectionCard(item.alert.title.ifBlank { "Alert" }) {
                Text("${item.alert.severity} / ${item.alert.box}")
                if (item.alert.body.isNotBlank()) Text(item.alert.body)
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    item.alert.actions.forEach { action ->
                        AssistChip(onClick = { viewModel.performAction(item.boxUrl, action) }, label = { Text(action.label) })
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        Button(onClick = { viewModel.ack(item) }) { Text("Ack") }
                        Button(onClick = { viewModel.snooze(item) }) { Text("Snooze") }
                        Button(onClick = { viewModel.resolve(item) }) { Text("Resolve") }
                    }
                }
            }
        }
    }
}
