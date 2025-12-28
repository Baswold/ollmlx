// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "OllmlxApp",
    platforms: [
        .macOS(.v14)
    ],
    products: [
        .executable(name: "OllmlxApp", targets: ["OllmlxApp"])
    ],
    targets: [
        .executableTarget(
            name: "OllmlxApp",
            path: "Sources"
        )
    ]
)
