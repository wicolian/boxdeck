# Android push setup

Boxdeck does not need a backend relay for Android alerts. The boxdeck ntfy sink
publishes the alert JSON, including its `actions` array, to a topic. The Android
app uses the UnifiedPush connector so the ntfy app can act as the distributor.

## Today: ntfy and UnifiedPush

1. Install the ntfy Android app.
2. Install the boxdeck Android app.
3. In the Android system UnifiedPush distributor chooser, choose ntfy.
4. In boxdeck, allow notifications and register the app for `boxdeck alerts`.
5. Configure an ntfy sink on each deck with the same topic you subscribe to in
   the ntfy app.

The app accepts a payload shaped like this:

```json
{
  "boxUrl": "https://box.example",
  "alert": {
    "id": "alert-id",
    "severity": "warning",
    "title": "Agent needs you",
    "body": "Permission is waiting",
    "actions": [
      {"label": "Approve", "method": "POST", "path": "/api/herd/keys", "body": {"pane": "w1:p1", "keys": "Enter"}}
    ]
  }
}
```

The alert is shown with up to three action buttons. Tapping an action calls the
deck using the encrypted token already stored for that box. The UnifiedPush
endpoint and registration errors are kept only in local app preferences.

## Future FCM relay

A future backend can relay the same payload through FCM. It should register the
device token at an authenticated deck endpoint, associate it with the box and
severity filter, and send the alert JSON plus `actions` as the FCM data payload.
The app should not need a screen change: the FCM receiver should hand the data
to the same notification builder used by `UnifiedPushReceiverService`.
