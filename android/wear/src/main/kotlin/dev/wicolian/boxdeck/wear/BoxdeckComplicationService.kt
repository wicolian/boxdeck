package dev.wicolian.boxdeck.wear

import androidx.wear.watchface.complications.data.ComplicationData
import androidx.wear.watchface.complications.data.PlainComplicationText
import androidx.wear.watchface.complications.data.ShortTextComplicationData
import androidx.wear.watchface.complications.data.ComplicationType
import androidx.wear.watchface.complications.datasource.ComplicationRequest
import androidx.wear.watchface.complications.datasource.SuspendingComplicationDataSourceService

class BoxdeckComplicationService : SuspendingComplicationDataSourceService() {
    override fun getPreviewData(type: ComplicationType): ComplicationData? = null

    override suspend fun onComplicationRequest(request: ComplicationRequest): ComplicationData? {
        val count = WearStore(applicationContext).refresh().alerts.size
        val text = PlainComplicationText.Builder(count.toString()).build()
        return ShortTextComplicationData.Builder(text, PlainComplicationText.Builder("boxdeck alerts").build()).build()
    }
}
