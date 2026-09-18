# iOS and APNs setup

1. Open Apple Developer at https://developer.apple.com/account and sign in to the team that owns the app.
2. Open Certificates, Identifiers and Profiles, choose Keys, and select the plus button.
3. Name the key `Boxdeck APNs`, select Apple Push Notifications service (APNs), and select Continue.
4. Select Register, then Download the `.p8` file once and keep it in a private location.
5. Copy the Key ID shown for the key and copy the Team ID from the Membership page.
6. Open `apple/project.yml`, set the bundle prefix and bundle identifier to your team values, and regenerate the Xcode project with XcodeGen.
7. Open the generated `apple/Boxdeck.xcodeproj` in Xcode, select the iOS Boxdeck target, and set your Apple Developer team under Signing and Capabilities.
8. Connect an iPhone, select it as the Xcode run destination, trust the device, and run Boxdeck until it registers for remote notifications.
9. Open the deck Settings view, open Phone push, paste the Key ID, Team ID, and `dev.wicolian.boxdeck` bundle ID, and choose sandbox for a development phone.
10. Choose the downloaded `.p8` file, select Upload, confirm the registered iPhone appears, and select Test.
