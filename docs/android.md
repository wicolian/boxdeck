# Android and Wear OS

The Android build is produced by GitHub Actions because this checkout does not
have an Android SDK or emulator.

## Phone screens

- Needs you: newest open alerts with Approve, Yes, No, Interrupt, Ack, Snooze,
  and Resolve actions, plus the quiet empty state.
- Boxes: the local deck first, every deck card, and every Tailscale peer from
  `/api/net/peers`. Plain peers show OS, online state, last seen, and either
  `phone` or `install boxdeck`.
- Box detail: Overview, Agents with pane tail and prompt, Ports with links,
  Usage with 30 day rows, Apps start and stop, Terminal, and Files.
- Usage: provider totals, quota bars, and sessions across the selected fleet.
- Settings: URL and token add, QR scan, notification toggle, quiet hours,
  disarm, deck launch, and encrypted box removal.

## Wear OS screens

- Needs you: large action chips for alert actions.
- Boxes: CPU, agents, memory and quota for synced decks.
- Tile: needs-you count, first box name, CPU, and agent count, refreshed every
  15 minutes.
- Complication: short needs-you count, refreshed by the system.

## Sideloading

Download the `app-debug.apk` artifact from the Android workflow, then run:

```sh
adb install app-debug.apk
```

Download the `wear-debug.apk` artifact for a Wear emulator or watch and run:

```sh
adb install wear-debug.apk
```

Debug signing is automatic. No emulator screenshots are included because the
build host has no Android SDK or emulator. The pull request describes each
phone and Wear screen in words for the first real-device review.
