import SwiftUI

enum BridgeTheme {
    static let hull = Color(red: 0.043, green: 0.055, blue: 0.075)
    static let deck = Color(red: 0.063, green: 0.078, blue: 0.106)
    static let ink = Color(red: 0.91, green: 0.92, blue: 0.90)
    static let amber = Color(red: 0.91, green: 0.64, blue: 0.24)
    static let moss = Color(red: 0.45, green: 0.68, blue: 0.48)
    static let rust = Color(red: 0.85, green: 0.41, blue: 0.35)
}

struct BridgeCard<Content: View>: View {
    @ViewBuilder var content: Content
    var body: some View { content.padding(16).frame(maxWidth: .infinity, alignment: .leading).background(BridgeTheme.deck).overlay(RoundedRectangle(cornerRadius: 8).stroke(BridgeTheme.ink.opacity(0.14))) }
}

struct MonoText: View {
    let value: String
    var body: some View { Text(value).font(.system(.body, design: .monospaced)).foregroundStyle(BridgeTheme.ink) }
}
