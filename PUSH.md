# Boxdeck push delivery

Three paths deliver alerts to a device: native APNs to the iOS and watchOS apps,
Web Push to any subscribed browser, and ntfy as the fallback. All three carry
the same per alert action token. The deck never sends while disarmed or during
quiet hours; that decision is made once in the alert manager, not per sink.

Native APNs delivery is the primary phone path. The deck signs provider JWTs
with the configured ES256 `.p8` key and sends directly to Apple over the APNs
HTTP/2 provider API. There is no relay and no third party in this path.

Configure the `apns` block in the deck config, or use Settings > Phone push:

```json
{
  "apns": {
    "keyPath": "~/.config/boxdeck/apns.p8",
    "keyId": "ABC123",
    "teamId": "TEAMID",
    "bundleId": "dev.wicolian.boxdeck",
    "environment": "sandbox"
  }
}
```

The deck stores registered iOS and watchOS device tokens in
`~/.local/share/boxdeck/push.json` with mode 0600. The GET push response only
returns a short token suffix. The `.p8` file is stored with mode 0600 and is
never returned by the API.

Authenticated push endpoints are:

```text
POST   /api/push/register
DELETE /api/push/register
GET    /api/push
POST   /api/push/test
POST   /api/push/config
POST   /api/push/key
```

Register with `{ "platform": "ios", "token": "...", "name": "phone", "bundleId": "dev.wicolian.boxdeck" }`.
Delete with the returned `id`, or with the original `platform` and `token`.
The test endpoint sends a native test alert to every registered device.

Every alert uses category `BOXDECK_ALERT`. The APNs payload includes the full
alert JSON, the deck URL, the per-alert action token, and the alert actions.
Critical alerts use the default sound. Critical and warning alerts use the
time-sensitive interruption level. The body is trimmed to keep the whole
payload at or below 4096 bytes. A 410 `Unregistered` response removes the
device token.

The iOS app registers Approve, Yes, No, Interrupt, Snooze 2h, and Ack actions.
The response handler uses the action token in the payload and a background
URLSession, so an action can run from the lock screen. Tapping the notification
opens the alert in Needs You. The watch extension registers the same category,
acts directly against the deck, and refreshes the complication on its normal
15 minute timeline.

ntfy remains an optional sink if you do not want to run the iOS app. It keeps
the existing topic, action, and quiet-hour behavior.

## Web Push

The deck generates a VAPID P-256 key pair on first run and stores it as PKCS8 in
`~/.local/share/boxdeck/vapid.json` with mode 0600. Browser subscriptions live
in `~/.local/share/boxdeck/webpush.json`, sealed with AES-256-GCM under a key
derived from the deck session secret, so the data directory alone cannot send
to a browser. The `webpush` sink appears in Delivery while at least one browser
is subscribed and disappears when the last one leaves.

Authenticated endpoints are:

```text
GET    /api/push/web
POST   /api/push/web/subscribe
DELETE /api/push/web/subscribe
POST   /api/push/web/test
```

The service worker at `/sw.js` shows the notification, runs a tapped action
against the deck with the action token, and focuses or opens the deck at the
alert link on a plain tap. It has no fetch handler on purpose: the deck is live
data and is never served from a cache. A `pushsubscriptionchange` event
resubscribes with the current public key and re-registers with the deck.

Web Push only works from a secure context. Open the deck at `http://localhost`
on the box, or serve it over https behind Tailscale or a reverse proxy.
