import Foundation

struct HTTPError: LocalizedError, Equatable {
    let status: Int

    var errorDescription: String? { "boxdeck returned HTTP \(status)" }
}

struct BoxdeckClient {
    let baseURL: URL
    let token: String
    let session: URLSession

    init?(baseURL: String, token: String, session: URLSession = .shared) {
        guard let url = URL(string: baseURL.trimmingCharacters(in: CharacterSet(charactersIn: "/"))), let scheme = url.scheme?.lowercased(), ["http", "https"].contains(scheme) else {
            return nil
        }
        self.baseURL = url
        self.token = token
        self.session = session
    }

    func fetchBox(name: String) async throws -> BoxSnapshot {
        let state: BoxState = try await get("/api/state")
        var box = BoxSnapshot(name: name, url: baseURL.absoluteString, local: false, ok: true, health: state.health, agentCount: state.agents.count, portCount: state.ports.count, state: state)
        if let usage: UsageResponse = try? await get("/api/usage?days=30") {
            box.usage = usage
        }
        return box
    }

    func fetchBoxCards() async throws -> [BoxSnapshot] {
        let cards: [BoxCardResponse] = try await get("/api/boxes")
        return cards.map(BoxSnapshot.init(card:))
    }

    func fetchUsageAll() async throws -> UsageAllResponse {
        try await get("/api/usage/all?days=30")
    }

    func fetchPeers() async throws -> [Peer] {
        do {
            return try await get("/api/net/peers")
        } catch let error as HTTPError where error.status == 404 {
            return []
        }
    }

    func fetchAlerts() async throws -> [Alert] {
        do {
            let data = try await getData("/api/alerts?state=open")
            if let alerts = try? JSONDecoder().decode([Alert].self, from: data) { return alerts }
            struct Envelope: Decodable { var alerts: [Alert] = [] }
            return try JSONDecoder().decode(Envelope.self, from: data).alerts
        } catch let error as HTTPError where error.status == 404 {
            return []
        }
    }

    func post(path: String, method: String = "POST", body: Data = Data("{}".utf8)) async throws {
        var request = try makeRequest(path: path, method: method)
        request.httpBody = body
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let (_, response) = try await session.data(for: request)
        try validate(response)
    }

    func disarm(_ enabled: Bool) async throws -> Bool {
        do {
            let body = try JSONEncoder().encode(["disarmed": enabled])
            try await post(path: "/api/alerts/disarm", method: "POST", body: body)
            return true
        } catch let error as HTTPError where error.status == 404 {
            return false
        }
    }

    private func get<T: Decodable>(_ path: String) async throws -> T {
        let data = try await getData(path)
        return try JSONDecoder().decode(T.self, from: data)
    }

    private func getData(_ path: String) async throws -> Data {
        let request = try makeRequest(path: path, method: "GET")
        let (data, response) = try await session.data(for: request)
        try validate(response)
        return data
    }

    private func makeRequest(path: String, method: String) throws -> URLRequest {
        let target: URL
        if let absolute = URL(string: path), absolute.scheme != nil {
            target = absolute
        } else {
            guard let joined = URL(string: path, relativeTo: baseURL)?.absoluteURL else { throw URLError(.badURL) }
            target = joined
        }
        var request = URLRequest(url: target)
        request.httpMethod = method
        request.timeoutInterval = 5
        if !token.isEmpty { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        return request
    }

    private func validate(_ response: URLResponse) throws {
        guard let http = response as? HTTPURLResponse else { throw URLError(.badServerResponse) }
        guard (200..<300).contains(http.statusCode) else { throw HTTPError(status: http.statusCode) }
    }
}

enum LocalUsageReader {
    static func readData() throws -> Data {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        process.arguments = ["boxdeck", "usage", "--json"]
        let pipe = Pipe()
        process.standardOutput = pipe
        try process.run()
        process.waitUntilExit()
        guard process.terminationStatus == 0 else { throw NSError(domain: "BoxdeckBar", code: Int(process.terminationStatus)) }
        return pipe.fileHandleForReading.readDataToEndOfFile()
    }

    static func read() throws -> UsageResponse {
        try JSONDecoder().decode(UsageResponse.self, from: readData())
    }
}

enum FleetLoader {
    static func load(config: BarConfig) async -> FleetSnapshot {
        let configured = await withTaskGroup(of: BoxSnapshot.self, returning: [BoxSnapshot].self) { group in
            for box in config.boxes {
                group.addTask {
                    guard let client = BoxdeckClient(baseURL: box.url, token: box.token) else {
                        return BoxSnapshot(name: box.name, url: box.url, since: ISO8601DateFormatter().string(from: Date()))
                    }
                    do {
                        return try await client.fetchBox(name: box.name)
                    } catch {
                        return BoxSnapshot(name: box.name, url: box.url, since: ISO8601DateFormatter().string(from: Date()))
                    }
                }
            }
            var result: [BoxSnapshot] = []
            for await box in group { result.append(box) }
            return result
        }

        guard let first = config.boxes.first, !config.fleetToken.isEmpty, let seed = BoxdeckClient(baseURL: first.url, token: config.fleetToken) else {
            return FleetSnapshot(boxes: configured, alerts: await loadAlerts(configured: configured, config: config))
        }

        var result = configured
        let discovered = (try? await seed.fetchBoxCards()) ?? []
        let usageAll = try? await seed.fetchUsageAll()
        for var card in discovered where card.discovered && !configured.contains(where: { sameBox($0.url, card.url) || ($0.name == card.name && !card.name.isEmpty) }) {
            if let usageAll, let match = usageAll.boxes.first(where: { sameBox($0.url, card.url) || ($0.name == card.name && !card.name.isEmpty) }) {
                card.usage = BoxSnapshot(card: match).usage
            }
            card.tag = "tailnet"
            result.append(card)
        }
        if let peers = try? await seed.fetchPeers() {
            let known = Set(result.map { $0.url.trimmingCharacters(in: CharacterSet(charactersIn: "/")) })
            let extra = await withTaskGroup(of: BoxSnapshot?.self, returning: [BoxSnapshot].self) { group in
                for peer in peers where peer.boxdeck && !peer.url.isEmpty && !known.contains(peer.url.trimmingCharacters(in: CharacterSet(charactersIn: "/"))) {
                    group.addTask {
                        guard let client = BoxdeckClient(baseURL: peer.url, token: config.fleetToken) else { return nil }
                        var box = (try? await client.fetchBox(name: peer.name)) ?? BoxSnapshot(name: peer.name, url: peer.url, since: ISO8601DateFormatter().string(from: Date()))
                        box.discovered = true
                        box.tag = "tailnet"
                        return box
                    }
                }
                var result: [BoxSnapshot] = []
                for await box in group { if let box { result.append(box) } }
                return result
            }
            result.append(contentsOf: extra.filter { candidate in
                !result.contains(where: { existing in sameBox(existing.url, candidate.url) })
            })
        }
        return FleetSnapshot(boxes: result, alerts: await loadAlerts(configured: result, config: config))
    }

    private static func loadAlerts(configured: [BoxSnapshot], config: BarConfig) async -> [Alert] {
        guard !configured.isEmpty else { return [] }
        return await withTaskGroup(of: [Alert].self, returning: [Alert].self) { group in
            for box in configured {
                let token = config.boxes.first(where: { sameBox($0.url, box.url) })?.token ?? config.fleetToken
                group.addTask {
                    guard let client = BoxdeckClient(baseURL: box.url, token: token), let alerts = try? await client.fetchAlerts() else { return [] }
                    return alerts.map { alert in
                        var value = alert
                        if value.boxName.isEmpty { value.boxName = box.name }
                        if value.boxURL.isEmpty { value.boxURL = box.url }
                        return value
                    }
                }
            }
            var result: [Alert] = []
            for await alerts in group { result.append(contentsOf: alerts) }
            return result
        }
    }

    private static func sameBox(_ lhs: String, _ rhs: String) -> Bool {
        lhs.trimmingCharacters(in: CharacterSet(charactersIn: "/")) == rhs.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
    }
}
