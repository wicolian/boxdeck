import Foundation

public enum BoxdeckPushRegistration {
    public static let bundleID = "dev.wicolian.boxdeck"

    public static func register(tokenData: Data, platform: String, name: String) {
        UserDefaults.standard.set(tokenData, forKey: "boxdeck.push-token.\(platform)")
        let token = tokenData.map { String(format: "%02x", $0) }.joined()
        guard !token.isEmpty else { return }
        Task {
            let store = BoxStore()
            for box in store.boxes {
                guard let client = try? store.client(for: box) else { continue }
                try? await client.registerPushToken(platform: platform, token: token, name: name, bundleID: bundleID)
            }
        }
    }

    public static func unregister(tokenData: Data, platform: String, box: BoxConnection) {
        let token = tokenData.map { String(format: "%02x", $0) }.joined()
        guard !token.isEmpty, let client = try? BoxStore().client(for: box) else { return }
        Task { try? await client.unregisterPushToken(platform: platform, token: token) }
    }

    public static func unregister(platform: String, box: BoxConnection) {
        guard let data = UserDefaults.standard.data(forKey: "boxdeck.push-token.\(platform)") else { return }
        unregister(tokenData: data, platform: platform, box: box)
    }
}
