# Roadmap

## Web Push for the deck PWA

Add `manifest.webmanifest`, a service worker and VAPID keys generated on first
run. Add `POST /api/push/subscribe` with the browser PushSubscription JSON and a
"Get alerts on this device" control in the Alerts Delivery tab. Store encrypted
subscription records under the same 0600 alert data directory, add a `webpush`
sink with severity and rule filters, and send the event payload from the async
delivery worker. The service worker should show the title and body, open the
alert link on click, and never send while disarmed or during quiet hours.
