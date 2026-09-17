import AppKit
import Combine
import Foundation

@MainActor
final class AppModel: ObservableObject {
    @Published private(set) var config: BarConfig
    @Published private(set) var menu: MenuModel
    @Published private(set) var isRefreshing = false
    @Published private(set) var errorMessage: String?
    @Published var isDisarmed = false
    @Published private(set) var launchAtLogin = LaunchAtLoginController.isEnabled

    private let store: ConfigStore
    private let notifications = NotificationController()
    private var timerTask: Task<Void, Never>?
    private var previousSnapshot: FleetSnapshot?
    private var lastNotificationAt: [String: Date] = [:]

    init(store: ConfigStore = ConfigStore()) {
        self.store = store
        config = (try? store.load()) ?? .default
        menu = MenuModelBuilder.build(boxes: [], alerts: [], localUsage: nil)
        notifications.actionHandler = { [weak self] action, boxURL, alertID in
            Task { @MainActor [weak self] in
                await self?.handleNotificationAction(action, boxURL: boxURL, alertID: alertID)
            }
        }
        notifications.configure(enabled: config.notify)
        refresh()
        startTimer()
    }

    deinit {
        timerTask?.cancel()
    }

    var iconSystemName: String {
        switch menu.iconState {
        case .healthy: return "server.rack"
        case .attention: return "server.rack.badge.exclamationmark"
        case .rust: return "server.rack.badge.xmark"
        }
    }

    var quietHours: QuietHours {
        config.quiet ?? QuietHours()
    }

    var quietEnabled: Bool { config.quiet?.enabled ?? false }

    var configPathDisplay: String { store.path.path }

    func refresh() {
        guard !isRefreshing else { return }
        isRefreshing = true
        let currentConfig = config
        Task { [weak self] in
            let snapshot = await FleetLoader.load(config: currentConfig)
            let localData = await Task.detached(priority: .utility) { try? LocalUsageReader.readData() }.value
            let localUsage = localData.flatMap { try? JSONDecoder().decode(UsageResponse.self, from: $0) }
            self?.apply(snapshot: snapshot, localUsage: localUsage, config: currentConfig)
        }
    }

    func setConfig(_ value: BarConfig) {
        do {
            try store.save(value)
            config = value
            notifications.configure(enabled: value.notify)
            refresh()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func addBox(name: String, url: String, token: String) {
        var value = config
        value.boxes.append(BoxConfig(name: name.trimmingCharacters(in: .whitespacesAndNewlines), url: url.trimmingCharacters(in: .whitespacesAndNewlines), token: token))
        setConfig(value)
    }

    func removeBox(at offsets: IndexSet) {
        var value = config
        for index in offsets.sorted(by: >) {
            value.boxes.remove(at: index)
        }
        setConfig(value)
    }

    func setQuietEnabled(_ enabled: Bool) {
        var value = config
        var quiet = value.quiet ?? QuietHours()
        quiet.enabled = enabled
        value.quiet = quiet
        setConfig(value)
    }

    func setQuietHours(start: String, end: String) {
        var value = config
        var quiet = value.quiet ?? QuietHours()
        quiet.start = start
        quiet.end = end
        value.quiet = quiet
        setConfig(value)
    }

    func toggleDisarmed() {
        isDisarmed.toggle()
        guard let first = config.boxes.first, let client = BoxdeckClient(baseURL: first.url, token: first.token) else { return }
        let value = isDisarmed
        Task { [weak self] in
            do {
                _ = try await client.disarm(value)
            } catch {
                await MainActor.run { self?.errorMessage = error.localizedDescription }
            }
        }
    }

    func setLaunchAtLogin(_ enabled: Bool) {
        do {
            try LaunchAtLoginController.setEnabled(enabled)
            launchAtLogin = LaunchAtLoginController.isEnabled
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func open(_ url: String) {
        guard let target = URL(string: url) else { return }
        NSWorkspace.shared.open(target)
    }

    func openSettings() {
        NSApp.sendAction(Selector(("showSettingsWindow:")), to: nil, from: nil)
    }

    func ack(_ alert: Alert) {
        let action = AlertAction(id: "ack", title: "Ack", path: nil)
        perform(action, for: alert, fallback: "ack")
    }

    func perform(_ action: AlertAction, for alert: Alert) {
        perform(action, for: alert, fallback: nil)
    }

    func quit() {
        NSApp.terminate(nil)
    }

    private func startTimer() {
        timerTask = Task { [weak self] in
            while !Task.isCancelled {
                let seconds = max(5, self?.config.refreshSec ?? 30)
                try? await Task.sleep(nanoseconds: UInt64(seconds) * 1_000_000_000)
                if Task.isCancelled { return }
                self?.refresh()
            }
        }
    }

    private func apply(snapshot: FleetSnapshot, localUsage: UsageResponse?, config: BarConfig) {
        if config.notify {
            for event in NotificationDecider.newEvents(previous: previousSnapshot, current: snapshot) {
                let last = lastNotificationAt[event.boxURL]
                let quiet = config.quiet?.contains(Date()) ?? false
                if !isDisarmed && !quiet && (last == nil || Date().timeIntervalSince(last!) >= 300) {
                    notifications.send(event)
                    lastNotificationAt[event.boxURL] = Date()
                }
            }
        }
        previousSnapshot = snapshot
        menu = MenuModelBuilder.build(boxes: snapshot.boxes, alerts: snapshot.alerts, localUsage: localUsage)
        isRefreshing = false
    }

    private func handleNotificationAction(_ action: String, boxURL: String, alertID: String?) async {
        guard let box = config.boxes.first(where: { $0.url.trimmingCharacters(in: CharacterSet(charactersIn: "/")) == boxURL.trimmingCharacters(in: CharacterSet(charactersIn: "/")) }), let client = BoxdeckClient(baseURL: box.url, token: box.token) else { return }
        guard let alertID, let escaped = alertID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) else { return }
        let suffix = action == "BOXDECK_SNOOZE" ? "snooze" : "ack"
        try? await client.post(path: "/api/alerts/\(escaped)/\(suffix)")
        refresh()
    }

    private func perform(_ action: AlertAction, for alert: Alert, fallback: String?) {
        guard let box = config.boxes.first(where: { $0.url.trimmingCharacters(in: CharacterSet(charactersIn: "/")) == alert.boxURL.trimmingCharacters(in: CharacterSet(charactersIn: "/")) }), let client = BoxdeckClient(baseURL: box.url, token: box.token) else { return }
        let path: String
        if let actionPath = action.path, !actionPath.isEmpty {
            path = actionPath
        } else if let fallback {
            let escaped = alert.id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? alert.id
            path = "/api/alerts/\(escaped)/\(fallback)"
        } else {
            return
        }
        Task { [weak self] in
            do {
                try await client.post(path: path, method: action.method)
                self?.refresh()
            } catch {
                await MainActor.run { self?.errorMessage = error.localizedDescription }
            }
        }
    }
}
