import Foundation
import Security
#if canImport(WidgetKit)
import WidgetKit
#endif

// StillPoets iOS 主 App 侧原生小组件桥接实现：
// 1. 优先尝试写入 App Group 共享容器（若当前描述文件开通了 App Groups，可同时共享自定义字体文件）；
// 2. 自动同步写入同 Team 共享的 Keychain Access Group（<AppIDPrefix>.stillpoets.shared），
//    确保在企业通配符证书（无 App Groups 权限）下主 App 与 WidgetKit 扩展依然 100% 互通；
// 3. 写入完成后立即调用 WidgetCenter.shared.reloadAllTimelines() 刷新桌面小组件。

func makeBridge() throws -> any Bridge { AppleWidgetBridge() }

private final class _CountBox: @unchecked Sendable {
    var value: Int = 0
}

private final class AppleWidgetBridge: Bridge {
    private static let keychainService = "app.stillpoets.widget"
    private static let timelineAccount = "widget_timeline.json"
    private static let offsetAccount = "manual_offset"
    private static let poolRevisionAccount = "last_pool_revision"
    private static let sharedGroupSuffix = "stillpoets.shared"

    func sync(timelinePath: String, fontsDir: String) throws {
        guard !timelinePath.isEmpty else { return }
        let timelineURL = URL(fileURLWithPath: timelinePath)
        guard let rawData = try? Data(contentsOf: timelineURL), !rawData.isEmpty else {
            return
        }

        let poolRevision = extractPoolRevision(from: rawData)
        let fontFile = extractFontFile(from: rawData)

        // 通道 1：若存在可用的 App Group 共享容器，则同步写入快照与当前自定义字体
        if let groupID = resolveAppGroupID(),
           let containerURL = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: groupID) {
            let dstTimeline = containerURL.appendingPathComponent("widget_timeline.json")
            try? rawData.write(to: dstTimeline, options: .atomic)

            if let fontFile = fontFile, !fontFile.isEmpty, !fontsDir.isEmpty {
                let srcFont = URL(fileURLWithPath: fontsDir).appendingPathComponent(fontFile)
                if FileManager.default.fileExists(atPath: srcFont.path) {
                    let dstFontsDir = containerURL.appendingPathComponent("fonts", isDirectory: true)
                    try? FileManager.default.createDirectory(at: dstFontsDir, withIntermediateDirectories: true)
                    let dstFont = dstFontsDir.appendingPathComponent(fontFile)
                    if !FileManager.default.fileExists(atPath: dstFont.path) {
                        try? FileManager.default.copyItem(at: srcFont, to: dstFont)
                    }
                }
            }

            if !poolRevision.isEmpty, let defaults = UserDefaults(suiteName: groupID) {
                let prev = defaults.string(forKey: Self.poolRevisionAccount)
                if prev != poolRevision {
                    defaults.set(poolRevision, forKey: Self.poolRevisionAccount)
                    defaults.set(0, forKey: Self.offsetAccount)
                }
            }
        }

        // 通道 2：同步写入共享 Keychain Access Group（企业通配符证书核心互通通道）
        let accessGroup = resolveKeychainAccessGroup()
        writeKeychain(data: rawData, account: Self.timelineAccount, accessGroup: accessGroup)

        // 仅当诗池标识变更（切换诗集/抽选模式或显式点击「刷新小组件」）时归零手动翻诗偏移；
        // 普通冷启动、WebDAV 同步、调整主题/字号/间隔均保留当前诗句与偏移。
        if !poolRevision.isEmpty {
            let prev = readKeychainString(account: Self.poolRevisionAccount, accessGroup: accessGroup)
            if prev != poolRevision {
                writeKeychain(data: Data(poolRevision.utf8), account: Self.poolRevisionAccount, accessGroup: accessGroup)
                writeKeychain(data: Data("0".utf8), account: Self.offsetAccount, accessGroup: accessGroup)
            }
        }

        #if canImport(WidgetKit)
        if #available(iOS 14.0, *) {
            WidgetCenter.shared.reloadAllTimelines()
        }
        #endif
    }

    func requestPin() throws -> Bool {
        // iOS 系统不开放程序化固定桌面小组件 API，返回 false 由 UI 提示用户长按主屏幕添加
        return false
    }

    func activeWidgetCount() throws -> Int {
        #if canImport(WidgetKit)
        if #available(iOS 14.0, *) {
            let sem = DispatchSemaphore(value: 0)
            let box = _CountBox()
            WidgetCenter.shared.getCurrentConfigurations { result in
                if case .success(let infos) = result {
                    box.value = infos.count
                }
                sem.signal()
            }
            _ = sem.wait(timeout: .now() + 1.0)
            return box.value
        }
        #endif
        return 0
    }

    // MARK: - 共享容器与 Keychain 组解析

    private func resolveAppGroupID() -> String? {
        if let configured = Bundle.main.object(forInfoDictionaryKey: "StillPoetsAppGroup") as? String {
            let trimmed = configured.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmed.isEmpty, !trimmed.hasPrefix("$(") {
                return trimmed
            }
        }
        return nil
    }

    private func resolveKeychainAccessGroup() -> String? {
        if let configured = Bundle.main.object(forInfoDictionaryKey: "StillPoetsKeychainGroup") as? String {
            let trimmed = configured.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmed.isEmpty, !trimmed.hasPrefix("$(") {
                return trimmed
            }
        }
        if let prefix = detectAppIdentifierPrefix() {
            return prefix + Self.sharedGroupSuffix
        }
        return nil
    }

    /// 通过一次无害的 Keychain 属性查询探测当前签名证书的真实 App ID 前缀（形如 "ABCDE12345."），
    /// 让更换任意企业通配符证书时无需硬编码 Team ID。
    private func detectAppIdentifierPrefix() -> String? {
        let probeAccount = "stillpoets_prefix_probe"
        let baseQuery: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: Self.keychainService,
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

    private func writeKeychain(data: Data, account: String, accessGroup: String?) {
        var query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: Self.keychainService,
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
                writeKeychain(data: data, account: account, accessGroup: nil)
            }
        } else if status == errSecMissingEntitlement, accessGroup != nil {
            writeKeychain(data: data, account: account, accessGroup: nil)
        }
    }

    private func readKeychainString(account: String, accessGroup: String?) -> String? {
        var query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: Self.keychainService,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        if let group = accessGroup, !group.isEmpty {
            query[kSecAttrAccessGroup as String] = group
        }
        var result: AnyObject?
        guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess,
              let data = result as? Data else {
            return nil
        }
        return String(data: data, encoding: .utf8)
    }

    private func extractPoolRevision(from data: Data) -> String {
        guard let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return ""
        }
        if let rev = obj["poolRevision"] as? String {
            let trimmed = rev.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmed.isEmpty {
                return trimmed
            }
        }
        if let n = obj["generatedAt"] as? NSNumber, n.int64Value > 0 {
            return String(n.int64Value)
        }
        return ""
    }

    private func extractFontFile(from data: Data) -> String? {
        guard let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let theme = obj["theme"] as? [String: Any] else {
            return nil
        }
        return theme["fontFile"] as? String
    }
}
