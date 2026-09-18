import Foundation

public struct HealthSnapshot: Codable, Sendable, Hashable {
    public var cpu: Double
    public var memUsed: UInt64
    public var memTotal: UInt64
    public var load1: Double
    public var swapUsed: UInt64
    public var swapTotal: UInt64

    public init(cpu: Double = 0, memUsed: UInt64 = 0, memTotal: UInt64 = 0, load1: Double = 0, swapUsed: UInt64 = 0, swapTotal: UInt64 = 0) {
        self.cpu = cpu
        self.memUsed = memUsed
        self.memTotal = memTotal
        self.load1 = load1
        self.swapUsed = swapUsed
        self.swapTotal = swapTotal
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        cpu = try c.decodeIfPresent(Double.self, forKey: .cpu) ?? 0
        memUsed = try c.decodeIfPresent(UInt64.self, forKey: .memUsed) ?? 0
        memTotal = try c.decodeIfPresent(UInt64.self, forKey: .memTotal) ?? 0
        load1 = try c.decodeIfPresent(Double.self, forKey: .load1) ?? 0
        swapUsed = try c.decodeIfPresent(UInt64.self, forKey: .swapUsed) ?? 0
        swapTotal = try c.decodeIfPresent(UInt64.self, forKey: .swapTotal) ?? 0
    }
}

public struct Agent: Codable, Sendable, Hashable, Identifiable {
    public var pid: Int
    public var kind: String
    public var model: String
    public var cwd: String
    public var pane: String
    public var paneID: String
    public var title: String
    public var status: String
    public var tokens: [String: String]

    public var id: String { paneID.isEmpty ? "pid-\(pid)" : paneID }

    enum CodingKeys: String, CodingKey {
        case pid, kind, model, cwd, pane
        case paneID = "pane_id"
        case title = "terminal_title_stripped"
        case status = "agent_status"
        case tokens
    }

    public init(pid: Int = 0, kind: String = "agent", model: String = "", cwd: String = "", pane: String = "", paneID: String = "", title: String = "", status: String = "", tokens: [String: String] = [:]) {
        self.pid = pid
        self.kind = kind
        self.model = model
        self.cwd = cwd
        self.pane = pane
        self.paneID = paneID
        self.title = title
        self.status = status
        self.tokens = tokens
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        pid = try c.decodeIfPresent(Int.self, forKey: .pid) ?? 0
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? "agent"
        model = try c.decodeIfPresent(String.self, forKey: .model) ?? ""
        cwd = try c.decodeIfPresent(String.self, forKey: .cwd) ?? ""
        pane = try c.decodeIfPresent(String.self, forKey: .pane) ?? ""
        paneID = try c.decodeIfPresent(String.self, forKey: .paneID) ?? ""
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        status = try c.decodeIfPresent(String.self, forKey: .status) ?? ""
        tokens = try c.decodeIfPresent([String: String].self, forKey: .tokens) ?? [:]
    }
}

public struct Port: Codable, Sendable, Hashable, Identifiable {
    public var port: Int
    public var label: String
    public var title: String
    public var url: String
    public var process: String

    public var id: Int { port }

    enum CodingKeys: String, CodingKey {
        case port, label, title, url
        case process = "proc"
    }

    public init(port: Int = 0, label: String = "", title: String = "", url: String = "", process: String = "") {
        self.port = port
        self.label = label
        self.title = title
        self.url = url
        self.process = process
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        port = try c.decodeIfPresent(Int.self, forKey: .port) ?? 0
        label = try c.decodeIfPresent(String.self, forKey: .label) ?? ""
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        url = try c.decodeIfPresent(String.self, forKey: .url) ?? ""
        process = try c.decodeIfPresent(String.self, forKey: .process) ?? ""
    }
}

public struct ProcessRow: Codable, Sendable, Hashable, Identifiable {
    public var pid: Int
    public var name: String
    public var cpu: Double
    public var rss: Int64
    public var args: String
    public var id: Int { pid }
    public init(pid: Int = 0, name: String = "", cpu: Double = 0, rss: Int64 = 0, args: String = "") { self.pid = pid; self.name = name; self.cpu = cpu; self.rss = rss; self.args = args }
}

public struct BoxState: Codable, Sendable {
    public var host: String
    public var health: HealthSnapshot
    public var agents: [Agent]
    public var ports: [Port]

    public init(host: String = "", health: HealthSnapshot = .init(), agents: [Agent] = [], ports: [Port] = []) {
        self.host = host
        self.health = health
        self.agents = agents
        self.ports = ports
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        host = try c.decodeIfPresent(String.self, forKey: .host) ?? ""
        health = try c.decodeIfPresent(HealthSnapshot.self, forKey: .health) ?? .init()
        agents = try c.decodeIfPresent([Agent].self, forKey: .agents) ?? []
        ports = try c.decodeIfPresent([Port].self, forKey: .ports) ?? []
    }
}

public struct TokenTotals: Codable, Sendable, Hashable {
    public var input: Int64
    public var cachedInput: Int64
    public var cacheWrite: Int64
    public var output: Int64
    public var total: Int64 { input + cachedInput + cacheWrite + output }

    enum CodingKeys: String, CodingKey {
        case input = "in"
        case cachedInput = "cachedIn"
        case cacheWrite, output = "out"
    }

    public init(input: Int64 = 0, cachedInput: Int64 = 0, cacheWrite: Int64 = 0, output: Int64 = 0) {
        self.input = input
        self.cachedInput = cachedInput
        self.cacheWrite = cacheWrite
        self.output = output
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        input = try c.decodeIfPresent(Int64.self, forKey: .input) ?? 0
        cachedInput = try c.decodeIfPresent(Int64.self, forKey: .cachedInput) ?? 0
        cacheWrite = try c.decodeIfPresent(Int64.self, forKey: .cacheWrite) ?? 0
        output = try c.decodeIfPresent(Int64.self, forKey: .output) ?? 0
    }
}

public struct UsageSummary: Codable, Sendable, Hashable {
    public var tokens: TokenTotals
    public var costUSD: Double

    enum CodingKeys: String, CodingKey { case tokens, costUSD = "costUsd" }

    public init(tokens: TokenTotals = .init(), costUSD: Double = 0) {
        self.tokens = tokens
        self.costUSD = costUSD
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tokens = try c.decodeIfPresent(TokenTotals.self, forKey: .tokens) ?? .init()
        costUSD = try c.decodeIfPresent(Double.self, forKey: .costUSD) ?? 0
    }
}

public struct QuotaWindow: Codable, Sendable, Hashable {
    public var pct: Double
    public var resetsAt: String
    public init(pct: Double = 0, resetsAt: String = "") { self.pct = pct; self.resetsAt = resetsAt }
}

public struct Quota: Codable, Sendable, Hashable {
    public var fiveHour: QuotaWindow?
    public var sevenDay: QuotaWindow?
    public init(fiveHour: QuotaWindow? = nil, sevenDay: QuotaWindow? = nil) { self.fiveHour = fiveHour; self.sevenDay = sevenDay }
}

public struct UsageDay: Codable, Sendable, Hashable {
    public var day: String
    public var tokens: TokenTotals
    public var costUSD: Double
    public var models: [String: UsageSummary]

    enum CodingKeys: String, CodingKey { case day, tokens, costUSD = "costUsd", models }

    public init(day: String = "", tokens: TokenTotals = .init(), costUSD: Double = 0, models: [String: UsageSummary] = [:]) {
        self.day = day; self.tokens = tokens; self.costUSD = costUSD; self.models = models
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        day = try c.decodeIfPresent(String.self, forKey: .day) ?? ""
        tokens = try c.decodeIfPresent(TokenTotals.self, forKey: .tokens) ?? .init()
        costUSD = try c.decodeIfPresent(Double.self, forKey: .costUSD) ?? 0
        models = try c.decodeIfPresent([String: UsageSummary].self, forKey: .models) ?? [:]
    }
}

public struct UsageProvider: Codable, Sendable {
    public var quota: Quota?
    public var today: UsageSummary
    public var localDay: String
    public var days: [UsageDay]
    public var sessions: Int

    public init(quota: Quota? = nil, today: UsageSummary = .init(), localDay: String = "", days: [UsageDay] = [], sessions: Int = 0) {
        self.quota = quota; self.today = today; self.localDay = localDay; self.days = days; self.sessions = sessions
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        quota = try c.decodeIfPresent(Quota.self, forKey: .quota)
        today = try c.decodeIfPresent(UsageSummary.self, forKey: .today) ?? .init()
        localDay = try c.decodeIfPresent(String.self, forKey: .localDay) ?? ""
        days = try c.decodeIfPresent([UsageDay].self, forKey: .days) ?? []
        sessions = try c.decodeIfPresent(Int.self, forKey: .sessions) ?? 0
    }
}

public struct UsageResponse: Codable, Sendable {
    public var providers: [String: UsageProvider]
    public var device: String
    public var updatedAt: String
    public init(providers: [String: UsageProvider] = [:], device: String = "", updatedAt: String = "") { self.providers = providers; self.device = device; self.updatedAt = updatedAt }
}

public struct BoxSnapshot: Codable, Sendable, Identifiable {
    public var name: String
    public var url: String
    public var local: Bool
    public var ok: Bool
    public var since: String
    public var discovered: Bool
    public var tag: String
    public var health: HealthSnapshot
    public var agents: Int
    public var ports: Int
    public var usage: BoxUsage
    public var id: String { url.isEmpty ? name : url }

    public init(name: String = "", url: String = "", local: Bool = false, ok: Bool = false, since: String = "", discovered: Bool = false, tag: String = "", health: HealthSnapshot = .init(), agents: Int = 0, ports: Int = 0, usage: BoxUsage = .init()) {
        self.name = name; self.url = url; self.local = local; self.ok = ok; self.since = since; self.discovered = discovered; self.tag = tag; self.health = health; self.agents = agents; self.ports = ports; self.usage = usage
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        url = try c.decodeIfPresent(String.self, forKey: .url) ?? ""
        local = try c.decodeIfPresent(Bool.self, forKey: .local) ?? false
        ok = try c.decodeIfPresent(Bool.self, forKey: .ok) ?? false
        since = try c.decodeIfPresent(String.self, forKey: .since) ?? ""
        discovered = try c.decodeIfPresent(Bool.self, forKey: .discovered) ?? false
        tag = try c.decodeIfPresent(String.self, forKey: .tag) ?? ""
        health = try c.decodeIfPresent(HealthSnapshot.self, forKey: .health) ?? .init()
        agents = try c.decodeIfPresent(Int.self, forKey: .agents) ?? 0
        ports = try c.decodeIfPresent(Int.self, forKey: .ports) ?? 0
        usage = try c.decodeIfPresent(BoxUsage.self, forKey: .usage) ?? .init()
    }
}

public struct BoxUsage: Codable, Sendable {
    public var today: [String: UsageSummary]
    public var quota: [String: Quota?]
    public init(today: [String: UsageSummary] = [:], quota: [String: Quota?] = [:]) { self.today = today; self.quota = quota }
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        today = try c.decodeIfPresent([String: UsageSummary].self, forKey: .today) ?? [:]
        quota = try c.decodeIfPresent([String: Quota?].self, forKey: .quota) ?? [:]
    }
}

public struct UsageAllResponse: Codable, Sendable {
    public var providers: [String: UsageProvider]
    public var boxes: [BoxSnapshot]
    public var updatedAt: String
    public init(providers: [String: UsageProvider] = [:], boxes: [BoxSnapshot] = [], updatedAt: String = "") { self.providers = providers; self.boxes = boxes; self.updatedAt = updatedAt }
}

public struct AppRecord: Codable, Sendable, Identifiable {
    public var id: String
    public var name: String
    public var tag: String
    public var status: String
    public var running: Bool
    public var detected: Bool
    public var url: String
    public var canStart: Bool
    public var canStop: Bool
    public init(id: String = "", name: String = "", tag: String = "", status: String = "", running: Bool = false, detected: Bool = false, url: String = "", canStart: Bool = false, canStop: Bool = false) { self.id = id; self.name = name; self.tag = tag; self.status = status; self.running = running; self.detected = detected; self.url = url; self.canStart = canStart; self.canStop = canStop }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decodeIfPresent(String.self, forKey: .id) ?? ""
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? id
        tag = try c.decodeIfPresent(String.self, forKey: .tag) ?? ""
        status = try c.decodeIfPresent(String.self, forKey: .status) ?? ""
        running = try c.decodeIfPresent(Bool.self, forKey: .running) ?? false
        detected = try c.decodeIfPresent(Bool.self, forKey: .detected) ?? false
        url = try c.decodeIfPresent(String.self, forKey: .url) ?? ""
        canStart = try c.decodeIfPresent(Bool.self, forKey: .canStart) ?? false
        canStop = try c.decodeIfPresent(Bool.self, forKey: .canStop) ?? false
    }
}

public struct AlertAction: Codable, Sendable, Hashable, Identifiable {
    public var label: String
    public var method: String
    public var path: String
    public var body: [String: String]
    public var id: String { label + path }
    public init(label: String = "", method: String = "POST", path: String = "", body: [String: String] = [:]) { self.label = label; self.method = method; self.path = path; self.body = body }
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        label = try container.decodeIfPresent(String.self, forKey: .label) ?? ""
        method = try container.decodeIfPresent(String.self, forKey: .method) ?? "POST"
        path = try container.decodeIfPresent(String.self, forKey: .path) ?? ""
        body = try container.decodeIfPresent([String: String].self, forKey: .body) ?? [:]
    }
}

public struct Alert: Codable, Sendable, Identifiable {
    public var id: String
    public var box: String
    public var rule: String
    public var severity: String
    public var title: String
    public var body: String
    public var at: String
    public var state: String
    public var link: String
    public var actions: [AlertAction]
    public var count: Int
    public init(id: String = "", box: String = "", rule: String = "", severity: String = "info", title: String = "", body: String = "", at: String = "", state: String = "open", link: String = "", actions: [AlertAction] = [], count: Int = 1) { self.id = id; self.box = box; self.rule = rule; self.severity = severity; self.title = title; self.body = body; self.at = at; self.state = state; self.link = link; self.actions = actions; self.count = count }
}

public struct Peer: Codable, Sendable, Identifiable {
    public var name: String
    public var url: String
    public var boxdeck: Bool
    public var os: String
    public var online: Bool
    public var lastSeen: String
    public var version: String
    public var agents: Int
    public var ports: Int
    public var health: HealthSnapshot
    public var id: String { url.isEmpty ? name : url }
    enum CodingKeys: String, CodingKey { case name, url, boxdeck, os, online, lastSeen, version, agents, ports, health }
    public init(name: String = "", url: String = "", boxdeck: Bool = false, os: String = "", online: Bool = false, lastSeen: String = "", version: String = "", agents: Int = 0, ports: Int = 0, health: HealthSnapshot = .init()) { self.name = name; self.url = url; self.boxdeck = boxdeck; self.os = os; self.online = online; self.lastSeen = lastSeen; self.version = version; self.agents = agents; self.ports = ports; self.health = health }
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        url = try c.decodeIfPresent(String.self, forKey: .url) ?? ""
        boxdeck = try c.decodeIfPresent(Bool.self, forKey: .boxdeck) ?? false
        os = try c.decodeIfPresent(String.self, forKey: .os) ?? ""
        online = try c.decodeIfPresent(Bool.self, forKey: .online) ?? false
        lastSeen = try c.decodeIfPresent(String.self, forKey: .lastSeen) ?? ""
        version = try c.decodeIfPresent(String.self, forKey: .version) ?? ""
        agents = try c.decodeIfPresent(Int.self, forKey: .agents) ?? 0
        ports = try c.decodeIfPresent(Int.self, forKey: .ports) ?? 0
        health = try c.decodeIfPresent(HealthSnapshot.self, forKey: .health) ?? .init()
    }
}

public struct BoxConnection: Codable, Sendable, Identifiable, Hashable {
    public var name: String
    public var url: String
    public var token: String
    public var id: String { url }
    public init(name: String, url: String, token: String) { self.name = name; self.url = url; self.token = token }
}

public struct Pairing: Codable, Sendable {
    public var label: String
    public var url: String
    public var token: String
    public init(label: String, url: String, token: String) { self.label = label; self.url = url; self.token = token }
}

public struct BoxdeckEvent: Sendable {
    public var id: String
    public var name: String
    public var data: Data
    public init(id: String, name: String, data: Data) { self.id = id; self.name = name; self.data = data }
}

public enum BoxdeckError: Error, LocalizedError, Sendable {
    case invalidURL
    case http(Int)
    case invalidPairingURL
    case emptyResponse
    public var errorDescription: String? {
        switch self { case .invalidURL: return "The box URL is invalid."; case .http(let status): return "The box returned HTTP \(status)."; case .invalidPairingURL: return "This is not a Boxdeck pairing link."; case .emptyResponse: return "The box returned an empty response." }
    }
}
