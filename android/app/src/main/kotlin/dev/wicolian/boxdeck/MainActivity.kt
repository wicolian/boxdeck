package dev.wicolian.boxdeck

import android.Manifest
import android.content.Intent
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.wicolian.boxdeck.notifications.AlertNotifications
import dev.wicolian.boxdeck.ui.MainScreen
import org.unifiedpush.android.connector.UnifiedPush

class MainActivity : ComponentActivity() {
    private val viewModel: MainViewModel by viewModels {
        MainViewModel.factory(applicationContext, (application as BoxdeckApplication).boxStore)
    }
    private val notificationPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        viewModel.handleDeepLink(intent?.data)
        if (Build.VERSION.SDK_INT >= 33) notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        UnifiedPush.tryUseCurrentOrDefaultDistributor(this) { success ->
            if (success) UnifiedPush.register(this, messageForDistributor = "boxdeck alerts")
        }
        viewModel.onNewAlert = { url, alert ->
            if (viewModel.ui.value.notifications) AlertNotifications.show(this, url, alert)
        }
        setContent {
            BoxdeckTheme {
                val storeBoxes by (application as BoxdeckApplication).boxStore.boxes.collectAsStateWithLifecycle()
                LaunchedEffect(Unit) { viewModel.startEvents() }
                LaunchedEffect(storeBoxes) { WearSync.publish(this@MainActivity, storeBoxes) }
                MainScreen(viewModel)
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        viewModel.handleDeepLink(intent.data)
    }
}
