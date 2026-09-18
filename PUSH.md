# Boxdeck push delivery

The current app uses the ntfy sink for background delivery. Install the ntfy app on the iPhone, subscribe to the same topic configured on the box, and enable notifications. The Boxdeck app also listens to the ntfy HTTP JSON stream while it is in the foreground so the Needs You screen updates without a refresh.

The server has no APNs relay today. A future relay can be added without changing the screens with this contract:

1. The deck exposes `POST /api/push/subscribe` for an authenticated device token. The request contains `{ "platform": "ios" | "watchos", "deviceToken": "...", "bundle": "dev.wicolian.boxdeck" }`.
2. The deck stores the token per authenticated phone or watch pairing and returns `{ "ok": true, "id": "..." }`.
3. The relay sends an APNs payload whose `aps.alert` contains the alert title and body, `aps.category` is `BOXDECK_ALERT`, and the top level contains `alert` with the complete alert JSON plus `actions` with the same label, method, path, and body fields returned by `/api/alerts`.
4. The app registers the `BOXDECK_ALERT` category with Approve and Snooze actions. A future relay therefore only needs token registration and delivery. No view contract changes are required.
