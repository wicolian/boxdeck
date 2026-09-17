package dev.wicolian.boxdeck.wear

import androidx.wear.protolayout.LayoutElementBuilders.Column
import androidx.wear.protolayout.LayoutElementBuilders.Text
import androidx.wear.protolayout.material3.MaterialScope
import androidx.wear.tiles.Material3TileService
import androidx.wear.tiles.RequestBuilders.TileRequest
import androidx.wear.tiles.TileBuilders.Tile
import androidx.wear.tiles.TimelineBuilders.Timeline
import androidx.wear.tiles.tile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlin.time.Duration.Companion.minutes

class BoxdeckTileService : Material3TileService() {
    override suspend fun MaterialScope.tileResponse(requestParams: TileRequest): Tile = withContext(Dispatchers.IO) {
        val snapshot = WearStore(applicationContext).refresh()
        val count = snapshot.alerts.size
        val box = snapshot.boxes.firstOrNull()
        val state = box?.let { snapshot.states[it.url] }
        val content = Column.Builder()
            .addContent(Text.Builder().setText("Needs you: $count").build())
            .addContent(Text.Builder().setText(box?.name ?: "No box").build())
            .addContent(Text.Builder().setText("cpu ${state?.health?.cpu?.toInt() ?: 0}% / agents ${state?.agents?.size ?: 0}").build())
            .build()
        tile(Timeline.fromLayoutElement(content), freshness = 15.minutes, resourcesVersion = "1")
    }
}
