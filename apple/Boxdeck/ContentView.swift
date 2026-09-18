import SwiftUI

struct ContentView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        TabView {
            NeedsYouView().tabItem { Label("Needs You", systemImage: "bell") }
            BoxesView().tabItem { Label("Boxes", systemImage: "server.rack") }
            UsageView().tabItem { Label("Usage", systemImage: "chart.bar") }
            SettingsView().tabItem { Label("Settings", systemImage: "gearshape") }
        }
        .tint(BridgeTheme.amber)
        .background(BridgeTheme.hull)
        .task { await model.startPushRegistration(); await model.refresh() }
        .onOpenURL { url in model.handle(url) }
    }
}

struct NeedsYouView: View {
    @EnvironmentObject private var model: AppModel
    var body: some View {
        NavigationStack {
            ScrollView {
                LazyVStack(spacing: 12) {
                    if model.alerts.isEmpty {
                        BridgeCard { VStack(alignment: .leading, spacing: 8) { Text("Quiet. Nothing needs you.").font(.title3); Text("Alerts from your boxes will appear here.").foregroundStyle(.secondary) } }
                    } else {
                        ForEach(model.alerts) { alert in AlertCard(alert: alert) }
                    }
                    if !model.message.isEmpty { Text(model.message).font(.footnote).foregroundStyle(.secondary).frame(maxWidth: .infinity, alignment: .leading) }
                }.padding()
            }
            .refreshable { await model.refresh() }
            .navigationTitle("Needs You")
        }
    }
}

struct AlertCard: View {
    @EnvironmentObject private var model: AppModel
    let alert: Alert
    var body: some View {
        BridgeCard {
            VStack(alignment: .leading, spacing: 10) {
                HStack { Circle().fill(alert.severity == "critical" ? BridgeTheme.rust : BridgeTheme.amber).frame(width: 8, height: 8); Text(alert.title).font(.headline); Spacer(); Text(alert.box).font(.caption).foregroundStyle(.secondary) }
                Text(alert.body).font(.subheadline).foregroundStyle(.secondary)
                HStack { ForEach(alert.actions.prefix(4)) { action in Button(action.label) { Task { await model.action(action, for: alert) } }.buttonStyle(.borderedProminent) }; Spacer() }
                HStack { Button("Ack") { Task { await model.acknowledge(alert) } }.buttonStyle(.bordered); Button("Snooze") { Task { await model.snooze(alert) } }.buttonStyle(.bordered); Button("Resolve") { Task { await model.resolve(alert) } }.buttonStyle(.bordered) }
            }
        }
    }
}

struct BoxesView: View {
    @EnvironmentObject private var model: AppModel
    var body: some View {
        NavigationStack {
            ScrollView {
                LazyVStack(spacing: 12) {
                    ForEach(model.devices) { device in
                        if let deck = device.deck { NavigationLink { BoxDetailView(device: device) } label: { DeckCard(device: device, deck: deck) } }
                        else { PeerCard(device: device) }
                    }
                    if model.devices.isEmpty { BridgeCard { Text("No boxes are configured. Add one in Settings.").foregroundStyle(.secondary) } }
                }.padding()
            }.refreshable { await model.refresh() }.navigationTitle("Boxes")
        }
    }
}

struct DeckCard: View {
    let device: AppModel.DisplayDevice
    let deck: BoxSnapshot
    var body: some View {
        BridgeCard { VStack(alignment: .leading, spacing: 8) { HStack { Text(deck.name).font(.headline); Spacer(); Text(deck.ok ? "online" : "offline").foregroundStyle(deck.ok ? BridgeTheme.moss : BridgeTheme.rust).font(.caption) }; MonoText(value: "CPU \(Int(deck.health.cpu))%  mem \(memory(deck.health))"); Text("\(deck.agents) agents  \(deck.ports) ports").font(.caption).foregroundStyle(.secondary); HStack { ForEach(deck.usage.quota.keys.sorted(), id: \.self) { provider in Text(provider).font(.caption2).foregroundStyle(BridgeTheme.amber) } } }
        }
    }
}

struct PeerCard: View {
    let device: AppModel.DisplayDevice
    var body: some View { BridgeCard { HStack { Circle().fill(device.online ? BridgeTheme.moss : BridgeTheme.rust).frame(width: 9, height: 9); VStack(alignment: .leading) { Text(device.name).font(.headline); Text(device.peer?.os.isEmpty == false ? device.peer!.os : "tailnet device").font(.caption).foregroundStyle(.secondary); Text(device.peer?.lastSeen.isEmpty == false ? "last seen \(device.peer!.lastSeen)" : "No boxdeck installed").font(.caption2).foregroundStyle(.secondary) }; Spacer(); Text(device.peer?.boxdeck == true ? "boxdeck" : "install boxdeck").font(.caption).foregroundStyle(BridgeTheme.amber) } }
    }
}

func memory(_ health: HealthSnapshot) -> String { health.memTotal == 0 ? "n/a" : String(format: "%.1f/%.1f GB", Double(health.memUsed) / 1_073_741_824, Double(health.memTotal) / 1_073_741_824) }
