package dev.wicolian.boxdeck.ui

import android.webkit.CookieManager
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import java.net.URLEncoder
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView

@Composable
fun WebViewScreen(baseUrl: String, path: String) {
    var user by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var loggedIn by remember { mutableStateOf(false) }
    var webView by remember { mutableStateOf<WebView?>(null) }
    Column(Modifier.fillMaxSize(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        AndroidView(
            modifier = Modifier.weight(1f),
            factory = { context ->
                WebView(context).apply {
                    webView = this
                    settings.javaScriptEnabled = true
                    settings.domStorageEnabled = true
                    settings.cacheMode = WebSettings.LOAD_DEFAULT
                    CookieManager.getInstance().setAcceptCookie(true)
                    webViewClient = object : WebViewClient() {
                        override fun onPageFinished(view: WebView?, url: String?) {
                            loggedIn = url?.contains(path) == true
                        }
                    }
                    loadUrl(baseUrl.trimEnd('/') + "/login?next=" + path)
                }
            },
            update = { view ->
                webView = view
                if (view.url?.contains(path) != true && view.url?.contains("/login") != true) view.loadUrl(baseUrl.trimEnd('/') + "/login?next=" + path)
            }
        )
        if (!loggedIn) {
            Column(Modifier.padding(horizontal = 12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text("Sign in to this deck")
                OutlinedTextField(user, { user = it }, label = { Text("User") })
                OutlinedTextField(password, { password = it }, label = { Text("Password") })
                Button(onClick = {
                    val form = "user=${URLEncoder.encode(user, "UTF-8")}&password=${URLEncoder.encode(password, "UTF-8")}&next=${URLEncoder.encode(path, "UTF-8")}".toByteArray()
                    webView?.postUrl(baseUrl.trimEnd('/') + "/login", form)
                }) { Text("Sign in") }
            }
        }
    }
}
