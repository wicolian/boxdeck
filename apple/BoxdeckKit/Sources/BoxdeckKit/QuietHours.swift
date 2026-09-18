import Foundation

public struct QuietHours: Codable, Sendable, Hashable {
    public var from: String
    public var to: String
    public var allowCritical: Bool
    public init(from: String = "23:00", to: String = "08:00", allowCritical: Bool = true) { self.from = from; self.to = to; self.allowCritical = allowCritical }

    public func contains(_ date: Date = Date(), calendar: Calendar = .current) -> Bool {
        guard let start = minute(from), let end = minute(to) else { return false }
        let components = calendar.dateComponents([.hour, .minute], from: date)
        let current = (components.hour ?? 0) * 60 + (components.minute ?? 0)
        if start <= end { return current >= start && current < end }
        return current >= start || current < end
    }

    private func minute(_ value: String) -> Int? {
        let parts = value.split(separator: ":")
        guard parts.count == 2, let hour = Int(parts[0]), let minute = Int(parts[1]), (0..<24).contains(hour), (0..<60).contains(minute) else { return nil }
        return hour * 60 + minute
    }
}

public enum NotificationPolicy {
    public static func shouldDeliver(severity: String, quiet: QuietHours, disarmed: Bool, date: Date = Date()) -> Bool {
        if disarmed { return false }
        if !quiet.contains(date) { return true }
        return severity == "critical" && quiet.allowCritical
    }
}
