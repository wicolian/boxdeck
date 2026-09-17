import SwiftUI
import BoxdeckKit

struct SettingsView: View {
    @EnvironmentObject private var model: AppModel
    @State private var boxName = ""
    @State private var boxURL = ""
    @State private var token = ""
    @State private var pairing = false
    @State private var error = ""

    var body: some View {
        NavigationStack {
            Form {
                Section("Add a box") {
                    TextField("Name", text: $boxName).textInputAutocapitalization(.never)
                    TextField("URL", text: $boxURL).textInputAutocapitalization(.never).keyboardType(.URL)
                    SecureField("Bearer token", text: $token)
                    Button("Save box") { saveBox() }.disabled(boxURL.isEmpty || token.isEmpty)
                    Button { pairing = true } label: { Label("Scan pairing QR", systemImage: "qrcode.viewfinder") }
                }
                Section("Configured boxes") {
                    ForEach(model.store.boxes) { box in HStack { VStack(alignment: .leading) { Text(box.name); Text(box.url).font(.caption).foregroundStyle(.secondary) }; Spacer(); Button("Remove", role: .destructive) { model.store.remove(box); Task { await model.refresh() } } } }
                }
                Section("Notifications") {
                    Toggle("Notify while the app is open", isOn: $model.notificationsEnabled).onChange(of: model.notificationsEnabled) { enabled in if enabled { Task { await model.requestNotifications() } } }
                    Toggle("Disarm alerts", isOn: $model.disarmed).onChange(of: model.disarmed) { _ in Task { await model.toggleDisarm() } }
                }
                Section("Quiet hours") {
                    TextField("From", text: $model.quietFrom).textInputAutocapitalization(.never)
                    TextField("To", text: $model.quietTo).textInputAutocapitalization(.never)
                    Toggle("Allow critical alerts", isOn: $model.quietAllowCritical)
                }
                Section("Foreground ntfy") {
                    TextField("Server", text: $model.ntfyURL).textInputAutocapitalization(.never).keyboardType(.URL)
                    TextField("Topic", text: $model.ntfyTopic).textInputAutocapitalization(.never)
                    SecureField("Optional ntfy token", text: $model.ntfyToken)
                    Text("Background delivery uses the ntfy app. The Boxdeck stream is foreground only.").font(.caption).foregroundStyle(.secondary)
                }
                Section("Deck") { ForEach(model.store.boxes) { box in if let url = URL(string: box.url) { Link("Launch \(box.name)", destination: url) } } }
                if !error.isEmpty { Text(error).foregroundStyle(BridgeTheme.rust) }
            }
            .navigationTitle("Settings")
            .sheet(isPresented: $pairing) { QRScannerView { value in pairing = false; handlePairing(value) } }
        }
    }

    private func saveBox() {
        do { try model.store.save(BoxConnection(name: boxName.isEmpty ? (URL(string: boxURL)?.host ?? "Box") : boxName, url: boxURL, token: token)); boxName = ""; boxURL = ""; token = ""; Task { await model.refresh() } } catch { error = error.localizedDescription }
    }

    private func handlePairing(_ url: URL) {
        do { let pairing = try PairingURL.parse(url); try model.add(pairing); Task { await model.refresh() } } catch { error = error.localizedDescription }
    }
}
