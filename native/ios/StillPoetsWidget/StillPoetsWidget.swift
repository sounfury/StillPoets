import SwiftUI
import WidgetKit

struct StillPoetsWidgetEntry: TimelineEntry {
    let date: Date
    let item: WidgetEntryPayload
    let theme: WidgetThemePayload
    let customFontName: String?
    let hasMultipleEntries: Bool
}

struct StillPoetsTimelineProvider: TimelineProvider {
    func placeholder(in context: Context) -> StillPoetsWidgetEntry {
        StillPoetsWidgetEntry(
            date: Date(),
            item: .emptyPlaceholder,
            theme: .fallback,
            customFontName: nil,
            hasMultipleEntries: false
        )
    }

    func getSnapshot(in context: Context, completion: @escaping (StillPoetsWidgetEntry) -> Void) {
        completion(currentSingleEntry(at: Date()))
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<StillPoetsWidgetEntry>) -> Void) {
        let now = Date()
        guard let payload = WidgetStore.loadTimeline(),
              let items = payload.entries,
              !items.isEmpty else {
            let empty = StillPoetsWidgetEntry(
                date: now,
                item: .emptyPlaceholder,
                theme: WidgetStore.loadTimeline()?.theme ?? .fallback,
                customFontName: nil,
                hasMultipleEntries: false
            )
            let nextRefresh = Calendar.current.date(byAdding: .minute, value: 30, to: now) ?? now.addingTimeInterval(1800)
            completion(Timeline(entries: [empty], policy: .after(nextRefresh)))
            return
        }

        let theme = payload.theme ?? .fallback
        let fontName = WidgetStore.resolveCustomFontName(fontFile: theme.fontFile)
        let manualOffset = WidgetStore.readManualOffset()
        let intervalMinutes = payload.effectiveIntervalMinutes
        let baseSlot = slotIndex(at: now, intervalMinutes: intervalMinutes)
        let total = items.count
        let slots = total > 1 ? 24 : 1

        var timelineEntries: [StillPoetsWidgetEntry] = []
        timelineEntries.reserveCapacity(slots)

        for step in 0..<slots {
            let entryDate: Date
            if step == 0 {
                entryDate = now
            } else {
                entryDate = slotBoundaryDate(from: now, step: step, intervalMinutes: intervalMinutes)
            }
            let rawIdx = (baseSlot + manualOffset + step) % total
            let idx = rawIdx >= 0 ? rawIdx : rawIdx + total
            timelineEntries.append(
                StillPoetsWidgetEntry(
                    date: entryDate,
                    item: items[idx],
                    theme: theme,
                    customFontName: fontName,
                    hasMultipleEntries: total > 1
                )
            )
        }

        if total <= 1 {
            let nextRefresh = slotBoundaryDate(from: now, step: 1, intervalMinutes: intervalMinutes)
            completion(Timeline(entries: timelineEntries, policy: .after(nextRefresh)))
            return
        }

        completion(Timeline(entries: timelineEntries, policy: .atEnd))
    }

    private func currentSingleEntry(at date: Date) -> StillPoetsWidgetEntry {
        guard let payload = WidgetStore.loadTimeline(),
              let items = payload.entries,
              !items.isEmpty else {
            return StillPoetsWidgetEntry(
                date: date,
                item: .emptyPlaceholder,
                theme: WidgetStore.loadTimeline()?.theme ?? .fallback,
                customFontName: nil,
                hasMultipleEntries: false
            )
        }
        let theme = payload.theme ?? .fallback
        let fontName = WidgetStore.resolveCustomFontName(fontFile: theme.fontFile)
        let manualOffset = WidgetStore.readManualOffset()
        let slot = slotIndex(at: date, intervalMinutes: payload.effectiveIntervalMinutes)
        let rawIdx = (slot + manualOffset) % items.count
        let idx = rawIdx >= 0 ? rawIdx : rawIdx + items.count
        return StillPoetsWidgetEntry(
            date: date,
            item: items[idx],
            theme: theme,
            customFontName: fontName,
            hasMultipleEntries: items.count > 1
        )
    }

    private func slotIndex(at date: Date, intervalMinutes: Int) -> Int {
        let clamped = min(max(intervalMinutes, 15), 1440)
        let tzOffset = Double(TimeZone.current.secondsFromGMT(for: date))
        let localSeconds = date.timeIntervalSince1970 + tzOffset
        return Int(floor(localSeconds / Double(clamped * 60)))
    }

    private func slotBoundaryDate(from now: Date, step: Int, intervalMinutes: Int) -> Date {
        let clamped = min(max(intervalMinutes, 15), 1440)
        let cal = Calendar.current
        let dayStart = cal.startOfDay(for: now)
        if clamped >= 1440 {
            return cal.date(byAdding: .day, value: step, to: dayStart) ?? now.addingTimeInterval(Double(step) * 86400)
        }
        let minutesSinceMidnight = max(0, Int(now.timeIntervalSince(dayStart) / 60))
        let currentDaySlot = minutesSinceMidnight / clamped
        let targetMinutes = (currentDaySlot + step) * clamped
        return cal.date(byAdding: .minute, value: targetMinutes, to: dayStart)
            ?? now.addingTimeInterval(Double(step * clamped * 60))
    }
}

struct StillPoetsWidgetView: View {
    @Environment(\.widgetFamily) private var family
    let entry: StillPoetsWidgetEntry

    private var bgColor: Color {
        Color(hex: entry.theme.bgColor, fallback: Color(red: 0.969, green: 0.953, blue: 0.914))
    }

    private var textColor: Color {
        Color(hex: entry.theme.textColor, fallback: Color(red: 0.141, green: 0.122, blue: 0.102))
    }

    private var accentColor: Color {
        Color(hex: entry.theme.accentColor, fallback: Color(red: 0.702, green: 0.361, blue: 0.243))
    }

    private var mutedColor: Color {
        textColor.opacity(0.58)
    }

    private var dividerColor: Color {
        textColor.opacity(0.12)
    }

    private var poemFontSize: CGFloat {
        switch family {
        case .systemSmall:
            return 14
        case .systemLarge:
            return 17
        default:
            return 15.5
        }
    }

    private var poemLineLimit: Int {
        switch family {
        case .systemSmall:
            return 4
        case .systemLarge:
            return 10
        default:
            return 4
        }
    }

    private var poemFont: Font {
        if let customName = entry.customFontName, !customName.isEmpty {
            return .custom(customName, size: poemFontSize)
        }
        return .system(size: poemFontSize, weight: .regular, design: .serif)
    }

    private var deeplinkURL: URL? {
        let raw = (entry.item.deeplink ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        if !raw.isEmpty, let url = URL(string: raw) {
            return url
        }
        return URL(string: "stillpoets://shelf")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: family == .systemSmall ? 6 : 8) {
            // 顶部：诗笺/散页标签 + iOS 17 原地换一首按钮
            HStack(spacing: 6) {
                Circle()
                    .fill(accentColor)
                    .frame(width: 6, height: 6)

                Text(entry.item.tagLabel)
                    .font(.system(size: 11, weight: .medium))
                    .foregroundStyle(mutedColor)

                Spacer(minLength: 4)

                if entry.hasMultipleEntries {
                    Button(intent: NextPoemIntent()) {
                        Image(systemName: "arrow.triangle.2.circlepath")
                            .font(.system(size: 11, weight: .medium))
                            .foregroundStyle(mutedColor)
                            .frame(width: 24, height: 24)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                }
            }

            Spacer(minLength: 0)

            // 正文：诗句主体
            Text((entry.item.displayText ?? "").trimmingCharacters(in: .whitespacesAndNewlines))
                .font(poemFont)
                .foregroundStyle(textColor)
                .lineSpacing(family == .systemLarge ? 7 : 5)
                .lineLimit(poemLineLimit)
                .multilineTextAlignment(.leading)
                .frame(maxWidth: .infinity, alignment: .leading)

            Spacer(minLength: 0)

            // 底部分割线 + 出处与跳转提示
            Rectangle()
                .fill(dividerColor)
                .frame(height: 0.5)

            HStack(spacing: 8) {
                Text(entry.item.metaLine)
                    .font(.system(size: 11))
                    .foregroundStyle(mutedColor)
                    .lineLimit(1)
                    .truncationMode(.tail)

                if family != .systemSmall {
                    Spacer(minLength: 4)
                    Text(entry.item.actionHint)
                        .font(.system(size: 10.5, weight: .medium))
                        .foregroundStyle(accentColor)
                        .lineLimit(1)
                }
            }
        }
        .padding(.vertical, family == .systemSmall ? 2 : 4)
        .widgetURL(deeplinkURL)
        .containerBackground(for: .widget) {
            bgColor
        }
    }
}

struct StillPoetsWidget: Widget {
    let kind: String = "StillPoetsWidget"

    var body: some WidgetConfiguration {
        StaticConfiguration(kind: kind, provider: StillPoetsTimelineProvider()) { entry in
            StillPoetsWidgetView(entry: entry)
        }
        .configurationDisplayName("StillPoets 诗笺")
        .description("按设定间隔轮播你的诗集与散页；点击诗句直达原诗并高亮原行。")
        .supportedFamilies([.systemSmall, .systemMedium, .systemLarge])
    }
}

@main
struct StillPoetsWidgetBundle: WidgetBundle {
    var body: some Widget {
        StillPoetsWidget()
    }
}
