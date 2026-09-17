import SwiftUI

struct MenuPopover: View {
    @ObservedObject var model: AppModel
    @State private var newName = ""
    @State private var newURL = ""
    @State private var newToken = ""

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                header
                if !model.menu.alerts.isEmpty { alertsSection }
                if model.menu.addBox { addBoxSection }
                ForEach(model.menu.boxes) { box in
                    boxCard(box)
                }
                if let localUsageTitle = model.menu.localUsageTitle {
                    Text(localUsageTitle)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                controls
                if let errorMessage = model.errorMessage {
                    Text(errorMessage)
                        .font(.caption)
                        .foregroundStyle(.red)
                        .textSelection(.enabled)
                }
            }
            .padding(16)
        }
        .frame(width: 390, idealHeight: 520)
    }

    private var header: some View {
        HStack {
            Image(systemName: model.iconSystemName)
                .font(.title3)
            VStack(alignment: .leading, spacing: 2) {
                Text("boxdeck")
                    .font(.headline)
                Text(model.menu.tooltip)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            if model.isRefreshing { ProgressView().controlSize(.small) }
        }
    }

    private var alertsSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Needs you")
                .font(.headline)
            ForEach(model.menu.alerts) { alert in
                VStack(alignment: .leading, spacing: 6) {
                    Text(alert.title).font(.subheadline.weight(.semibold))
                    if !alert.message.isEmpty { Text(alert.message).font(.caption).foregroundStyle(.secondary) }
                    HStack {
                        Button("Ack") { model.ack(alert) }
                        if let action = alert.actions.first {
                            Button(action.title) { model.perform(action, for: alert) }
                        }
                    }
                }
                .padding(10)
                .background(.quaternary, in: RoundedRectangle(cornerRadius: 8))
            }
        }
    }

    private var addBoxSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Add a box").font(.headline)
            TextField("Name", text: $newName)
            TextField("URL", text: $newURL)
            SecureField("Token", text: $newToken)
            Button("Save box") {
                model.addBox(name: newName, url: newURL, token: newToken)
                newName = ""
                newURL = ""
                newToken = ""
            }
            .disabled(newName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || newURL.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
    }

    private func boxCard(_ box: MenuBox) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            Button(box.title) { model.open(box.url) }
                .buttonStyle(.link)
                .font(.headline)
            ForEach(box.lines) { line in
                if let action = line.action {
                    Button {
                        switch action {
                        case .open(let url), .agents(let url), .usage(let url): model.open(url)
                        }
                    } label: {
                        Text(line.title)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .buttonStyle(.plain)
                    .foregroundStyle(.secondary)
                } else {
                    Text(line.title)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .padding(10)
        .background(.quaternary, in: RoundedRectangle(cornerRadius: 8))
    }

    private var controls: some View {
        VStack(alignment: .leading, spacing: 8) {
            Divider()
            HStack {
                Button("Refresh") { model.refresh() }
                Button("Settings") { model.openSettings() }
                Spacer()
                Button("Quit") { model.quit() }
            }
            Toggle("Quiet hours", isOn: Binding(get: { model.quietEnabled }, set: model.setQuietEnabled))
            Toggle("Disarm", isOn: Binding(get: { model.isDisarmed }, set: { _ in model.toggleDisarmed() }))
            Toggle("Launch at login", isOn: Binding(get: { model.launchAtLogin }, set: model.setLaunchAtLogin))
        }
    }
}
