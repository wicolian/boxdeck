// swift-tools-version: 6.3

import PackageDescription

let package = Package(
    name: "BoxdeckBar",
    platforms: [
        .macOS(.v13)
    ],
    products: [
        .executable(name: "BoxdeckBar", targets: ["BoxdeckBar"])
    ],
    targets: [
        .executableTarget(
            name: "BoxdeckBar",
            path: "macos/BoxdeckBar",
            swiftSettings: [.swiftLanguageMode(.v5)]
        ),
        .testTarget(
            name: "BoxdeckBarTests",
            dependencies: ["BoxdeckBar"],
            path: "macos/BoxdeckBarTests",
            resources: [.copy("Fixtures")],
            swiftSettings: [.swiftLanguageMode(.v5)]
        )
    ]
)
