import Foundation

struct BoxConfig: Codable, Equatable {
    var name: String
    var url: String
    var token: String

    init(name: String = "", url: String = "", token: String = "") {
        self.name = name
        self.url = url
        self.token = token
    }
}

struct QuietHours: Codable, Equatable {
    var enabled: Bool
    var start: String
    var end: String

    init(enabled: Bool = false, start: String = "22:00", end: String = "07:00") {
        self.enabled = enabled
        self.start = start
        self.end = end
    }

    func contains(_ date: Date, calendar: Calendar = .current) -> Bool {
        guard enabled, let startMinutes = minutes(start), let endMinutes = minutes(end) else { return false }
        let components = calendar.dateComponents([.hour, .minute], from: date)
        let current = (components.hour ?? 0) * 60 + (components.minute ?? 0)
        if startMinutes == endMinutes { return true }
        if startMinutes < endMinutes {
            return current >= startMinutes && current < endMinutes
        }
        return current >= startMinutes || current < endMinutes
    }

    private func minutes(_ value: String) -> Int? {
        let parts = value.split(separator: ":", omittingEmptySubsequences: false)
        guard parts.count == 2, let hour = Int(parts[0]), let minute = Int(parts[1]), (0..<24).contains(hour), (0..<60).contains(minute) else {
            return nil
        }
        return hour * 60 + minute
    }
}

struct BarConfig: Codable, Equatable {
    var boxes: [BoxConfig]
    var fleetToken: String
    var refreshSec: Int
    var openWith: String
    var notify: Bool
    var quiet: QuietHours?

    static let `default` = BarConfig(boxes: [], fleetToken: "", refreshSec: 30, openWith: "browser", notify: false, quiet: nil)

    init(boxes: [BoxConfig], fleetToken: String, refreshSec: Int, openWith: String, notify: Bool, quiet: QuietHours? = nil) {
        self.boxes = boxes
        self.fleetToken = fleetToken
        self.refreshSec = refreshSec
        self.openWith = openWith
        self.notify = notify
        self.quiet = quiet
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        boxes = try container.decodeIfPresent([BoxConfig].self, forKey: .boxes) ?? []
        fleetToken = try container.decodeIfPresent(String.self, forKey: .fleetToken) ?? ""
        refreshSec = try container.decodeIfPresent(Int.self, forKey: .refreshSec) ?? 30
        openWith = try container.decodeIfPresent(String.self, forKey: .openWith) ?? "browser"
        notify = try container.decodeIfPresent(Bool.self, forKey: .notify) ?? false
        if let value = try? container.decode(QuietHours.self, forKey: .quiet) {
            quiet = value
        } else if let enabled = try? container.decode(Bool.self, forKey: .quiet) {
            quiet = QuietHours(enabled: enabled)
        } else {
            quiet = nil
        }
    }

    private enum CodingKeys: String, CodingKey {
        case boxes, fleetToken, refreshSec, openWith, notify, quiet
    }
}

struct Health: Codable, Equatable {
    var cpu: Double = 0
    var memUsed: Double = 0
    var memTotal: Double = 0
    var load1: Double = 0

    init(cpu: Double = 0, memUsed: Double = 0, memTotal: Double = 0, load1: Double = 0) {
        self.cpu = cpu
        self.memUsed = memUsed
        self.memTotal = memTotal
        self.load1 = load1
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        cpu = try container.decodeIfPresent(Double.self, forKey: .cpu) ?? 0
        memUsed = try container.decodeIfPresent(Double.self, forKey: .memUsed) ?? 0
        memTotal = try container.decodeIfPresent(Double.self, forKey: .memTotal) ?? 0
        load1 = try container.decodeIfPresent(Double.self, forKey: .load1) ?? 0
    }

    private enum CodingKeys: String, CodingKey {
        case cpu, memUsed, memTotal, load1
    }
}

struct Agent: Codable, Equatable {
    var kind: String = ""
    var status: String = ""

    init(kind: String = "", status: String = "") {
        self.kind = kind
        self.status = status
    }

    enum CodingKeys: String, CodingKey {
        case kind
        case status = "agent_status"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        kind = try container.decodeIfPresent(String.self, forKey: .kind) ?? ""
        status = try container.decodeIfPresent(String.self, forKey: .status) ?? ""
    }
}

struct Port: Codable, Equatable {
    var port: Int = 0
}

struct BoxState: Codable, Equatable {
    var host: String = ""
    var health = Health()
    var agents: [Agent] = []
    var ports: [Port] = []

    init(host: String = "", health: Health = Health(), agents: [Agent] = [], ports: [Port] = []) {
        self.host = host
        self.health = health
        self.agents = agents
        self.ports = ports
    }

    enum CodingKeys: String, CodingKey {
        case host, health, agents, ports
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        host = try container.decodeIfPresent(String.self, forKey: .host) ?? ""
        health = try container.decodeIfPresent(Health.self, forKey: .health) ?? Health()
        agents = try container.decodeIfPresent([Agent].self, forKey: .agents) ?? []
        ports = try container.decodeIfPresent([Port].self, forKey: .ports) ?? []
    }
}

struct TokenTotals: Codable, Equatable {
    var input: Int64 = 0
    var cachedInput: Int64 = 0
    var cacheWrite: Int64 = 0
    var output: Int64 = 0

    var total: Int64 { input + cachedInput + cacheWrite + output }

    enum CodingKeys: String, CodingKey {
        case input = "in"
        case cachedInput = "cachedIn"
        case cacheWrite
        case output = "out"
    }

    init() {}

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        input = try container.decodeIfPresent(Int64.self, forKey: .input) ?? 0
        cachedInput = try container.decodeIfPresent(Int64.self, forKey: .cachedInput) ?? 0
        cacheWrite = try container.decodeIfPresent(Int64.self, forKey: .cacheWrite) ?? 0
        output = try container.decodeIfPresent(Int64.self, forKey: .output) ?? 0
    }
}

struct UsageSummary: Codable, Equatable {
    var tokens = TokenTotals()
    var costUSD: Double = 0

    enum CodingKeys: String, CodingKey {
        case tokens
        case costUSD = "costUsd"
    }

    init() {}

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        tokens = try container.decodeIfPresent(TokenTotals.self, forKey: .tokens) ?? TokenTotals()
        costUSD = try container.decodeIfPresent(Double.self, forKey: .costUSD) ?? 0
    }
}

struct QuotaWindow: Codable, Equatable {
    var pct: Double = 0
    var resetsAt: String = ""

    init() {}

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        pct = try container.decodeIfPresent(Double.self, forKey: .pct) ?? 0
        resetsAt = try container.decodeIfPresent(String.self, forKey: .resetsAt) ?? ""
    }

    private enum CodingKeys: String, CodingKey {
        case pct, resetsAt
    }
}

struct Quota: Codable, Equatable {
    var fiveHour: QuotaWindow?
    var sevenDay: QuotaWindow?
}

struct UsageModel: Codable, Equatable {
    var tokens = TokenTotals()
    var costUSD: Double = 0
    var priceSet: Bool = false

    enum CodingKeys: String, CodingKey {
        case tokens
        case costUSD = "costUsd"
        case priceSet
    }
}

struct UsageDay: Codable, Equatable {
    var day: String = ""
    var tokens = TokenTotals()
    var costUSD: Double = 0
    var models: [String: UsageModel] = [:]

    enum CodingKeys: String, CodingKey {
        case day, tokens
        case costUSD = "costUsd"
        case models
    }
}

struct UsageProvider: Codable, Equatable {
    var quota: Quota?
    var today = UsageSummary()
    var localDay: String = ""
    var days: [UsageDay] = []
    var sessions: Int = 0

    init(quota: Quota? = nil, today: UsageSummary = UsageSummary(), localDay: String = "", days: [UsageDay] = [], sessions: Int = 0) {
        self.quota = quota
        self.today = today
        self.localDay = localDay
        self.days = days
        self.sessions = sessions
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        quota = try container.decodeIfPresent(Quota.self, forKey: .quota)
        today = try container.decodeIfPresent(UsageSummary.self, forKey: .today) ?? UsageSummary()
        localDay = try container.decodeIfPresent(String.self, forKey: .localDay) ?? ""
        days = try container.decodeIfPresent([UsageDay].self, forKey: .days) ?? []
        sessions = try container.decodeIfPresent(Int.self, forKey: .sessions) ?? 0
    }

    private enum CodingKeys: String, CodingKey {
        case quota, today, localDay, days, sessions
    }
}

struct UsageResponse: Codable, Equatable {
    var providers: [String: UsageProvider] = [:]
    var device: String = ""
    var updatedAt: String = ""

    init(providers: [String: UsageProvider] = [:], device: String = "", updatedAt: String = "") {
        self.providers = providers
        self.device = device
        self.updatedAt = updatedAt
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        providers = try container.decodeIfPresent([String: UsageProvider].self, forKey: .providers) ?? [:]
        device = try container.decodeIfPresent(String.self, forKey: .device) ?? ""
        updatedAt = try container.decodeIfPresent(String.self, forKey: .updatedAt) ?? ""
    }

    private enum CodingKeys: String, CodingKey {
        case providers, device, updatedAt
    }
}

struct BoxUsageCard: Codable, Equatable {
    var today: [String: UsageSummary] = [:]
    var quota: [String: Quota?] = [:]

    init() {}

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        today = try container.decodeIfPresent([String: UsageSummary].self, forKey: .today) ?? [:]
        quota = try container.decodeIfPresent([String: Quota?].self, forKey: .quota) ?? [:]
    }

    private enum CodingKeys: String, CodingKey {
        case today, quota
    }
}

struct BoxCardResponse: Codable, Equatable {
    var name: String = ""
    var url: String = ""
    var local: Bool = false
    var ok: Bool = false
    var since: String = ""
    var discovered: Bool = false
    var tag: String = ""
    var health = Health()
    var agents: Int = 0
    var ports: Int = 0
    var usage = BoxUsageCard()

    init() {}

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? ""
        url = try container.decodeIfPresent(String.self, forKey: .url) ?? ""
        local = try container.decodeIfPresent(Bool.self, forKey: .local) ?? false
        ok = try container.decodeIfPresent(Bool.self, forKey: .ok) ?? false
        since = try container.decodeIfPresent(String.self, forKey: .since) ?? ""
        discovered = try container.decodeIfPresent(Bool.self, forKey: .discovered) ?? false
        tag = try container.decodeIfPresent(String.self, forKey: .tag) ?? ""
        health = try container.decodeIfPresent(Health.self, forKey: .health) ?? Health()
        agents = try container.decodeIfPresent(Int.self, forKey: .agents) ?? 0
        ports = try container.decodeIfPresent(Int.self, forKey: .ports) ?? 0
        usage = try container.decodeIfPresent(BoxUsageCard.self, forKey: .usage) ?? BoxUsageCard()
    }

    private enum CodingKeys: String, CodingKey {
        case name, url, local, ok, since, discovered, tag, health, agents, ports, usage
    }
}

struct UsageAllResponse: Codable, Equatable {
    var providers: [String: UsageProvider] = [:]
    var boxes: [BoxCardResponse] = []
    var updatedAt: String = ""

    init() {}

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        providers = try container.decodeIfPresent([String: UsageProvider].self, forKey: .providers) ?? [:]
        boxes = try container.decodeIfPresent([BoxCardResponse].self, forKey: .boxes) ?? []
        updatedAt = try container.decodeIfPresent(String.self, forKey: .updatedAt) ?? ""
    }

    private enum CodingKeys: String, CodingKey {
        case providers, boxes, updatedAt
    }
}

struct Peer: Codable, Equatable {
    var name: String = ""
    var url: String = ""
    var boxdeck: Bool = false

    init() {}

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? ""
        url = try container.decodeIfPresent(String.self, forKey: .url) ?? ""
        boxdeck = try container.decodeIfPresent(Bool.self, forKey: .boxdeck) ?? false
    }

    private enum CodingKeys: String, CodingKey {
        case name, url, boxdeck
    }
}

struct BoxSnapshot: Equatable {
    var name: String
    var url: String
    var local: Bool = false
    var ok: Bool = false
    var since: String = ""
    var discovered: Bool = false
    var tag: String = ""
    var health = Health()
    var agentCount: Int = 0
    var portCount: Int = 0
    var state = BoxState()
    var usage = UsageResponse()

    init(name: String, url: String, local: Bool = false, ok: Bool = false, since: String = "", discovered: Bool = false, tag: String = "", health: Health = Health(), agentCount: Int = 0, portCount: Int = 0, state: BoxState = BoxState(), usage: UsageResponse = UsageResponse()) {
        self.name = name
        self.url = url
        self.local = local
        self.ok = ok
        self.since = since
        self.discovered = discovered
        self.tag = tag
        self.health = health
        self.agentCount = agentCount
        self.portCount = portCount
        self.state = state
        self.usage = usage
    }

    init(card: BoxCardResponse) {
        var providers: [String: UsageProvider] = [:]
        for (provider, summary) in card.usage.today {
            providers[provider] = UsageProvider(quota: card.usage.quota[provider] ?? nil, today: summary)
        }
        for (provider, quota) in card.usage.quota where providers[provider] == nil {
            providers[provider] = UsageProvider(quota: quota)
        }
        self.init(name: card.name, url: card.url, local: card.local, ok: card.ok, since: card.since, discovered: card.discovered, tag: card.tag, health: card.health, agentCount: card.agents, portCount: card.ports, usage: UsageResponse(providers: providers))
    }
}

struct AlertAction: Codable, Equatable, Identifiable {
    var id: String
    var title: String
    var path: String?
    var method: String = "POST"

    enum CodingKeys: String, CodingKey {
        case id, title, path, method
    }

    private enum DecodeKeys: String, CodingKey {
        case id, title, label, path, url, method, action
    }

    init(id: String = "", title: String, path: String? = nil, method: String = "POST") {
        self.id = id
        self.title = title
        self.path = path
        self.method = method
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: DecodeKeys.self)
        id = try container.decodeIfPresent(String.self, forKey: .id)
            ?? (try container.decodeIfPresent(String.self, forKey: .action))
            ?? "action"
        title = try container.decodeIfPresent(String.self, forKey: .title)
            ?? (try container.decodeIfPresent(String.self, forKey: .label))
            ?? id
        path = try container.decodeIfPresent(String.self, forKey: .path)
            ?? (try container.decodeIfPresent(String.self, forKey: .url))
        method = try container.decodeIfPresent(String.self, forKey: .method) ?? "POST"
    }
}

struct Alert: Codable, Equatable, Identifiable {
    var id: String
    var title: String
    var message: String
    var severity: String
    var boxName: String
    var boxURL: String
    var actions: [AlertAction]

    enum CodingKeys: String, CodingKey {
        case id, title, message, severity, boxName, boxURL, actions
    }

    private enum DecodeKeys: String, CodingKey {
        case id, key, title, message, body, severity, boxName, boxURL, box, url, actions
    }

    init(id: String, title: String, message: String, severity: String = "warning", boxName: String = "", boxURL: String = "", actions: [AlertAction] = []) {
        self.id = id
        self.title = title
        self.message = message
        self.severity = severity
        self.boxName = boxName
        self.boxURL = boxURL
        self.actions = actions
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: DecodeKeys.self)
        id = try container.decodeIfPresent(String.self, forKey: .id)
            ?? (try container.decodeIfPresent(String.self, forKey: .key))
            ?? UUID().uuidString
        title = try container.decodeIfPresent(String.self, forKey: .title) ?? "Needs you"
        message = try container.decodeIfPresent(String.self, forKey: .message)
            ?? (try container.decodeIfPresent(String.self, forKey: .body))
            ?? ""
        severity = try container.decodeIfPresent(String.self, forKey: .severity) ?? "warning"
        boxName = try container.decodeIfPresent(String.self, forKey: .boxName)
            ?? (try container.decodeIfPresent(String.self, forKey: .box))
            ?? ""
        boxURL = try container.decodeIfPresent(String.self, forKey: .boxURL)
            ?? (try container.decodeIfPresent(String.self, forKey: .url))
            ?? ""
        actions = try container.decodeIfPresent([AlertAction].self, forKey: .actions) ?? []
    }
}

struct FleetSnapshot: Equatable {
    var boxes: [BoxSnapshot]
    var alerts: [Alert]
}

enum NotificationKind: String, Equatable {
    case alert
    case needsYou
    case unreachable
}

struct NotificationEvent: Equatable {
    var kind: NotificationKind
    var boxURL: String
    var title: String
    var body: String
    var alertID: String?
}

enum NotificationDecider {
    static func newEvents(previous: FleetSnapshot?, current: FleetSnapshot) -> [NotificationEvent] {
        guard let previous else { return [] }
        var events: [NotificationEvent] = []
        let oldAlerts = Set(previous.alerts.map(\.id))
        for alert in current.alerts where !oldAlerts.contains(alert.id) {
            events.append(NotificationEvent(kind: .alert, boxURL: alert.boxURL, title: alert.title, body: alert.message, alertID: alert.id))
        }
        for box in current.boxes {
            guard let old = previous.boxes.first(where: { $0.url == box.url }) else { continue }
            if old.ok && !box.ok {
                events.append(NotificationEvent(kind: .unreachable, boxURL: box.url, title: "Boxdeck box is unreachable", body: box.name, alertID: nil))
            }
            if !hasNeedsYou(old) && hasNeedsYou(box) {
                events.append(NotificationEvent(kind: .needsYou, boxURL: box.url, title: "An agent needs your attention", body: box.name, alertID: nil))
            }
        }
        return events
    }

    private static func hasNeedsYou(_ box: BoxSnapshot) -> Bool {
        box.state.agents.contains { status in
            let normalized = status.status.trimmingCharacters(in: .whitespacesAndNewlines).lowercased().replacingOccurrences(of: "-", with: "_")
            return ["needs_you", "waiting", "blocked"].contains(normalized)
        }
    }
}

enum MenuAction: Equatable {
    case open(String)
    case agents(String)
    case usage(String)
}

struct MenuLine: Equatable, Identifiable {
    let id = UUID()
    var title: String
    var action: MenuAction?
}

struct MenuBox: Equatable, Identifiable {
    let id = UUID()
    var title: String
    var url: String
    var lines: [MenuLine]
}

enum IconState: String, Equatable {
    case healthy
    case attention
    case rust
}

struct MenuModel: Equatable {
    var boxes: [MenuBox]
    var alerts: [Alert]
    var localUsageTitle: String?
    var addBox: Bool
    var iconState: IconState
    var tooltip: String
}

enum MenuModelBuilder {
    static func build(boxes: [BoxSnapshot], alerts: [Alert], localUsage: UsageResponse?) -> MenuModel {
        var iconState: IconState = .healthy
        let menuBoxes = boxes.map { box -> MenuBox in
            var title = box.name
            if box.discovered || box.tag == "tailnet" { title += " [tailnet]" }
            if !box.ok {
                iconState = .rust
                return MenuBox(title: title, url: box.url, lines: [MenuLine(title: unreachableTitle(box.since), action: nil)])
            }

            let health = box.health == Health() ? box.state.health : box.health
            let portCount = box.portCount == 0 ? box.state.ports.count : box.portCount
            var lines = [
                MenuLine(title: healthTitle(health), action: nil),
                MenuLine(title: agentTitle(box), action: .agents(hashURL(box.url, route: "/agents"))),
                MenuLine(title: "ports: \(portCount) open", action: nil)
            ]
            for provider in ["claude", "codex"] {
                let usage = box.usage.providers[provider] ?? UsageProvider(quota: nil)
                lines.append(MenuLine(title: quotaTitle(provider, usage), action: .usage(hashURL(box.url, route: "/usage"))))
                if quotaNeedsAttention(usage.quota) && iconState != .rust { iconState = .attention }
            }
            if box.state.agents.contains(where: { needsYou($0.status) }) && iconState != .rust { iconState = .attention }
            return MenuBox(title: title, url: box.url, lines: lines)
        }
        if !alerts.isEmpty && iconState != .rust { iconState = .attention }
        return MenuModel(
            boxes: menuBoxes,
            alerts: alerts,
            localUsageTitle: localUsage.map(localUsageTitle),
            addBox: boxes.isEmpty,
            iconState: iconState,
            tooltip: tooltip(boxes)
        )
    }

    static func renderText(_ model: MenuModel) -> String {
        var lines: [String] = []
        if !model.alerts.isEmpty {
            lines.append("Needs you")
            for alert in model.alerts {
                lines.append("  \(alert.title)")
            }
        }
        if model.addBox { lines.append("Add a box") }
        for box in model.boxes {
            lines.append(box.title)
            lines.append(contentsOf: box.lines.map { "  \($0.title)" })
        }
        if let localUsageTitle = model.localUsageTitle { lines.append(localUsageTitle) }
        lines.append(contentsOf: ["Refresh", "Settings", "Quit"])
        return lines.joined(separator: "\n")
    }

    private static func healthTitle(_ health: Health) -> String {
        "cpu \(String(format: "%.0f", health.cpu))% mem \(formatGB(health.memUsed))/\(formatGB(health.memTotal)) GB load \(String(format: "%.1f", health.load1))"
    }

    private static func formatGB(_ value: Double) -> String {
        guard value > 0 else { return "0" }
        let result = String(format: "%.1f", value / (1024 * 1024 * 1024))
        return result.hasSuffix(".0") ? String(result.dropLast(2)) : result
    }

    private static func agentTitle(_ box: BoxSnapshot) -> String {
        let waiting = box.state.agents.filter { needsYou($0.status) }.count
        let working = box.state.agents.isEmpty && box.agentCount > 0 ? box.agentCount : box.state.agents.count - waiting
        return "agents: \(working) working, \(waiting) needs you"
    }

    private static func needsYou(_ status: String) -> Bool {
        let normalized = status.trimmingCharacters(in: .whitespacesAndNewlines).lowercased().replacingOccurrences(of: "-", with: "_")
        return ["needs_you", "waiting", "blocked"].contains(normalized)
    }

    private static func quotaTitle(_ provider: String, _ usage: UsageProvider) -> String {
        guard let quota = usage.quota else { return "\(provider) api key" }
        var parts = [provider]
        if let fiveHour = quota.fiveHour { parts += ["5h", "\(Int(fiveHour.pct))%"] }
        if let sevenDay = quota.sevenDay { parts += ["7d", "\(Int(sevenDay.pct))%"] }
        return parts.count == 1 ? "\(provider) quota unavailable" : parts.joined(separator: " ")
    }

    private static func quotaNeedsAttention(_ quota: Quota?) -> Bool {
        guard let quota else { return false }
        return quota.fiveHour?.pct ?? 0 > 90 || quota.sevenDay?.pct ?? 0 > 90
    }

    private static func unreachableTitle(_ since: String) -> String {
        guard !since.isEmpty else { return "unreachable" }
        let formatter = ISO8601DateFormatter()
        guard let date = formatter.date(from: since) else { return "unreachable since \(since)" }
        let time = DateFormatter()
        time.locale = Locale(identifier: "en_US_POSIX")
        time.dateFormat = "HH:mm"
        return "unreachable since \(time.string(from: date))"
    }

    private static func hashURL(_ base: String, route: String) -> String {
        base.trimmingCharacters(in: CharacterSet(charactersIn: "/")) + "/#" + route
    }

    private static func tooltip(_ boxes: [BoxSnapshot]) -> String {
        guard !boxes.isEmpty else { return "boxdeck: no boxes" }
        return boxes.map { box in
            box.ok ? "\(box.name) \(box.agentCount) agents \(box.portCount) ports" : "\(box.name) down"
        }.joined(separator: " | ")
    }

    private static func localUsageTitle(_ usage: UsageResponse) -> String {
        let parts = ["claude", "codex"].compactMap { provider -> String? in
            guard let row = usage.providers[provider] else { return nil }
            return "\(provider) \(row.today.tokens.total) tokens $\(String(format: "%.4f", row.today.costUSD))"
        }
        return parts.isEmpty ? "Local usage unavailable" : "Local usage: " + parts.joined(separator: ", ")
    }
}
