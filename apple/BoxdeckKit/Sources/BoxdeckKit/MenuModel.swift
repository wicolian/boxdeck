import Foundation

public struct MenuBox: Sendable, Hashable, Identifiable {
    public var name: String
    public var url: String
    public var reachable: Bool
    public var cpu: Double
    public var memory: String
    public var agents: String
    public var ports: Int
    public var quota: [String]
    public var id: String { url.isEmpty ? name : url }
    public init(name: String, url: String, reachable: Bool, cpu: Double, memory: String, agents: String, ports: Int, quota: [String]) { self.name = name; self.url = url; self.reachable = reachable; self.cpu = cpu; self.memory = memory; self.agents = agents; self.ports = ports; self.quota = quota }
}

public struct MenuModel: Sendable {
    public var boxes: [MenuBox]
    public var openAlerts: [Alert]
    public var localUsage: UsageResponse?
    public init(boxes: [MenuBox] = [], openAlerts: [Alert] = [], localUsage: UsageResponse? = nil) { self.boxes = boxes; self.openAlerts = openAlerts; self.localUsage = localUsage }
}

public enum MenuModelBuilder {
    public static func build(boxes: [BoxSnapshot], states: [String: BoxState] = [:], alerts: [Alert] = [], usage: UsageResponse? = nil) -> MenuModel {
        MenuModel(boxes: boxes.map { box in
            let state = states[box.url]
            let working = state?.agents.filter { $0.status == "working" }.count ?? box.agents
            let needs = state?.agents.filter { ["needs_you", "needs-you", "waiting", "blocked"].contains($0.status) }.count ?? 0
            let memory = box.health.memTotal == 0 ? "memory unavailable" : "\(formatGB(box.health.memUsed))/\(formatGB(box.health.memTotal)) GB"
            var quota: [String] = []
            for (provider, value) in box.usage.quota {
                guard let value else { continue }
                let five = value.fiveHour.map { "\(provider) 5h \(Int($0.pct))%" }
                let seven = value.sevenDay.map { "7d \(Int($0.pct))%" }
                quota.append([five, seven].compactMap { $0 }.joined(separator: " "))
            }
            return MenuBox(name: box.name, url: box.url, reachable: box.ok, cpu: box.health.cpu, memory: memory, agents: "\(working) working, \(needs) needs you", ports: box.ports, quota: quota)
        }, openAlerts: alerts.filter { $0.state == "open" }, localUsage: usage)
    }

    private static func formatGB(_ bytes: UInt64) -> String { String(format: "%.1f", Double(bytes) / 1_073_741_824) }
}
