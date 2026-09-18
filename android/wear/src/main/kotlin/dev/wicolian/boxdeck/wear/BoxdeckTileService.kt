package dev.wicolian.boxdeck.wear

import androidx.wear.protolayout.LayoutElementBuilders.Column
import androidx.wear.protolayout.LayoutElementBuilders.LayoutElement
import androidx.wear.protolayout.LayoutElementBuilders.Text
import androidx.wear.tiles.RequestBuilders.TileRequest
import androidx.wear.tiles.TileService
import androidx.wear.tiles.TileBuilders.Tile
import androidx.wear.protolayout.TimelineBuilders.Timeline
import androidx.wear.tiles.tile
import androidx.concurrent.futures.CallbackToFutureAdapter
import com.google.common.util.concurrent.ListenableFuture
import kotlinx.coroutines.runBlocking

class BoxdeckTileService : TileService() {
    override fun onTileRequest(requestParams: TileRequest): ListenableFuture<Tile> {
        val snapshot = runBlocking { WearStore(applicationContext).refresh() }
        val count = snapshot.alerts.size
        val box = snapshot.boxes.firstOrNull()
        val state = box?.let { snapshot.states[it.url] }
        val content: LayoutElement = Column.Builder()
            .addContent(Text.Builder().setText("Needs you: $count").build())
            .addContent(Text.Builder().setText(box?.name ?: "No box").build())
            .addContent(Text.Builder().setText("cpu ${state?.health?.cpu?.toInt() ?: 0}% / agents ${state?.agents?.size ?: 0}").build())
            .build()
        val result = tile(Timeline.fromLayoutElement(content), freshness = kotlin.time.Duration.parse("15m"), resourcesVersion = "1")
        return CallbackToFutureAdapter.getFuture { completer -> completer.set(result); "boxdeck-tile" }
    }
}
