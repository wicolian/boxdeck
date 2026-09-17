import WidgetKit
import SwiftUI
import BoxdeckKit

struct BoxdeckComplicationEntry: TimelineEntry {
    let date: Date
    let needsYou: Int
    let boxName: String
    let cpu: Int
    let agents: Int
}

struct BoxdeckComplicationProvider: TimelineProvider {
    func placeholder(in context: Context) -> BoxdeckComplicationEntry { BoxdeckComplicationEntry(date: Date(), needsYou: 1, boxName: "box", cpu: 12, agents: 2) }
    func getSnapshot(in context: Context, completion: @escaping (BoxdeckComplicationEntry) -> Void) { completion(placeholder(in: context)) }
    func getTimeline(in context: Context, completion: @escaping (Timeline<BoxdeckComplicationEntry>) -> Void) {
        let entry = placeholder(in: context)
        completion(Timeline(entries: [entry], policy: .after(Date().addingTimeInterval(15 * 60))))
    }
}

struct BoxdeckComplication: Widget {
    let kind = "BoxdeckComplication"
    var body: some WidgetConfiguration { StaticConfiguration(kind: kind, provider: BoxdeckComplicationProvider()) { entry in ComplicationView(entry: entry) }.configurationDisplayName("Boxdeck").description("Needs you and box health.") }
}

struct ComplicationView: View {
    @Environment(\.widgetFamily) var family
    let entry: BoxdeckComplicationEntry
    var body: some View { switch family { case .accessoryCircular: Text("\(entry.needsYou)").foregroundStyle(entry.needsYou > 0 ? .orange : .green); default: VStack(alignment: .leading) { Text(entry.boxName).bold(); Text("CPU \(entry.cpu)%  \(entry.agents) agents").font(.caption) } } }
}

@main
struct BoxdeckComplicationBundle: WidgetBundle { var body: some Widget { BoxdeckComplication() } }
