import SwiftUI

struct SettingsView: View {
    @ObservedObject var model: AppModel
    @State private var draft: BarConfig

    init(model: AppModel) {
        self.model = model
        _draft = State(initialValue: model.config)
    }

    var body: some View {
        Form {
            Section("Boxes") {
                ForEach(draft.boxes.indices, id: \.self) { index in
                    VStack(alignment: .leading, spacing: 6) {
                        TextField("Name", text: $draft.boxes[index].name)
                        TextField("URL", text: $draft.boxes[index].url)
                        SecureField("Token", text: $draft.boxes[index].token)
                        Button("Remove box", role: .destructive) {
                            draft.boxes.remove(at: index)
                        }
                    }
                    .padding(.vertical, 4)
                }
                Button("Add box") {
                    draft.boxes.append(BoxConfig())
                }
            }
            Section("Refresh") {
                Stepper("Every \(draft.refreshSec) seconds", value: $draft.refreshSec, in: 5...3600)
                Toggle("Notifications", isOn: $draft.notify)
                Toggle("Quiet hours", isOn: Binding(get: { draft.quiet?.enabled ?? false }, set: { enabled in
                    var quiet = draft.quiet ?? QuietHours()
                    quiet.enabled = enabled
                    draft.quiet = quiet
                }))
                if draft.quiet?.enabled == true {
                    TextField("Quiet starts", text: Binding(get: { draft.quiet?.start ?? "22:00" }, set: { value in
                        var quiet = draft.quiet ?? QuietHours()
                        quiet.start = value
                        draft.quiet = quiet
                    }))
                    TextField("Quiet ends", text: Binding(get: { draft.quiet?.end ?? "07:00" }, set: { value in
                        var quiet = draft.quiet ?? QuietHours()
                        quiet.end = value
                        draft.quiet = quiet
                    }))
                }
                Toggle("Launch at login", isOn: Binding(get: { model.launchAtLogin }, set: model.setLaunchAtLogin))
            }
            Section {
                HStack {
                    Button("Save") { model.setConfig(draft) }
                    Button("Reload") { draft = model.config }
                    Spacer()
                    Text(model.configPathDisplay)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            if let errorMessage = model.errorMessage {
                Text(errorMessage).foregroundStyle(.red)
            }
        }
        .formStyle(.grouped)
        .frame(width: 520, height: 460)
        .navigationTitle("boxdeck Settings")
    }
}
