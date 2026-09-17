import Foundation

enum ConfigError: LocalizedError {
    case invalid(String)

    var errorDescription: String? {
        switch self {
        case .invalid(let message): return message
        }
    }
}

struct ConfigStore {
    let path: URL

    init(path: URL = ConfigStore.defaultPath()) {
        self.path = path
    }

    static func defaultPath(home: URL = FileManager.default.homeDirectoryForCurrentUser) -> URL {
        home.appendingPathComponent(".config/boxdeck/bar.json")
    }

    func load() throws -> BarConfig {
        guard FileManager.default.fileExists(atPath: path.path) else { return .default }
        let data = try Data(contentsOf: path)
        let config = try JSONDecoder().decode(BarConfig.self, from: data)
        try validate(config)
        return config
    }

    func save(_ config: BarConfig) throws {
        try validate(config)
        let directory = path.deletingLastPathComponent()
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        let data = encoder.encode(config)
        let temporary = directory.appendingPathComponent(".bar.json.\(UUID().uuidString).tmp")
        try data.write(to: temporary, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: temporary.path)
        if FileManager.default.fileExists(atPath: path.path) {
            _ = try FileManager.default.replaceItemAt(path, withItemAt: temporary)
        } else {
            try FileManager.default.moveItem(at: temporary, to: path)
        }
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: path.path)
    }

    func validate(_ config: BarConfig) throws {
        guard (5...3600).contains(config.refreshSec) else {
            throw ConfigError.invalid("refreshSec must be between 5 and 3600")
        }
        guard config.openWith.isEmpty || config.openWith == "browser" else {
            throw ConfigError.invalid("openWith must be browser")
        }
        var seen = Set<String>()
        for (index, box) in config.boxes.enumerated() {
            guard !box.name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
                throw ConfigError.invalid("boxes[\(index)].name must be nonempty")
            }
            guard let url = URL(string: box.url), ["http", "https"].contains(url.scheme?.lowercased()) else {
                throw ConfigError.invalid("boxes[\(index)].url must be an http(s) URL")
            }
            let normalized = url.absoluteString.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
            guard seen.insert(normalized).inserted else {
                throw ConfigError.invalid("boxes[\(index)].url is duplicated")
            }
        }
    }
}
