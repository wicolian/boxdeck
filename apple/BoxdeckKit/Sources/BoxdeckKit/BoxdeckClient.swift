import Foundation

public final class BoxdeckClient: @unchecked Sendable {
    public let baseURL: URL
    public let token: String
    private let session: URLSession

    public init(baseURL: URL, token: String, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.token = token
        self.session = session
    }

    public convenience init(url: String, token: String) throws {
        guard let baseURL = URL(string: url), ["http", "https"].contains(baseURL.scheme?.lowercased()), baseURL.host != nil else { throw BoxdeckError.invalidURL }
        self.init(baseURL: baseURL, token: token)
    }

    public func health() async throws -> Bool {
        struct Health: Decodable { var ok: Bool }
        return try await get("/api/health", as: Health.self).ok
    }

    public func state() async throws -> BoxState { try await get("/api/state", as: BoxState.self) }
    public func ports() async throws -> [Port] { try await get("/api/ports", as: [Port].self) }
    public func processes() async throws -> [ProcessRow] { try await get("/api/procs?sort=cpu&n=200", as: [ProcessRow].self) }
    public func agents() async throws -> [Agent] { try await get("/api/agents", as: [Agent].self) }
    public func usage(days: Int = 30) async throws -> UsageResponse { try await get("/api/usage?days=\(min(max(days, 1), 30))", as: UsageResponse.self) }
    public func usageAll(days: Int = 30) async throws -> UsageAllResponse { try await get("/api/usage/all?days=\(min(max(days, 1), 30))", as: UsageAllResponse.self) }
    public func boxes() async throws -> [BoxSnapshot] { try await get("/api/boxes", as: [BoxSnapshot].self) }
    public func peers() async throws -> [Peer] { try await get("/api/net/peers", as: [Peer].self) }
    public func apps() async throws -> [AppRecord] { try await get("/api/apps", as: [AppRecord].self) }
    public func pairing() async throws -> Pairing { try await get("/api/pair.json", as: Pairing.self) }

    public func alerts(state: String = "open", since: String? = nil) async throws -> [Alert] {
        var path = "/api/alerts?state=\(encode(state))"
        if let since { path += "&since=\(encode(since))" }
        if let envelope = try? await get(path, as: AlertListEnvelope.self) { return envelope.alerts }
        return try await get(path, as: [Alert].self)
    }

    public func alert(id: String) async throws -> Alert { try await get("/api/alerts/\(encode(id))", as: Alert.self) }
    public func acknowledge(alert: Alert) async throws { _ = try await post("/api/alerts/\(encode(alert.id))/ack", body: EmptyBody(), as: EmptyReply.self) }
    public func snooze(alert: Alert, until: Date) async throws { _ = try await post("/api/alerts/\(encode(alert.id))/snooze", body: UntilBody(until: ISO8601DateFormatter().string(from: until)), as: EmptyReply.self) }
    public func resolve(alert: Alert) async throws { _ = try await post("/api/alerts/\(encode(alert.id))/resolve", body: EmptyBody(), as: EmptyReply.self) }
    public func disarm(_ on: Bool) async throws { _ = try await post("/api/alerts/disarm", body: DisarmBody(on: on), as: EmptyReply.self) }
    public func alertAction(_ action: AlertAction) async throws { _ = try await post(action.path, rawBody: action.body, method: action.method) }
    public func appAction(id: String, action: String) async throws { _ = try await post("/api/apps/\(encode(id))/\(encode(action))", rawBody: [:], method: "POST") }
    public func paneTail(_ pane: String, lines: Int = 100) async throws -> String { try await get("/api/herd/read?pane=\(encode(pane))&lines=\(lines)", as: PaneTail.self).text }
    public func sendKeys(pane: String, keys: String) async throws { _ = try await post("/api/herd/keys", body: KeyBody(pane: pane, keys: keys), as: EmptyReply.self) }
    public func prompt(pane: String, text: String) async throws { _ = try await post("/api/herd/prompt", body: PromptBody(pane: pane, text: text), as: EmptyReply.self) }
    public func interrupt(pane: String) async throws { _ = try await post("/api/herd/interrupt", body: PaneBody(pane: pane), as: EmptyReply.self) }

    public func events(lastEventID: String? = nil) -> AsyncThrowingStream<BoxdeckEvent, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    var request = try makeRequest(path: "/api/events", method: "GET")
                    if let lastEventID { request.setValue(lastEventID, forHTTPHeaderField: "Last-Event-ID") }
                    let (bytes, response) = try await session.bytes(for: request)
                    guard let http = response as? HTTPURLResponse, 200..<300 ~= http.statusCode else { throw BoxdeckError.http((response as? HTTPURLResponse)?.statusCode ?? 0) }
                    var eventID = ""
                    var eventName = "message"
                    var dataLines: [String] = []
                    for try await line in bytes.lines {
                        if line.isEmpty {
                            if !dataLines.isEmpty {
                                continuation.yield(BoxdeckEvent(id: eventID, name: eventName, data: Data(dataLines.joined(separator: "\n").utf8)))
                            }
                            eventID = ""
                            eventName = "message"
                            dataLines.removeAll(keepingCapacity: true)
                        } else if line.hasPrefix("id:") {
                            eventID = String(line.dropFirst(3)).trimmingCharacters(in: .whitespaces)
                        } else if line.hasPrefix("event:") {
                            eventName = String(line.dropFirst(6)).trimmingCharacters(in: .whitespaces)
                        } else if line.hasPrefix("data:") {
                            dataLines.append(String(line.dropFirst(5)).trimmingCharacters(in: .whitespaces))
                        }
                    }
                    continuation.finish()
                } catch is CancellationError {
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    private func get<T: Decodable>(_ path: String, as type: T.Type) async throws -> T {
        let request = try makeRequest(path: path, method: "GET")
        let (data, response) = try await session.data(for: request)
        try validate(response)
        return try JSONDecoder.boxdeck.decode(type, from: data)
    }

    private func post<T: Encodable, R: Decodable>(_ path: String, body: T, as type: R.Type) async throws -> R {
        let data = try JSONEncoder().encode(body)
        return try await post(path, data: data, as: type)
    }

    private func post(_ path: String, rawBody: [String: String], method: String) async throws {
        let data = try JSONSerialization.data(withJSONObject: rawBody)
        _ = try await post(path, data: data, as: EmptyReply.self, method: method)
    }

    private func post<R: Decodable>(_ path: String, data: Data, as type: R.Type, method: String = "POST") async throws -> R {
        let request = try makeRequest(path: path, method: method, body: data)
        let (responseData, response) = try await session.data(for: request)
        try validate(response)
        if responseData.isEmpty { return try JSONDecoder.boxdeck.decode(type, from: Data("{}".utf8)) }
        return try JSONDecoder.boxdeck.decode(type, from: responseData)
    }

    private func makeRequest(path: String, method: String, body: Data? = nil) throws -> URLRequest {
        guard let url = URL(string: path, relativeTo: baseURL)?.absoluteURL else { throw BoxdeckError.invalidURL }
        var request = URLRequest(url: url, timeoutInterval: 5)
        request.httpMethod = method
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        if body != nil { request.setValue("application/json", forHTTPHeaderField: "Content-Type") }
        request.httpBody = body
        return request
    }

    private func validate(_ response: URLResponse) throws {
        guard let http = response as? HTTPURLResponse, 200..<300 ~= http.statusCode else { throw BoxdeckError.http((response as? HTTPURLResponse)?.statusCode ?? 0) }
    }

    private func encode(_ value: String) -> String { value.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? value }
}

private struct AlertListEnvelope: Decodable { var alerts: [Alert] }
private struct PaneTail: Decodable { var text: String }
private struct EmptyBody: Encodable {}
private struct EmptyReply: Decodable {}
private struct UntilBody: Encodable { var until: String }
private struct DisarmBody: Encodable { var on: Bool }
private struct PaneBody: Encodable { var pane: String }
private struct KeyBody: Encodable { var pane: String; var keys: String }
private struct PromptBody: Encodable { var pane: String; var text: String }

private extension JSONDecoder {
    static var boxdeck: JSONDecoder { JSONDecoder() }
}
