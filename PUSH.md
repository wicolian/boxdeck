# Boxdeck push delivery

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
