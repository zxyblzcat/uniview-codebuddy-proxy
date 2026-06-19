// swift-tools-version: 6.1
import PackageDescription

let package = Package(
    name: "UniviewCodeBuddyProxy",
    platforms: [
        .macOS(.v14)
    ],
    products: [
        .executable(
            name: "UniviewCodeBuddyProxy",
            targets: ["UniviewCodeBuddyProxy"]
        )
    ],
    dependencies: [
        .package(url: "https://github.com/hummingbird-project/hummingbird.git", from: "2.0.0"),
    ],
    targets: [
        .executableTarget(
            name: "UniviewCodeBuddyProxy",
            dependencies: [
                .product(name: "Hummingbird", package: "hummingbird"),
            ]
        )
    ]
)
