import SwiftUI
import WebKit

struct BoxWebView: UIViewRepresentable {
    let url: String
    let token: String
    func makeUIView(context: Context) -> WKWebView { WKWebView(frame: .zero) }
    func updateUIView(_ webView: WKWebView, context: Context) {
        guard let url = URL(string: url), webView.url?.absoluteString != url.absoluteString else { return }
        var request = URLRequest(url: url, timeoutInterval: 5)
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        webView.load(request)
    }
}
