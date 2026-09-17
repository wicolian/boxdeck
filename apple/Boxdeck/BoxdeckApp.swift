import SwiftUI

@main
struct BoxdeckApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup { ContentView().environmentObject(model).preferredColorScheme(.dark) }
    }
}
