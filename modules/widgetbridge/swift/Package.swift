// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Widgetbridge",
    platforms: [.iOS(.v17)],
    products: [.library(name: "Widgetbridge", type: .dynamic, targets: ["Widgetbridge"])],
    dependencies: [],
    targets: [
        .target(name: "Widgetbridge"),
    ],
    swiftLanguageModes: [.v5]
)
