// swift-tools-version: 6.0

import PackageDescription

let package = Package(
    name: "Cytisus",
    defaultLocalization: "en",
    platforms: [
        .iOS(.v17),
        .macOS(.v14),
    ],
    products: [
        .executable(name: "CytisusApp", targets: ["CytisusApp"]),
    ],
    targets: [
        .executableTarget(name: "CytisusApp"),
        .testTarget(name: "CytisusAppTests", dependencies: ["CytisusApp"]),
    ]
)
