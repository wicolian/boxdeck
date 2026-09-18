import Foundation
import SwiftUI
import UserNotifications
import UIKit

@MainActor
final class AppModel: ObservableObject {
    struct DisplayDevice: Identifiable {
        var name: String
        var url: String
        var deck: BoxSnapshot?
        var peer: Peer?
        var id: String { url.isEmpty ? name : url }
        var isDeck: Bool { deck != nil || peer?.boxdeck == true }
        var online: Bool { deck?.ok ?? peer?.online ?? false }
    }

    let store = BoxStore()
    @Published var devices: [DisplayDevice] = []
    @Published var boxes: [BoxSnapshot] = []
    @Published var alerts: [Alert] = []
    @Published var usage: UsageAllResponse?
    @Published var loading = false
    @Published var message = ""
    @Published var notificationsEnabled = false
    @AppStorage("quietFrom") var quietFrom = "23:00"
    @AppStorage("quietTo") var quietTo = "08:00"
    @AppStorage("quietAllowCritical") var quietAllowCritical = true
    @AppStorage("disarmed") var disarmed = false
    @AppStorage("ntfyURL") var ntfyURL = "https://ntfy.sh"
    @AppStorage("ntfyTopic") var ntfyTopic = ""
    @AppStorage("ntfyToken") var ntfyToken = ""
    private var eventTask: Task<Void, Never>?
    private var ntfyTask: Task<Void, Never>?
    private var clients: [String: BoxdeckClient] = [:]
    private var previousAlertIDs = Set<String>()

    deinit { eventTask?.cancel(); ntfyTask?.cancel() }

    func refresh() async {
        guard !loading else { return }
        loading = true
        defer { loading = false }
        let connections = store.boxes
        clients = [:]
        var allBoxes: [BoxSnapshot] = []
        var allPeers: [Peer] = []
        var primary: BoxdeckClient?
        for connection in connections {
            guard let client = try? store.client(for: connection) else { continue }
            clients[connection.name] = client
            if primary == nil { primary = client }
            if let cards = try? await client.boxes() { allBoxes.append(contentsOf: cards) }
            if let peers = try? await client.peers() { allPeers.append(contentsOf: peers) }
        }
        boxes = uniqueBoxes(allBoxes)
        devices = mergeDevices(boxes: boxes, peers: uniquePeers(allPeers))
        if let primary {
            if let freshAlerts = try? await primary.alerts() { publishAlerts(freshAlerts) }
            usage = try? await primary.usageAll(days: 30)
            startEvents(client: primary)
            startNtfy()
        }
        if connections.isEmpty { message = "Add a box in Settings to get started." }
        else if devices.isEmpty { message = "No reachable boxes yet. Check the URL and token." }
        else { message = "" }
    }

    func client(for device: DisplayDevice) -> BoxdeckClient? {
        if let client = clients[device.name] { return client }
        if let connection = store.boxes.first(where: { $0.url == device.url || $0.name == device.name }) { return try? store.client(for: connection) }
        return clients.values.first
    }

    func action(_ action: AlertAction, for alert: Alert) async {
        guard let client = clients[alert.box] ?? clients.values.first else { return }
        do { try await client.alertAction(action); await refresh() } catch { message = error.localizedDescription }
        haptic()
    }

    func acknowledge(_ alert: Alert) async { await mutate(alert) { try await $0.acknowledge(alert: alert) } }
    func snooze(_ alert: Alert) async { await mutate(alert) { try await $0.snooze(alert: alert, until: Date().addingTimeInterval(2 * 60 * 60)) } }
    func resolve(_ alert: Alert) async { await mutate(alert) { try await $0.resolve(alert: alert) } }

    func add(_ pairing: Pairing) throws {
        try store.save(BoxConnection(name: URL(string: pairing.url)?.host ?? "Box", url: pairing.url, token: pairing.token))
    }

    func handle(_ url: URL) {
        guard url.scheme?.lowercased() == "boxdeck" else { return }
        if url.host?.lowercased() == "add", let pairing = try? PairingURL.parse(url) { try? add(pairing) }
        Task { await refresh() }
    }

    func requestNotifications() async {
        let granted = (try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound])) ?? false
        notificationsEnabled = granted
    }

    func toggleDisarm() async {
        disarmed.toggle()
        if let client = clients.values.first { try? await client.disarm(disarmed) }
    }

    private func mutate(_ alert: Alert, operation: (BoxdeckClient) async throws -> Void) async {
        guard let client = clients[alert.box] ?? clients.values.first else { return }
        do { try await operation(client); await refresh() } catch { message = error.localizedDescription }
        haptic()
    }

    private func publishAlerts(_ fresh: [Alert]) {
        let new = fresh.filter { !previousAlertIDs.contains($0.id) && $0.state == "open" }
        alerts = fresh.sorted { $0.at > $1.at }
        previousAlertIDs = Set(fresh.map(\.id))
        guard notificationsEnabled else { return }
        for alert in new.prefix(5) {
            let content = UNMutableNotificationContent()
            content.title = alert.title
            content.body = alert.body
            content.sound = .default
            let request = UNNotificationRequest(identifier: "boxdeck-\(alert.id)", content: content, trigger: nil)
            UNUserNotificationCenter.current().add(request)
        }
    }

    private func startEvents(client: BoxdeckClient) {
        guard eventTask == nil else { return }
        eventTask = Task { [weak self] in
            guard let self else { return }
            do {
                for try await _ in client.events() {
                    await self.refresh()
                }
            } catch {
            }
            self.eventTask = nil
        }
    }

    private func startNtfy() {
        guard !ntfyTopic.isEmpty, ntfyTask == nil, let url = URL(string: ntfyURL) else { return }
        ntfyTask = Task { [weak self] in
            guard let self else { return }
            let client = NtfyClient()
            do {
                for try await _ in client.stream(server: url, topic: self.ntfyTopic, token: self.ntfyToken) { await self.refresh() }
            } catch {
            }
            self.ntfyTask = nil
        }
    }

    private func uniqueBoxes(_ values: [BoxSnapshot]) -> [BoxSnapshot] {
        var seen = Set<String>()
        return values.filter { seen.insert($0.url).inserted }
    }

    private func uniquePeers(_ values: [Peer]) -> [Peer] {
        var seen = Set<String>()
        return values.filter { seen.insert($0.id).inserted }
    }

    private func mergeDevices(boxes: [BoxSnapshot], peers: [Peer]) -> [DisplayDevice] {
        let decks = boxes.map { box in DisplayDevice(name: box.name, url: box.url, deck: box, peer: peers.first { peer in peer.url == box.url }) }
        let deckURLs = Set(boxes.map(\.url))
        let plain = peers.filter { !$0.boxdeck || !deckURLs.contains($0.url) }.map { DisplayDevice(name: $0.name, url: $0.url, deck: nil, peer: $0) }
        return (decks + plain).sorted { lhs, rhs in
            if lhs.deck?.local == true { return true }
            if rhs.deck?.local == true { return false }
            if lhs.isDeck != rhs.isDeck { return lhs.isDeck }
            if lhs.online != rhs.online { return lhs.online }
            return lhs.name.localizedCaseInsensitiveCompare(rhs.name) == .orderedAscending
        }
    }

    private func haptic() { UIImpactFeedbackGenerator(style: .light).impactOccurred() }
}
