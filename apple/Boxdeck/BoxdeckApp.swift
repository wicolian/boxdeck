import SwiftUI

@main
struct BoxdeckApp: App {
    @UIApplicationDelegateAdaptor(BoxdeckAppDelegate.self) private var appDelegate
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup { ContentView().environmentObject(model).preferredColorScheme(.dark) }
    }
}
