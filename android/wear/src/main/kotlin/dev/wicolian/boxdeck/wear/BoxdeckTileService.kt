package dev.wicolian.boxdeck.wear

import androidx.wear.protolayout.LayoutElementBuilders.Column
import androidx.wear.protolayout.LayoutElementBuilders.LayoutElement
import androidx.wear.protolayout.LayoutElementBuilders.Text
import androidx.wear.tiles.RequestBuilders.TileRequest
import androidx.wear.tiles.ResourceBuilders.Resources
import androidx.wear.tiles.TileService
import androidx.wear.tiles.TileBuilders.Tile
import androidx.wear.protolayout.TimelineBuilders.Timeline
import androidx.wear.tiles.tile
import com.google.common.util.concurrent.Futures
import com.google.common.util.concurrent.ListenableFuture
import kotlinx.coroutines.Dispatchers

class BoxdeckTileService : TileService() {
    override fun onTileRequest(requestParams: TileRequest): ListenableFuture<Tile> {
        val snapshot = WearStore(applicationContext).refresh()
        val count = snapshot.alerts.size
        val box = snapshot.boxes.firstOrNull()
        val state = box?.let { snapshot.states[it.url] }
        val content: LayoutElement = Column.Builder()
            .addContent(Text.Builder().setText("Needs you: $count").build())
            .addContent(Text.Builder().setText(box?.name ?: "No box").build())
            .addContent(Text.Builder().setText("cpu ${state?.health?.cpu?.toInt() ?: 0}% / agents ${state?.agents?.size ?: 0}").build())
            .build()
        return Futures.immediateFuture(tile(Timeline.fromLayoutElement(content), freshness = kotlin.time.Duration.parse("15m"), resourcesVersion = "1"))
    }

    override fun onTileResourcesRequest(requestParams: androidx.wear.tiles.RequestBuilders.ResourcesRequest): ListenableFuture<Resources> {
        return Futures.immediateFuture(Resources.Builder().setVersion(requestParams.version).build())
    }
}
