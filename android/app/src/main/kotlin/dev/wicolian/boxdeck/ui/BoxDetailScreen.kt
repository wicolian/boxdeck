package dev.wicolian.boxdeck.ui

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.ScrollableTabRow
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.unit.dp
import dev.wicolian.boxdeck.MainViewModel
import dev.wicolian.boxdeck.core.AppView
import dev.wicolian.boxdeck.core.FleetDevice
import dev.wicolian.boxdeck.core.needsYouStatus

enum class BoxDetailTab(val label: String) { OVERVIEW("Overview"), AGENTS("Agents"), PORTS("Ports"), USAGE("Usage"), APPS("Apps"), TERMINAL("Terminal"), FILES("Files") }

@Composable
fun BoxDetailScreen(device: FleetDevice, detail: dev.wicolian.boxdeck.DetailState, tab: BoxDetailTab, onTab: (BoxDetailTab) -> Unit, viewModel: MainViewModel) {
    Column(Modifier.fillMaxWidth()) {
        ScrollableTabRow(selectedTabIndex = tab.ordinal) {
            BoxDetailTab.entries.forEach { item -> Tab(selected = tab == item, onClick = { onTab(item) }, text = { Text(item.label) }) }
        }
        if (detail.loading) LoadingOrError(true, "")
        when (tab) {
            BoxDetailTab.OVERVIEW -> OverviewDetail(device)
            BoxDetailTab.AGENTS -> AgentsDetail(device, detail, viewModel)
            BoxDetailTab.PORTS -> PortsDetail(detail)
            BoxDetailTab.USAGE -> UsageScreen(detail.usage)
            BoxDetailTab.APPS -> AppsDetail(device, detail.apps, viewModel)
            BoxDetailTab.TERMINAL -> WebViewScreen(device.url, "/term/")
            BoxDetailTab.FILES -> WebViewScreen(device.url, "/files/")
        }
    }
}

@Composable
private fun OverviewDetail(device: FleetDevice) {
    val deck = device.deck ?: return
    SectionCard(device.name) {
        MetricLine("state", if (deck.ok) "online" else "unreachable")
        MetricLine("cpu", "${deck.health.cpu.toInt()}%")
        MetricLine("memory", "${deck.health.memUsed.toLong()} / ${deck.health.memTotal.toLong()}")
        MetricLine("load", "%.1f".format(deck.health.load1))
        MetricLine("agents", deck.agents.toString())
        MetricLine("ports", deck.ports.toString())
    }
}

@Composable
private fun AgentsDetail(device: FleetDevice, detail: dev.wicolian.boxdeck.DetailState, viewModel: MainViewModel) {
    LazyColumn(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        items(detail.agents, key = { it.pane.ifBlank { it.paneId + it.kind } }) { agent ->
            val pane = agent.pane.ifBlank { agent.paneId }
            var prompt by remember(pane) { mutableStateOf("") }
            SectionCard(agent.title.ifBlank { agent.kind.ifBlank { "Agent" } }) {
                Text(agent.effectiveStatus.ifBlank { "unknown" })
                if (pane.isNotBlank()) {
                    Text(detail.tails[pane].orEmpty(), modifier = Modifier.fillMaxWidth())
                    OutlinedTextField(prompt, { prompt = it }, Modifier.fillMaxWidth(), label = { Text("Prompt") })
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Button(onClick = { if (prompt.isNotBlank()) { viewModel.prompt(device, pane, prompt); prompt = "" } }) { Text("Send") }
                        OutlinedButton(onClick = { viewModel.interrupt(device, pane) }) { Text("Interrupt") }
                    }
                }
            }
        }
    }
}

@Composable
private fun PortsDetail(detail: dev.wicolian.boxdeck.DetailState) {
    val uriHandler = LocalUriHandler.current
    LazyColumn(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        items(detail.ports, key = { it.port }) { port ->
            SectionCard("${port.port} ${port.label}") {
                Text(port.proc.ifBlank { port.title.ifBlank { "listening" } })
                if (port.url.isNotBlank()) Button(onClick = { uriHandler.openUri(port.url) }) { Text("Open link") }
            }
        }
    }
}

@Composable
private fun AppsDetail(device: FleetDevice, apps: List<AppView>, viewModel: MainViewModel) {
    LazyColumn(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        items(apps, key = { it.id }) { app ->
            SectionCard(app.name) {
                Text(app.status.ifBlank { if (app.running) "running" else "stopped" })
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (app.canStart) Button(onClick = { viewModel.appAction(device, app, "start") }) { Text("Start") }
                    if (app.canStop) OutlinedButton(onClick = { viewModel.appAction(device, app, "stop") }) { Text("Stop") }
                    if (app.running) OutlinedButton(onClick = { viewModel.appAction(device, app, "restart") }) { Text("Restart") }
                }
            }
        }
    }
}
