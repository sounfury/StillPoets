import CoreGraphics
import CoreText
import Foundation
import Security
import SwiftUI

struct WidgetThemePayload: Codable, Equatable {
    var preset: String?
    var fontFamily: String?
    var fontFile: String?
    var bgColor: String?
    var textColor: String?
    var accentColor: String?

    static let fallback = WidgetThemePayload(
        preset: "artbook",
        fontFamily: nil,
        fontFile: nil,
        bgColor: "#F7F3E9",
        textColor: "#241F1A",
        accentColor: "#B35C3E"
    )
}

struct WidgetEntryPayload: Codable, Equatable {
    var entryType: String?
    var displayText: String?
    var poemTitle: String?
    var author: String?
    var sourceTitle: String?
    var deeplink: String?

    var isQuote: Bool {
        (entryType ?? "").lowercased() == "quote"
            || (deeplink ?? "").hasPrefix("stillpoets://quote/")
    }

    var tagLabel: String {
        isQuote ? "散页" : "诗笺"
    }

    var actionHint: String {
        isQuote ? "看散页 ›" : "读全诗 ›"
    }

    var metaLine: String {
        var parts: [String] = []
        let title = (poemTitle ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        let source = (sourceTitle ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        let writer = (author ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        if !title.isEmpty {
            parts.append("《\(title)》")
        } else if !source.isEmpty {
            parts.append("《\(source)》")
        }
        if !writer.isEmpty {
            parts.append(writer)
        }
        return parts.isEmpty ? "StillPoets" : parts.joined(separator: " · ")
    }

    static let emptyPlaceholder = WidgetEntryPayload(
        entryType: "poem",
        displayText: "纸上有风，留一首诗给自己。",
        poemTitle: nil,
        author: nil,
        sourceTitle: "打开 StillPoets 导入诗集或记一张散页",
        deeplink: "stillpoets://shelf"
    )
}

struct WidgetTimelinePayload: Codable {
    var generatedAt: Int64?
    var theme: WidgetThemePayload?
    var entries: [WidgetEntryPayload]?
}

/// 负责从主 App 与 WidgetKit 扩展的共享通道读取预编译的 widget_timeline.json：
/// 1. 若存在 App Group 共享容器（正式开发者账号），优先从共享目录读取快照与自定义字体；
/// 2. 否则从同 Team 共享的 Keychain Access Group 读取（完美适配企业通配符签名证书）。
enum WidgetStore {
    private static let keychainService = "app.stillpoets.widget"
    private static let timelineAccount = "widget_timeline.json"
    private static let offsetAccount = "manual_offset"
    private static let sharedGroupSuffix = "stillpoets.shared"

    private static let fontCacheLock = NSLock()
    private static var registeredFonts: [String: String] = [:]

    static func loadTimeline() -> WidgetTimelinePayload? {
        if let data = readTimelineDataFromAppGroup(),
           let decoded = try? JSONDecoder().decode(WidgetTimelinePayload.self, from: data) {
            return decoded
        }
        if let data = readKeychainData(account: timelineAccount, accessGroup: resolveKeychainAccessGroup()),
           let decoded = try? JSONDecoder().decode(WidgetTimelinePayload.self, from: data) {
            return decoded
        }
        return nil
    }

    static func readManualOffset() -> Int {
        let group = resolveKeychainAccessGroup()
        if let data = readKeychainData(account: offsetAccount, accessGroup: group),
           let str = String(data: data, encoding: .utf8),
           let value = Int(str.trimmingCharacters(in: .whitespacesAndNewlines)) {
            return max(0, value)
        }
        if let groupID = resolveAppGroupID(),
           let defaults = UserDefaults(suiteName: groupID) {
            return max(0, defaults.integer(forKey: offsetAccount))
        }
        return max(0, UserDefaults.standard.integer(forKey: offsetAccount))
    }

    @discardableResult
    static func incrementManualOffset() -> Int {
        let next = readManualOffset() + 1
        let group = resolveKeychainAccessGroup()
        writeKeychainData(Data(String(next).utf8), account: offsetAccount, accessGroup: group)
        if let groupID = resolveAppGroupID(),
           let defaults = UserDefaults(suiteName: groupID) {
            defaults.set(next, forKey: offsetAccount)
        }
        UserDefaults.standard.set(next, forKey: offsetAccount)
        return next
    }

    /// 当存在 App Group 且已同步自定义 .ttf/.otf 字体时，动态注册并返回 PostScript 字体名。
    static func resolveCustomFontName(fontFile: String?) -> String? {
        guard let file = fontFile?.trimmingCharacters(in: .whitespacesAndNewlines),
              !file.isEmpty,
              let containerURL = resolveAppGroupContainer() else {
            return nil
        }
        fontCacheLock.lock()
        if let cached = registeredFonts[file] {
            fontCacheLock.unlock()
            return cached
        }
        fontCacheLock.unlock()

        let fontURL = containerURL
            .appendingPathComponent("fonts", isDirectory: true)
            .appendingPathComponent(file)
        guard FileManager.default.fileExists(atPath: fontURL.path) else {
            return nil
        }

        _ = CTFontManagerRegisterFontsForURL(fontURL as CFURL, .process, nil)
        guard let provider = CGDataProvider(url: fontURL as CFURL),
              let cgFont = CGFont(provider),
              let psName = cgFont.postScriptName as String? else {
            return nil
        }

        fontCacheLock.lock()
        registeredFonts[file] = psName
        fontCacheLock.unlock()
        return psName
    }

    // MARK: - Private Helpers

    private static func resolveAppGroupID() -> String? {
        guard let configured = Bundle.main.object(forInfoDictionaryKey: "StillPoetsAppGroup") as? String else {
            return nil
        }
        let trimmed = configured.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty || trimmed.hasPrefix("$(") {
            return nil
        }
        return trimmed
    }

    private static func resolveAppGroupContainer() -> URL? {
        guard let groupID = resolveAppGroupID() else { return nil }
        return FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: groupID)
    }

    private static func readTimelineDataFromAppGroup() -> Data? {
        guard let containerURL = resolveAppGroupContainer() else { return nil }
        let fileURL = containerURL.appendingPathComponent("widget_timeline.json")
        guard let data = try? Data(contentsOf: fileURL), !data.isEmpty else {
            return nil
        }
        return data
    }

    private static func resolveKeychainAccessGroup() -> String? {
        if let configured = Bundle.main.object(forInfoDictionaryKey: "StillPoetsKeychainGroup") as? String {
            let trimmed = configured.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmed.isEmpty, !trimmed.hasPrefix("$(") {
                return trimmed
            }
        }
        if let prefix = detectAppIdentifierPrefix() {
            return prefix + sharedGroupSuffix
        }
        return nil
    }

    private static func detectAppIdentifierPrefix() -> String? {
        let probeAccount = "stillpoets_prefix_probe"
        let baseQuery: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: keychainService,
            kSecAttrAccount as String: probeAccount,
        ]
        var query = baseQuery
        query[kSecReturnAttributes as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne

        var result: AnyObject?
        var status = SecItemCopyMatching(query as CFDictionary, &result)
        if status == errSecItemNotFound {
            var addQuery = baseQuery
            addQuery[kSecValueData as String] = Data("1".utf8)
            addQuery[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
            _ = SecItemAdd(addQuery as CFDictionary, nil)
            status = SecItemCopyMatching(query as CFDictionary, &result)
        }
        guard status == errSecSuccess,
              let attrs = result as? [String: Any],
              let group = attrs[kSecAttrAccessGroup as String] as? String,
              let dotIdx = group.firstIndex(of: ".") else {
            return nil
        }
        return String(group[...dotIdx])
    }

    private static func readKeychainData(account: String, accessGroup: String?) -> Data? {
        var query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: keychainService,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        if let group = accessGroup, !group.isEmpty {
            query[kSecAttrAccessGroup as String] = group
        }
        var result: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        if status == errSecSuccess, let data = result as? Data, !data.isEmpty {
            return data
        }
        if status == errSecMissingEntitlement, accessGroup != nil {
            return readKeychainData(account: account, accessGroup: nil)
        }
        return nil
    }

    private static func writeKeychainData(_ data: Data, account: String, accessGroup: String?) {
        var query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: keychainService,
            kSecAttrAccount as String: account,
        ]
        if let group = accessGroup, !group.isEmpty {
            query[kSecAttrAccessGroup as String] = group
        }
        let updateAttrs: [String: Any] = [
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlock,
        ]
        let status = SecItemUpdate(query as CFDictionary, updateAttrs as CFDictionary)
        if status == errSecItemNotFound {
            var addQuery = query
            addQuery[kSecValueData as String] = data
            addQuery[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
            let addStatus = SecItemAdd(addQuery as CFDictionary, nil)
            if addStatus == errSecMissingEntitlement, accessGroup != nil {
                writeKeychainData(data, account: account, accessGroup: nil)
            }
        } else if status == errSecMissingEntitlement, accessGroup != nil {
            writeKeychainData(data, account: account, accessGroup: nil)
        }
    }
}

extension Color {
    init(hex: String?, fallback: Color) {
        guard let raw = hex?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty else {
            self = fallback
            return
        }
        let cleaned = raw.hasPrefix("#") ? String(raw.dropFirst()) : raw
        var int: UInt64 = 0
        guard Scanner(string: cleaned).scanHexInt64(&int) else {
            self = fallback
            return
        }
        let a, r, g, b: UInt64
        switch cleaned.count {
        case 6:
            (a, r, g, b) = (255, (int >> 16) & 0xFF, (int >> 8) & 0xFF, int & 0xFF)
        case 8:
            (a, r, g, b) = ((int >> 24) & 0xFF, (int >> 16) & 0xFF, (int >> 8) & 0xFF, int & 0xFF)
        default:
            self = fallback
            return
        }
        self = Color(
            .sRGB,
            red: Double(r) / 255.0,
            green: Double(g) / 255.0,
            blue: Double(b) / 255.0,
            opacity: Double(a) / 255.0
        )
    }
}
