package dev.wicolian.boxdeck

import android.app.Application
import dev.wicolian.boxdeck.notifications.NotificationChannels

class BoxdeckApplication : Application() {
    lateinit var boxStore: BoxStore
        private set

    override fun onCreate() {
        super.onCreate()
        boxStore = BoxStore(this)
        NotificationChannels.create(this)
    }
}
