package dev.wicolian.boxdeck

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

private val Hull = Color(0xFF0B0E13)
private val Deck = Color(0xFF10141B)
private val Ink = Color(0xFFE7EBF0)
private val InkMuted = Color(0xFF9AA5B1)
val Amber = Color(0xFFE8A33D)
val Moss = Color(0xFF75B798)
val Rust = Color(0xFFD26A5C)

private val BridgeColors = darkColorScheme(
    primary = Amber,
    onPrimary = Hull,
    secondary = Moss,
    error = Rust,
    background = Hull,
    onBackground = Ink,
    surface = Deck,
    onSurface = Ink,
    onSurfaceVariant = InkMuted,
    outline = Color(0xFF2A3441)
)

@Composable
fun BoxdeckTheme(content: @Composable () -> Unit) {
    MaterialTheme(colorScheme = BridgeColors, content = content)
}
