// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Photosaver",
    platforms: [.iOS(.v17)],
    products: [.library(name: "Photosaver", type: .dynamic, targets: ["Photosaver"])],
    dependencies: [],
    targets: [
        .target(name: "Photosaver"),
    ],
    swiftLanguageModes: [.v5]
)
