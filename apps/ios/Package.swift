// swift-tools-version: 6.2

import PackageDescription

let package = Package(
    name: "Cytisus",
    defaultLocalization: "en",
    platforms: [
        .iOS(.v26),
        .macOS(.v26),
    ],
    products: [
        .executable(name: "CytisusApp", targets: ["CytisusApp"]),
    ],
    targets: [
        .executableTarget(name: "CytisusApp"),
        .testTarget(name: "CytisusAppTests", dependencies: ["CytisusApp"]),
    ]
)
