package dev.wicolian.boxdeck.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.wicolian.boxdeck.MainViewModel
import dev.wicolian.boxdeck.PhoneTab

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MainScreen(viewModel: MainViewModel) {
    val state by viewModel.ui.collectAsStateWithLifecycle()
    val selectedDevice = state.selectedDevice
    var detailTab by remember { mutableStateOf(BoxDetailTab.OVERVIEW) }
    Scaffold(
        topBar = { TopAppBar(title = { Text(state.selectedDevice?.name ?: state.tab.label) }) },
        bottomBar = {
            NavigationBar {
                PhoneTab.entries.forEach { tab ->
                    NavigationBarItem(
                        selected = state.tab == tab && state.selectedDevice == null,
                        onClick = { viewModel.clearSelectedDevice(); viewModel.setTab(tab) },
                        icon = { Text(tab.label.take(1)) },
                        label = { Text(tab.label) }
                    )
                }
            }
        }
    ) { padding ->
        PullToRefreshBox(
            isRefreshing = state.refreshing,
            onRefresh = viewModel::refresh,
            modifier = Modifier.fillMaxSize().padding(padding)
        ) {
            when {
                selectedDevice != null && selectedDevice.isDeck -> BoxDetailScreen(selectedDevice, state.detail, detailTab, { detailTab = it }, viewModel)
                state.tab == PhoneTab.NEEDS -> AlertScreen(state.alerts, viewModel)
                state.tab == PhoneTab.BOXES -> BoxesScreen(state.fleet, viewModel)
                state.tab == PhoneTab.USAGE -> UsageScreen(state.usage)
                state.tab == PhoneTab.SETTINGS -> SettingsScreen(state, viewModel)
            }
        }
    }
}

@Composable
fun LoadingOrError(loading: Boolean, error: String, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth().padding(24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (loading) LinearProgressIndicator(Modifier.fillMaxWidth())
        if (error.isNotBlank()) Text(error)
    }
}

@Composable
fun EmptyState(text: String) {
    Column(Modifier.fillMaxSize().padding(32.dp), verticalArrangement = Arrangement.Center) { Text(text) }
}

@Composable
fun MetricLine(label: String, value: String) {
    Row(Modifier.fillMaxWidth().padding(vertical = 3.dp), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(label)
        Text(value, fontFamily = FontFamily.Monospace)
    }
}

@Composable
fun SectionCard(title: String, content: @Composable () -> Unit) {
    Card(Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 6.dp)) {
        Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(title)
            content()
        }
    }
}

@Composable
fun ColumnList(content: @Composable (PaddingValues) -> Unit) {
    LazyColumn(contentPadding = PaddingValues(vertical = 8.dp), modifier = Modifier.fillMaxSize()) { item { content(PaddingValues()) } }
}
