import Foundation
import SwiftUI

@main
struct BoxdeckBarApp: App {
    @StateObject private var model = AppModel()

    init() {
        guard CommandLine.arguments.contains("--print") else { return }
        Task { @MainActor in
            let config = (try? ConfigStore().load()) ?? .default
            let snapshot = await FleetLoader.load(config: config)
            let localData = await Task.detached(priority: .utility) { try? LocalUsageReader.readData() }.value
            let localUsage = localData.flatMap { try? JSONDecoder().decode(UsageResponse.self, from: $0) }
            let menu = MenuModelBuilder.build(boxes: snapshot.boxes, alerts: snapshot.alerts, localUsage: localUsage)
            print(MenuModelBuilder.renderText(menu))
            exit(0)
        }
    }

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
