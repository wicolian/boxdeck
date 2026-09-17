package dev.wicolian.boxdeck.wear

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.wear.compose.material.Button
import androidx.wear.compose.material.Chip
import androidx.wear.compose.material.MaterialTheme
import androidx.wear.compose.material.Scaffold
import androidx.wear.compose.material.ScalingLazyColumn
import androidx.wear.compose.material.Text
import androidx.wear.compose.material.TimeText
import dev.wicolian.boxdeck.core.Alert
import kotlinx.coroutines.launch

@Composable
fun WearApp(store: WearStore) {
    var snapshot by remember { mutableStateOf(WearSnapshot()) }
    var page by remember { mutableStateOf(0) }
    val scope = rememberCoroutineScope()
    fun refresh() { scope.launch { snapshot = store.refresh() } }
    LaunchedEffect(Unit) { refresh() }
    MaterialTheme {
        Scaffold(timeText = { TimeText() }) {
            ScalingLazyColumn(modifier = Modifier.fillMaxSize().padding(horizontal = 8.dp)) {
                item { Chip(onClick = { page = if (page == 0) 1 else 0 }, label = { Text(if (page == 0) "Boxes" else "Needs you") }) }
                if (page == 0) {
                    if (snapshot.alerts.isEmpty()) item { Text("Quiet. Nothing needs you.") }
                    items(snapshot.alerts.size) { index -> AlertChip(snapshot.alerts[index].first, snapshot.alerts[index].second, ::refresh) }
                } else {
                    if (snapshot.boxes.isEmpty()) item { Text("Sync boxes from phone") }
                    items(snapshot.boxes.size) { index ->
                        val box = snapshot.boxes[index]
                        val state = snapshot.states[box.url]
                        Chip(onClick = ::refresh, label = { Text(box.name) }, secondaryLabel = { Text("cpu ${state?.health?.cpu?.toInt() ?: 0}% / agents ${state?.agents?.size ?: 0}") })
                    }
                    item { Button(onClick = ::refresh) { Text("Refresh") } }
                }
            }
        }
    }
}

@Composable
private fun AlertChip(box: WearBox, alert: Alert, refresh: () -> Unit) {
    val scope = rememberCoroutineScope()
    Chip(
        onClick = {
            val action = alert.actions.firstOrNull()
            if (action != null) scope.launch { dev.wicolian.boxdeck.core.BoxdeckClient(box.url, box.token).rawPost(action.path, action.body); refresh() }
        },
        label = { Text(alert.title.ifBlank { "Alert" }) },
        secondaryLabel = { Text(alert.severity) }
    )
}
