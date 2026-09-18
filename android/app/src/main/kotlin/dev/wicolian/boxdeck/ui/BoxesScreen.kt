package dev.wicolian.boxdeck.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import dev.wicolian.boxdeck.MainViewModel
import dev.wicolian.boxdeck.core.FleetDevice

@Composable
fun BoxesScreen(devices: List<FleetDevice>, viewModel: MainViewModel) {
    if (devices.isEmpty()) {
        EmptyState("Add a box in Settings to see the fleet.")
        return
    }
    LazyColumn(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        items(devices, key = { it.name + it.url }) { device ->
            Card(Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 6.dp)) {
                Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text(device.name)
                    if (device.isDeck) {
                        val deck = device.deck!!
                        Text(if (deck.ok) "online deck" else "unreachable", color = if (deck.ok) dev.wicolian.boxdeck.Moss else dev.wicolian.boxdeck.Rust)
                        MetricLine("cpu", "${deck.health.cpu.toInt()}%")
                        MetricLine("agents", "${deck.agents}")
                        MetricLine("ports", "${deck.ports}")
                        Button(onClick = { viewModel.selectDevice(device) }) { Text("Open box") }
                    } else {
                        val peer = device.peer!!
                        Text("${peer.os.ifBlank { "tailnet device" }} / ${if (peer.online) "online" else "offline"}", fontFamily = FontFamily.Monospace)
                        if (peer.lastSeen.isNotBlank()) Text("last seen ${peer.lastSeen}")
                        Text(if (peer.os.lowercase() in setOf("android", "ios", "iosmobile")) "phone" else "install boxdeck")
                    }
                }
            }
        }
    }
}
