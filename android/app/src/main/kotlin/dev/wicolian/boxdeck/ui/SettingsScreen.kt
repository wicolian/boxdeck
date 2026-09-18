package dev.wicolian.boxdeck.ui

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
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
import dev.wicolian.boxdeck.PhoneUiState

@Composable
fun SettingsScreen(state: PhoneUiState, viewModel: MainViewModel) {
    val context = LocalContext.current
    val uriHandler = LocalUriHandler.current
    var name by remember { mutableStateOf("") }
    var url by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") }
    var scanning by remember { mutableStateOf(false) }
    if (scanning) {
        QrScannerScreen(
            onCode = { code ->
                viewModel.handleDeepLink(Uri.parse(code))
                scanning = false
            },
            onClose = { scanning = false }
        )
        return
    }
    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text("Add a box")
        OutlinedTextField(name, { name = it }, Modifier.fillMaxWidth(), label = { Text("Name") })
        OutlinedTextField(url, { url = it }, Modifier.fillMaxWidth(), label = { Text("URL") })
        OutlinedTextField(token, { token = it }, Modifier.fillMaxWidth(), label = { Text("Bearer token") })
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Button(onClick = { viewModel.addBox(name, url, token); name = ""; url = ""; token = "" }) { Text("Add") }
            OutlinedButton(onClick = { scanning = true }) { Text("Scan QR") }
        }
        state.fleet.filter { it.isDeck }.forEach { box ->
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                Text(box.name)
                OutlinedButton(onClick = { viewModel.removeBox(box.url) }) { Text("Remove") }
            }
        }
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
            Text("Notifications")
            Switch(checked = state.notifications, onCheckedChange = { viewModel.updateSettings(it, state.quietFrom, state.quietTo, state.disarmed) })
        }
        Text("Quiet hours")
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedTextField(state.quietFrom, { viewModel.updateSettings(state.notifications, it, state.quietTo, state.disarmed) }, Modifier.weight(1f), label = { Text("From") })
            OutlinedTextField(state.quietTo, { viewModel.updateSettings(state.notifications, state.quietFrom, it, state.disarmed) }, Modifier.weight(1f), label = { Text("To") })
        }
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
            Text(if (state.disarmed) "Alerts disarmed" else "Alerts armed")
            Switch(checked = state.disarmed, onCheckedChange = { viewModel.updateSettings(state.notifications, state.quietFrom, state.quietTo, it) })
        }
        state.fleet.firstOrNull { it.isDeck }?.url?.let { deck ->
            OutlinedButton(onClick = { uriHandler.openUri(deck) }) { Text("Launch deck") }
        }
        Text("Tokens stay in encrypted app storage. Do not paste them into reports.")
    }
}
