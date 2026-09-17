import SwiftUI

@main
struct BoxdeckBarApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        MenuBarExtra("boxdeck", systemImage: model.iconSystemName) {
            MenuPopover(model: model)
        }
        .menuBarExtraStyle(.window)

        Settings {
            SettingsView(model: model)
        }
    }
}
