import SwiftUI
import BoxdeckKit

@main
struct BoxdeckWatchApp: App {
    @StateObject private var model = WatchModel()
    var body: some Scene { WindowGroup { WatchRootView().environmentObject(model).preferredColorScheme(.dark) } }
}

struct WatchRootView: View {
    @EnvironmentObject private var model: WatchModel
    var body: some View {
        TabView {
            WatchNeedsYouView().tabItem { Label("Needs You", systemImage: "bell") }
            WatchBoxesView().tabItem { Label("Boxes", systemImage: "server.rack") }
        }.tint(.orange).task { await model.refresh() }
    }
}

struct WatchNeedsYouView: View {
    @EnvironmentObject private var model: WatchModel
    var body: some View {
        NavigationStack { ScrollView { VStack(spacing: 8) { if model.alerts.isEmpty { Text("Quiet. Nothing needs you.").font(.headline).padding(.vertical, 24) }; ForEach(model.alerts) { alert in VStack(alignment: .leading, spacing: 6) { Text(alert.title).font(.headline); Text(alert.box).font(.caption).foregroundStyle(.secondary); HStack { ForEach(alert.actions.prefix(2)) { action in Button(action.label) { Task { await model.action(action, alert: alert) } }.buttonStyle(.borderedProminent) }; Button("Snooze") { Task { await model.snooze(alert) } }.buttonStyle(.bordered) } }.padding(8).frame(maxWidth: .infinity, alignment: .leading).background(Color.black.opacity(0.2)).clipShape(RoundedRectangle(cornerRadius: 7)) } }.padding(8) }.refreshable { await model.refresh() }.navigationTitle("Needs You") }
    }
}

struct WatchBoxesView: View {
    @EnvironmentObject private var model: WatchModel
    var body: some View { NavigationStack { List(model.boxes) { box in VStack(alignment: .leading, spacing: 4) { HStack { Text(box.name).font(.headline); Spacer(); Circle().fill(box.ok ? .green : .red).frame(width: 7, height: 7) }; Text("CPU \(Int(box.health.cpu))%  mem \(memory(box.health))").font(.system(.caption, design: .monospaced)); Text("\(box.agents) agents  quota \(quota(box))").font(.caption).foregroundStyle(.secondary) } }.navigationTitle("Boxes") } }
    private func quota(_ box: BoxSnapshot) -> String { box.usage.quota.compactMap { _, value in value?.fiveHour.map { "\(Int($0.pct))%" } }.first ?? "n/a" }
}
