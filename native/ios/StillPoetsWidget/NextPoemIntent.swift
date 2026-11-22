import AppIntents
import WidgetKit

/// iOS 17+ 桌面交互意图：点击小组件右上角图标即刻切换下一首候选诗句，无需拉起主 App。
@available(iOS 17.0, *)
struct NextPoemIntent: AppIntent {
    static let title: LocalizedStringResource = "换一首诗"
    static let description = IntentDescription("在 StillPoets 诗笺小组件中切换下一首候选诗句")

    func perform() async throws -> some IntentResult {
        WidgetStore.incrementManualOffset()
        return .result()
    }
}
