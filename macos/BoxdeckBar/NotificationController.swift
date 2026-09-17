import Foundation
import UserNotifications

final class NotificationController: NSObject, UNUserNotificationCenterDelegate {
    private let center = UNUserNotificationCenter.current()
    var actionHandler: ((String, String, String?) -> Void)?

    func configure(enabled: Bool) {
        guard enabled else { return }
        let ack = UNNotificationAction(identifier: "BOXDECK_ACK", title: "Ack", options: [])
        let snooze = UNNotificationAction(identifier: "BOXDECK_SNOOZE", title: "Snooze", options: [])
        let category = UNNotificationCategory(identifier: "BOXDECK_ALERT", actions: [ack, snooze], intentIdentifiers: [], options: [])
        center.setNotificationCategories([category])
        center.delegate = self
        Task {
            _ = try? await center.requestAuthorization(options: [.alert, .sound])
        }
    }

    func send(_ event: NotificationEvent) {
        let content = UNMutableNotificationContent()
        content.title = event.title
        content.body = event.body
        content.sound = .default
        content.categoryIdentifier = "BOXDECK_ALERT"
        content.userInfo = [
            "boxURL": event.boxURL,
            "kind": event.kind.rawValue,
            "alertID": event.alertID as Any
        ]
        let request = UNNotificationRequest(identifier: "boxdeck-\(event.kind.rawValue)-\(event.boxURL)-\(UUID().uuidString)", content: content, trigger: nil)
        center.add(request)
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse, withCompletionHandler completionHandler: @escaping () -> Void) {
        let info = response.notification.request.content.userInfo
        let boxURL = info["boxURL"] as? String ?? ""
        let alertID = info["alertID"] as? String
        switch response.actionIdentifier {
        case "BOXDECK_ACK", "BOXDECK_SNOOZE":
            actionHandler?(response.actionIdentifier, boxURL, alertID)
        default:
            break
        }
        completionHandler()
    }
}
