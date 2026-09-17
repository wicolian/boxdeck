import Foundation
import SwiftUI
import WatchConnectivity
import UserNotifications
import BoxdeckKit

@MainActor
final class WatchModel: NSObject, ObservableObject, WCSessionDelegate {
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

    func handle(_ userInfo: [AnyHashable: Any]) {
        guard let id = userInfo["alert_id"] as? String, let alert = alerts.first(where: { $0.id == id }) else { return }
        Task { await snooze(alert) }
    }

    private func unique(_ values: [BoxSnapshot]) -> [BoxSnapshot] { var seen = Set<String>(); return values.filter { seen.insert($0.url).inserted } }

    private func transferToPhone() {
        guard WCSession.default.activationState == .activated else { return }
        let payload = boxes.map { ["name": $0.name, "url": $0.url, "ok": $0.ok] }
        try? WCSession.default.updateApplicationContext(["boxes": payload])
        WCSession.default.transferUserInfo(["boxes": payload])
    }

    private func registerNotificationActions() {
        let approve = UNNotificationAction(identifier: "BOXDECK_APPROVE", title: "Approve", options: [.foreground])
        let snooze = UNNotificationAction(identifier: "BOXDECK_SNOOZE", title: "Snooze", options: [])
        let category = UNNotificationCategory(identifier: "BOXDECK_ALERT", actions: [approve, snooze], intentIdentifiers: [])
        UNUserNotificationCenter.current().setNotificationCategories([category])
    }

    nonisolated func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: Error?) {}
    nonisolated func sessionDidBecomeInactive(_ session: WCSession) {}
    nonisolated func sessionDidDeactivate(_ session: WCSession) { session.activate() }
}
