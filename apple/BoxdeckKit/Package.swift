// swift-tools-version: 6.3
import PackageDescription

let package = Package(
    name: "BoxdeckKit",
    platforms: [
        .iOS(.v17),
        .watchOS(.v10),
        .macOS(.v13)
    ],
    products: [
        .library(name: "BoxdeckKit", targets: ["BoxdeckKit"])
    ],
    targets: [
        .target(name: "BoxdeckKit"),
        .testTarget(name: "BoxdeckKitTests", dependencies: ["BoxdeckKit"], resources: [.copy("Fixtures")])
    ]
)
