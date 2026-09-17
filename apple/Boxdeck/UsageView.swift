import SwiftUI
import BoxdeckKit

struct UsageView: View {
    @EnvironmentObject private var model: AppModel
    var body: some View {
        NavigationStack { ScrollView { VStack(spacing: 12) { if let usage = model.usage { ForEach(usage.providers.keys.sorted(), id: \.self) { provider in if let row = usage.providers[provider] { BridgeCard { VStack(alignment: .leading, spacing: 8) { Text(provider.capitalized).font(.headline); MonoText(value: "\(row.today.tokens.total) tokens"); Text("$\(row.today.costUSD, specifier: "%.4f") estimate at list price").font(.caption).foregroundStyle(.secondary); UsageBars(days: row.days) } } } }; if !usage.boxes.isEmpty { Text("Across devices").font(.headline).frame(maxWidth: .infinity, alignment: .leading); ForEach(usage.boxes) { box in HStack { Text(box.name); Spacer(); Text(box.ok ? "reachable" : "unreachable").foregroundStyle(box.ok ? BridgeTheme.moss : BridgeTheme.rust) }.padding(.horizontal) } } } else { ProgressView("Loading usage") } }.padding() }.refreshable { await model.refresh() }.navigationTitle("Usage") }
    }
}
