import Foundation
import Security

public final class BoxStore: @unchecked Sendable {
    private let defaults: UserDefaults
    private let service = "dev.wicolian.boxdeck"
    private let key = "connections"

    public init(defaults: UserDefaults = .standard) { self.defaults = defaults }

    public var boxes: [BoxConnection] {
        guard let data = defaults.data(forKey: key), let values = try? JSONDecoder().decode([BoxConnection].self, from: data) else { return [] }
        return values
    }

    public func save(_ box: BoxConnection) throws {
        var values = boxes.filter { $0.url != box.url }
        values.append(box)
        defaults.set(try JSONEncoder().encode(values), forKey: key)
        try saveToken(box.token, for: box.url)
    }

    public func remove(_ box: BoxConnection) {
        let values = boxes.filter { $0.url != box.url }
        defaults.set(try? JSONEncoder().encode(values), forKey: key)
        deleteToken(for: box.url)
    }

    public func client(for box: BoxConnection) throws -> BoxdeckClient {
        try BoxdeckClient(url: box.url, token: token(for: box.url) ?? box.token)
    }

    public func saveToken(_ token: String, for url: String) throws {
        guard let data = token.data(using: .utf8) else { throw StoreError.invalidToken }
        let query: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service, kSecAttrAccount as String: url, kSecValueData as String: data]
        SecItemDelete(query as CFDictionary)
        guard SecItemAdd(query as CFDictionary, nil) == errSecSuccess else { throw StoreError.keychain }
    }

    public func token(for url: String) -> String? {
        let query: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service, kSecAttrAccount as String: url, kSecReturnData as String: true, kSecMatchLimit as String: kSecMatchLimitOne]
        var result: AnyObject?
        guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess, let data = result as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }

    private func deleteToken(for url: String) {
        let query: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service, kSecAttrAccount as String: url]
        SecItemDelete(query as CFDictionary)
    }
}

public enum StoreError: Error { case invalidToken, keychain }

public enum PairingURL {
    public static func parse(_ value: URL) throws -> Pairing {
        guard value.scheme?.lowercased() == "boxdeck", value.host?.lowercased() == "add", let components = URLComponents(url: value, resolvingAgainstBaseURL: false), let url = components.queryItems?.first(where: { $0.name == "url" })?.value, let token = components.queryItems?.first(where: { $0.name == "token" })?.value, !url.isEmpty, !token.isEmpty else { throw BoxdeckError.invalidPairingURL }
        return Pairing(label: "phone", url: url, token: token)
    }
}
