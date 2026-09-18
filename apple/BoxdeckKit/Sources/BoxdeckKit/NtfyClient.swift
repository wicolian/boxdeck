import Foundation

public struct NtfyMessage: Codable, Sendable {
    public var id: String
    public var title: String?
    public var message: String?
    public var topic: String
    public var tags: [String]?
    public init(id: String = "", title: String? = nil, message: String? = nil, topic: String = "", tags: [String]? = nil) { self.id = id; self.title = title; self.message = message; self.topic = topic; self.tags = tags }
}

public final class NtfyClient: @unchecked Sendable {
    private let session: URLSession
    public init(session: URLSession = .shared) { self.session = session }
    public func stream(server: URL, topic: String, token: String = "") -> AsyncThrowingStream<NtfyMessage, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    guard var components = URLComponents(url: server, resolvingAgainstBaseURL: false) else { throw BoxdeckError.invalidURL }
                    components.path = components.path.trimmingCharacters(in: CharacterSet(charactersIn: "/")) + "/" + topic + "/json"
                    guard let url = components.url else { throw BoxdeckError.invalidURL }
                    var request = URLRequest(url: url, timeoutInterval: 0)
                    if !token.isEmpty { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
                    let (bytes, response) = try await session.bytes(for: request)
                    guard let http = response as? HTTPURLResponse, 200..<300 ~= http.statusCode else { throw BoxdeckError.http((response as? HTTPURLResponse)?.statusCode ?? 0) }
                    for try await line in bytes.lines where !line.isEmpty {
                        if let data = line.data(using: .utf8), let message = try? JSONDecoder().decode(NtfyMessage.self, from: data) { continuation.yield(message) }
                    }
                    continuation.finish()
                } catch is CancellationError { continuation.finish() } catch { continuation.finish(throwing: error) }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }
}
