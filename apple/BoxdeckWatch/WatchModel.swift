import Foundation
import SwiftUI
import WatchConnectivity
import UserNotifications

@MainActor
final class WatchModel: NSObject, ObservableObject, WCSessionDelegate, @preconcurrency UNUserNotificationCenterDelegate {
    let store = BoxStore()
    @Published var alerts: [Alert] = []
    @Published var boxes: [BoxSnapshot] = []
    @Published var loading = false

    override init() {
        super.init()
        if WCSession.isSupported() { WCSession.default.delegate = self; WCSession.default.activate() }
        registerNotificationActions()
    }

    func refresh() async {
        guard !loading else { return }
        loading = true
        defer { loading = false }
        var freshBoxes: [BoxSnapshot] = []
        var freshAlerts: [Alert] = []
        for box in store.boxes {
            guard let client = try? store.client(for: box) else { continue }
            freshBoxes.append(contentsOf: (try? await client.boxes()) ?? [])
            freshAlerts.append(contentsOf: (try? await client.alerts()) ?? [])
        }
        boxes = unique(freshBoxes)
        alerts = freshAlerts.filter { $0.state == "open" }
        transferToPhone()
    }

    func action(_ action: AlertAction, alert: Alert) async {
        guard let client = store.boxes.compactMap({ try? store.client(for: $0) }).first else { return }
        try? await client.alertAction(action)
        await refresh()
    }

    func acknowledge(_ alert: Alert) async {
        guard let client = store.boxes.compactMap({ try? store.client(for: $0) }).first else { return }
        try? await client.acknowledge(alert: alert)
        await refresh()
    }

    func snooze(_ alert: Alert) async {
        guard let client = store.boxes.compactMap({ try? store.client(for: $0) }).first else { return }
        try? await client.snooze(alert: alert, until: Date().addingTimeInterval(2 * 60 * 60))
        await refresh()
    }

    func handle(_ userInfo: [AnyHashable: Any], actionIdentifier: String = UNNotificationDefaultActionIdentifier) {
        let value = userInfo["boxdeck_alert"] ?? userInfo["alert"]
        let alertData = (value as? [String: Any]).flatMap { JSONSerialization.isValidJSONObject($0) ? try? JSONSerialization.data(withJSONObject: $0) : nil }
        handleRemote(alertData: alertData, deckURL: userInfo["deck_url"] as? String, token: userInfo["action_token"] as? String, actionIdentifier: actionIdentifier)
    }

    private func handleRemote(alertData: Data?, deckURL: String?, token: String?, actionIdentifier: String) {
        guard actionIdentifier != UNNotificationDefaultActionIdentifier, let token, let deckURL, let alertData, let alert = try? JSONDecoder().decode(Alert.self, from: alertData), let action = remoteAction(actionIdentifier, alert: alert) else {
            Task { await refresh() }
            return
        }
        Task {
            if let client = try? BoxdeckClient(url: deckURL, token: token) { try? await client.alertAction(action) }
            await refresh()
        }
    }

    private func unique(_ values: [BoxSnapshot]) -> [BoxSnapshot] { var seen = Set<String>(); return values.filter { seen.insert($0.url).inserted } }

    private func transferToPhone() {
        guard WCSession.default.activationState == .activated else { return }
        let payload = boxes.map { ["name": $0.name, "url": $0.url, "ok": $0.ok] }
        try? WCSession.default.updateApplicationContext(["boxes": payload])
        WCSession.default.transferUserInfo(["boxes": payload])
    }

    private func registerNotificationActions() {
        let actions = [
            UNNotificationAction(identifier: "BOXDECK_APPROVE", title: "Approve", options: []),
            UNNotificationAction(identifier: "BOXDECK_YES", title: "Yes", options: []),
            UNNotificationAction(identifier: "BOXDECK_NO", title: "No", options: []),
            UNNotificationAction(identifier: "BOXDECK_INTERRUPT", title: "Interrupt", options: []),
            UNNotificationAction(identifier: "BOXDECK_SNOOZE", title: "Snooze 2h", options: []),
            UNNotificationAction(identifier: "BOXDECK_ACK", title: "Ack", options: [])
        ]
        let category = UNNotificationCategory(identifier: "BOXDECK_ALERT", actions: actions, intentIdentifiers: [], options: [])
        UNUserNotificationCenter.current().setNotificationCategories([category])
        UNUserNotificationCenter.current().delegate = self
    }

    private func remoteAction(_ identifier: String, alert: Alert) -> AlertAction? {
        let labels = ["BOXDECK_APPROVE": "Approve", "BOXDECK_YES": "Yes", "BOXDECK_NO": "No", "BOXDECK_INTERRUPT": "Interrupt"]
        if let label = labels[identifier] { return alert.actions.first { $0.label == label } }
        if identifier == "BOXDECK_SNOOZE" { return AlertAction(label: "Snooze 2h", path: "/api/alerts/\(alert.id)/snooze", body: ["until": "2h"]) }
        if identifier == "BOXDECK_ACK" { return AlertAction(label: "Ack", path: "/api/alerts/\(alert.id)/ack") }
        return nil
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse, withCompletionHandler completionHandler: @escaping () -> Void) {
        let userInfo = response.notification.request.content.userInfo
        let value = userInfo["boxdeck_alert"] ?? userInfo["alert"]
        let alertData = (value as? [String: Any]).flatMap { JSONSerialization.isValidJSONObject($0) ? try? JSONSerialization.data(withJSONObject: $0) : nil }
        let deckURL = userInfo["deck_url"] as? String
        let token = userInfo["action_token"] as? String
        let actionIdentifier = response.actionIdentifier
        Task { @MainActor in
            self.handleRemote(alertData: alertData, deckURL: deckURL, token: token, actionIdentifier: actionIdentifier)
        }
        completionHandler()
    }

    nonisolated func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: Error?) {}
}
