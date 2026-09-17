// swift-tools-version: 5.10

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
            path: "macos/BoxdeckBar"
        ),
        .testTarget(
            name: "BoxdeckBarTests",
            dependencies: ["BoxdeckBar"],
            path: "macos/BoxdeckBarTests",
            resources: [.copy("Fixtures")]
        )
    ]
)
