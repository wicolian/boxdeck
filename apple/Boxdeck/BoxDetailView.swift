import SwiftUI

struct BoxDetailView: View {
    @EnvironmentObject private var model: AppModel
    let device: AppModel.DisplayDevice
    @State private var segment = "Overview"
    @State private var state = BoxState()
    @State private var apps: [AppRecord] = []
    @State private var usage: UsageResponse?
    @State private var selectedAgent: Agent?
    let segments = ["Overview", "Agents", "Ports", "Usage", "Apps", "Terminal", "Files"]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Picker("Box screen", selection: $segment) { ForEach(segments, id: \.self) { Text($0).tag($0) } }.pickerStyle(.segmented).accessibilityLabel("Box section")
                if segment == "Overview" { OverviewSegment(state: state, usage: usage) }
                if segment == "Agents" { AgentsSegment(client: model.client(for: device), state: $state, selected: $selectedAgent) }
                if segment == "Ports" { PortsSegment(ports: state.ports) }
                if segment == "Usage" { UsageSegment(usage: usage) }
                if segment == "Apps" { AppsSegment(client: model.client(for: device), apps: $apps) }
                if segment == "Terminal" { BoxWebView(url: device.url + "/term/", token: model.store.token(for: device.url) ?? "") }
                if segment == "Files" { BoxWebView(url: device.url + "/files/", token: model.store.token(for: device.url) ?? "") }
            }.padding()
        }
        .navigationTitle(device.name)
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        guard let client = model.client(for: device) else { return }
        state = (try? await client.state()) ?? state
        usage = try? await client.usage(days: 30)
        apps = (try? await client.apps()) ?? []
    }
}

struct OverviewSegment: View {
    let state: BoxState
    let usage: UsageResponse?
    var body: some View { VStack(spacing: 12) { BridgeCard { VStack(alignment: .leading, spacing: 8) { Text(state.host.isEmpty ? "Health" : state.host).font(.headline); MonoText(value: "CPU \(Int(state.health.cpu))%  load \(String(format: "%.2f", state.health.load1))"); MonoText(value: "Memory \(memory(state.health))"); Text("\(state.agents.count) agents  \(state.ports.count) ports").foregroundStyle(.secondary) } }; QuotaView(usage: usage) } }
}

struct AgentsSegment: View {
    let client: BoxdeckClient?
    @Binding var state: BoxState
    @Binding var selected: Agent?
    @State private var prompt = ""
    var body: some View {
        LazyVStack(spacing: 10) { ForEach(state.agents) { agent in BridgeCard { VStack(alignment: .leading, spacing: 8) { HStack { Text(agent.kind).font(.caption).foregroundStyle(.secondary); Text(agent.title.isEmpty ? agent.cwd : agent.title).font(.headline); Spacer(); Text(agent.status).font(.caption).foregroundStyle(agent.status == "needs_you" ? BridgeTheme.amber : .secondary) }; if !agent.paneID.isEmpty { Button("Read live tail") { selected = agent }; if selected?.id == agent.id { PaneTailView(client: client, agent: agent) } } } } }; if state.agents.isEmpty { Text("No agents are running.").foregroundStyle(.secondary) } }
    }
}

struct PaneTailView: View {
    let client: BoxdeckClient?
    let agent: Agent
    @State private var tail = "Loading live tail..."
    @State private var prompt = ""
    var body: some View { VStack(alignment: .leading, spacing: 8) { Text(tail).font(.system(.caption, design: .monospaced)).frame(maxWidth: .infinity, alignment: .leading).padding(10).background(BridgeTheme.hull).textSelection(.enabled); HStack { TextField("Prompt this agent", text: $prompt); Button("Send") { Task { guard let client, !prompt.isEmpty else { return }; try? await client.prompt(pane: agent.paneID, text: prompt); prompt = "" } }.buttonStyle(.borderedProminent) }; HStack { Button("Approve") { Task { try? await client?.sendKeys(pane: agent.paneID, keys: "Enter") } }; Button("Yes") { Task { try? await client?.sendKeys(pane: agent.paneID, keys: "y") } }; Button("Interrupt") { Task { try? await client?.interrupt(pane: agent.paneID) } } }.buttonStyle(.bordered) }.task { tail = (try? await client?.paneTail(agent.paneID)) ?? "Tail unavailable" } }
}

struct PortsSegment: View {
    let ports: [Port]
    var body: some View { LazyVStack(spacing: 10) { ForEach(ports) { port in if let url = URL(string: port.url), !port.url.isEmpty { Link(destination: url) { BridgeCard { HStack { Text("\(port.port)").font(.system(.body, design: .monospaced)); Text(port.label.isEmpty ? port.title : port.label); Spacer(); Text("Open").font(.caption).foregroundStyle(BridgeTheme.amber) } } } } } } }
}

struct UsageSegment: View {
    let usage: UsageResponse?
    var body: some View { VStack(spacing: 12) { ForEach((usage?.providers.keys.sorted() ?? []), id: \.self) { provider in if let row = usage?.providers[provider] { BridgeCard { VStack(alignment: .leading, spacing: 8) { Text(provider.capitalized).font(.headline); Text("\(row.today.tokens.total) tokens").font(.system(.body, design: .monospaced)); Text("$\(row.today.costUSD, specifier: "%.4f") estimate at list price").font(.caption).foregroundStyle(.secondary); UsageBars(days: row.days) } } } }; if usage == nil { Text("Usage is not available yet.").foregroundStyle(.secondary) } } }
}

struct UsageBars: View {
    let days: [UsageDay]
    var body: some View { HStack(alignment: .bottom, spacing: 3) { ForEach(days.suffix(30), id: \.day) { day in let value = max(1, day.tokens.total); Rectangle().fill(BridgeTheme.amber).frame(maxWidth: .infinity, minHeight: 2, maxHeight: CGFloat(min(110, value > 0 ? 10 + log10(Double(value)) * 18 : 2))).accessibilityLabel("\(day.day) \(value) tokens") } }.frame(height: 120, alignment: .bottom) }
}

struct AppsSegment: View {
    let client: BoxdeckClient?
    @Binding var apps: [AppRecord]
    var body: some View { LazyVStack(spacing: 10) { ForEach(apps) { app in BridgeCard { HStack { VStack(alignment: .leading) { Text(app.name).font(.headline); Text(app.status).font(.caption).foregroundStyle(.secondary) }; Spacer(); if app.running { Button("Stop") { Task { try? await client?.appAction(id: app.id, action: "stop"); apps = (try? await client?.apps()) ?? apps } }.buttonStyle(.bordered) } else if app.canStart { Button("Start") { Task { try? await client?.appAction(id: app.id, action: "start"); apps = (try? await client?.apps()) ?? apps } }.buttonStyle(.borderedProminent) } } } } } }
}

struct QuotaView: View {
    let usage: UsageResponse?
    var body: some View { ForEach((usage?.providers.keys.sorted() ?? []), id: \.self) { provider in if let quota = usage?.providers[provider]?.quota { HStack { Text(provider.capitalized); Spacer(); if let five = quota.fiveHour { Text("5h \(Int(five.pct))%") }; if let seven = quota.sevenDay { Text("7d \(Int(seven.pct))%") } }.font(.system(.caption, design: .monospaced)) } } }
}
