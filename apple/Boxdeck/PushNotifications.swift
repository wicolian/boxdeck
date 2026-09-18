import Foundation
import UIKit
import UserNotifications

private enum BoxdeckNotificationAction {
    static let category = "BOXDECK_ALERT"
    static let approve = "BOXDECK_APPROVE"
    static let yes = "BOXDECK_YES"
    static let no = "BOXDECK_NO"
    static let interrupt = "BOXDECK_INTERRUPT"
    static let snooze = "BOXDECK_SNOOZE"
    static let ack = "BOXDECK_ACK"

    static func register() {
        let actions = [
            UNNotificationAction(identifier: approve, title: "Approve", options: []),
            UNNotificationAction(identifier: yes, title: "Yes", options: []),
            UNNotificationAction(identifier: no, title: "No", options: []),
            UNNotificationAction(identifier: interrupt, title: "Interrupt", options: []),
            UNNotificationAction(identifier: snooze, title: "Snooze 2h", options: []),
            UNNotificationAction(identifier: ack, title: "Ack", options: [])
        ]
        let notificationCategory = UNNotificationCategory(identifier: category, actions: actions, intentIdentifiers: [], options: [])
        UNUserNotificationCenter.current().setNotificationCategories([notificationCategory])
    }

    static func action(identifier: String, alert: Alert) -> AlertAction? {
        switch identifier {
        case approve, yes, no, interrupt:
            let label = [approve: "Approve", yes: "Yes", no: "No", interrupt: "Interrupt"][identifier] ?? ""
            return alert.actions.first(where: { $0.label == label })
        case snooze:
            return AlertAction(label: "Snooze 2h", method: "POST", path: "/api/alerts/\(alert.id)/snooze", body: ["until": "2h"])
        case ack:
            return AlertAction(label: "Ack", method: "POST", path: "/api/alerts/\(alert.id)/ack")
        default:
            return nil
        }
    }
}

private final class PushActionTransport: NSObject, URLSessionDataDelegate, @unchecked Sendable {
    static let shared = PushActionTransport()
    private lazy var session: URLSession = {
        let configuration = URLSessionConfiguration.background(withIdentifier: "dev.wicolian.boxdeck.push-actions")
        configuration.sessionSendsLaunchEvents = true
        return URLSession(configuration: configuration, delegate: self, delegateQueue: nil)
    }()

    func send(action: AlertAction, deckURL: String, token: String) {
        guard let base = URL(string: deckURL), let url = URL(string: action.path, relativeTo: base)?.absoluteURL else { return }
        guard let body = try? JSONSerialization.data(withJSONObject: action.body) else { return }
        var request = URLRequest(url: url)
        request.httpMethod = action.method
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = body
        session.dataTask(with: request).resume()
    }
}

@MainActor
final class BoxdeckAppDelegate: NSObject, UIApplicationDelegate, @preconcurrency UNUserNotificationCenterDelegate {
    func application(_ application: UIApplication, didFinishLaunchingWithOptions options: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        BoxdeckNotificationAction.register()
        UNUserNotificationCenter.current().delegate = self
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        BoxdeckPushRegistration.register(tokenData: deviceToken, platform: "ios", name: UIDevice.current.name)
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {}

    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification, withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        completionHandler([.banner, .sound, .badge])
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse, withCompletionHandler completionHandler: @escaping () -> Void) {
        let userInfo = response.notification.request.content.userInfo
        guard let alert = decodeAlert(userInfo), let deckURL = userInfo["deck_url"] as? String else {
            completionHandler()
            return
        }
        if response.actionIdentifier == UNNotificationDefaultActionIdentifier {
            if let url = URL(string: deckURL + "/#/alerts") {
                DispatchQueue.main.async { UIApplication.shared.open(url) }
            }
        } else if let token = userInfo["action_token"] as? String, let action = BoxdeckNotificationAction.action(identifier: response.actionIdentifier, alert: alert) {
            PushActionTransport.shared.send(action: action, deckURL: deckURL, token: token)
        }
        completionHandler()
    }

    private func decodeAlert(_ userInfo: [AnyHashable: Any]) -> Alert? {
        let value = userInfo["boxdeck_alert"] ?? userInfo["alert"]
        guard let dictionary = value as? [String: Any], JSONSerialization.isValidJSONObject(dictionary), let data = try? JSONSerialization.data(withJSONObject: dictionary) else { return nil }
        return try? JSONDecoder().decode(Alert.self, from: data)
    }
}
