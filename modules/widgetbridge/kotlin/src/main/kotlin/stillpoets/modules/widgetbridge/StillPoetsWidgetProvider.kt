package stillpoets.modules.widgetbridge

import android.app.AlarmManager
import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.res.Configuration
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.RectF
import android.graphics.Typeface
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.text.Layout
import android.text.StaticLayout
import android.text.TextPaint
import android.text.TextUtils
import android.util.SizeF
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.widget.RemoteViews
import java.io.File
import java.util.Calendar
import java.util.TimeZone
import org.json.JSONObject
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt

/**
 * StillPoets Android 原生桌面小组件：
 * - 直接消费 Go 引擎预编译的 widget_timeline.json（零 Go 运行时常驻开销）；
 * - 完整支持 3 套内置主题配色、用户自定义 .ttf/.otf 字体渲染与自由拖拽缩放；
 * - 点击主体经 stillpoets:// Deeplink 直达整首诗并高亮原句，点击右上角图标在桌面即刻换一首。
 */
class StillPoetsWidgetProvider : AppWidgetProvider() {

    override fun onUpdate(context: Context, appWidgetManager: AppWidgetManager, appWidgetIds: IntArray) {
        for (appWidgetId in appWidgetIds) {
            updateSingleWidget(context, appWidgetManager, appWidgetId)
        }
        scheduleNextRefresh(context)
    }

    override fun onDisabled(context: Context) {
        super.onDisabled(context)
        cancelScheduledRefresh(context)
    }

    override fun onAppWidgetOptionsChanged(
        context: Context,
        appWidgetManager: AppWidgetManager,
        appWidgetId: Int,
        newOptions: Bundle,
    ) {
        super.onAppWidgetOptionsChanged(context, appWidgetManager, appWidgetId, newOptions)
        updateSingleWidget(context, appWidgetManager, appWidgetId)
    }

    override fun onDeleted(context: Context, appWidgetIds: IntArray) {
        super.onDeleted(context, appWidgetIds)
        val editor = prefs(context).edit()
        for (id in appWidgetIds) {
            editor.remove(KEY_OFFSET_PREFIX + id)
        }
        editor.apply()
    }

    override fun onReceive(context: Context, intent: Intent) {
        super.onReceive(context, intent)
        when (intent.action) {
            ACTION_NEXT_ENTRY -> {
                val manager = AppWidgetManager.getInstance(context) ?: return
                val appWidgetId = intent.getIntExtra(AppWidgetManager.EXTRA_APPWIDGET_ID, AppWidgetManager.INVALID_APPWIDGET_ID)
                if (appWidgetId != AppWidgetManager.INVALID_APPWIDGET_ID) {
                    val p = prefs(context)
                    val next = p.getInt(KEY_OFFSET_PREFIX + appWidgetId, 0) + 1
                    p.edit().putInt(KEY_OFFSET_PREFIX + appWidgetId, next).apply()
                    updateSingleWidget(context, manager, appWidgetId)
                } else {
                    updateAllWidgets(context)
                }
            }
            ACTION_TIMED_REFRESH -> {
                updateAllWidgets(context)
            }
        }
    }

    companion object {
        const val ACTION_NEXT_ENTRY = "stillpoets.modules.widgetbridge.ACTION_NEXT_ENTRY"
        const val ACTION_TIMED_REFRESH = "stillpoets.modules.widgetbridge.ACTION_TIMED_REFRESH"

        private const val REQUEST_CODE_TIMED_REFRESH = 9001
        private const val DEFAULT_INTERVAL_MINUTES = 60
        private const val MIN_INTERVAL_MINUTES = 15
        private const val MAX_INTERVAL_MINUTES = 1440

        private const val PREFS_NAME = "stillpoets_widget_prefs"
        private const val KEY_TIMELINE_PATH = "timeline_path"
        private const val KEY_FONTS_DIR = "fonts_dir"
        private const val KEY_LAST_POOL_REV = "last_pool_revision"
        private const val KEY_OFFSET_PREFIX = "manual_offset_"

        private fun prefs(context: Context) =
            context.applicationContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

        fun savePathsAndRefresh(context: Context, timelinePath: String, fontsDir: String) {
            val appCtx = context.applicationContext
            val p = prefs(appCtx)
            val editor = p.edit()
            if (timelinePath.isNotBlank()) {
                editor.putString(KEY_TIMELINE_PATH, timelinePath)
            }
            if (fontsDir.isNotBlank()) {
                editor.putString(KEY_FONTS_DIR, fontsDir)
            }
            // 仅当诗池本身变更（切换抽选模式/诗集或显式点「刷新小组件」）时才归零手动偏移；
            // 普通同步、冷启动或改主题/字号/间隔不打断当前槽位展示的诗句。
            val snap = loadTimeline(appCtx, timelinePath.ifBlank { null })
            val poolRev = snap?.effectivePoolRevision().orEmpty()
            if (poolRev.isNotEmpty() && poolRev != p.getString(KEY_LAST_POOL_REV, null)) {
                editor.putString(KEY_LAST_POOL_REV, poolRev)
                val manager = AppWidgetManager.getInstance(appCtx)
                val ids = manager?.getAppWidgetIds(ComponentName(appCtx, StillPoetsWidgetProvider::class.java)) ?: IntArray(0)
                for (id in ids) {
                    editor.putInt(KEY_OFFSET_PREFIX + id, 0)
                }
            }
            editor.apply()
            updateAllWidgets(appCtx)
        }

        fun updateAllWidgets(context: Context) {
            val appCtx = context.applicationContext
            val manager = AppWidgetManager.getInstance(appCtx) ?: return
            val ids = manager.getAppWidgetIds(ComponentName(appCtx, StillPoetsWidgetProvider::class.java))
            if (ids.isEmpty()) {
                cancelScheduledRefresh(appCtx)
                return
            }
            for (id in ids) {
                updateSingleWidget(appCtx, manager, id)
            }
            scheduleNextRefresh(appCtx)
        }

        private fun updateSingleWidget(context: Context, manager: AppWidgetManager, appWidgetId: Int) {
            val views = RemoteViews(context.packageName, R.layout.stillpoets_widget)
            val timeline = loadTimeline(context, null)
            val theme = timeline?.theme ?: WidgetTheme.defaultArtbook()

            val options = manager.getAppWidgetOptions(appWidgetId)
            val (widthDp, heightDp) = resolveWidgetDimensionsDp(context, options)
            val isLargeCard = heightDp >= 200

            val useDark = theme.followSystemDark && isSystemDarkMode(context)
            val bgColor = if (useDark) {
                parseColorOr(theme.darkBgColor, Color.parseColor("#151517"))
            } else {
                parseColorOr(theme.bgColor, Color.parseColor("#F4F4EF"))
            }
            val textColor = if (useDark) {
                parseColorOr(theme.darkTextColor, Color.parseColor("#ECE9E2"))
            } else {
                parseColorOr(theme.textColor, Color.parseColor("#1F2320"))
            }
            val accentColor = if (useDark) {
                parseColorOr(theme.darkAccentColor, Color.parseColor("#D0735E"))
            } else {
                parseColorOr(theme.accentColor, Color.parseColor("#3A6B5C"))
            }
            val secondaryColor = withAlpha(textColor, 0.58f)
            val borderColor = if (isDarkColor(bgColor)) {
                Color.argb(38, 255, 255, 255)
            } else {
                Color.argb(28, 31, 35, 32)
            }

            // 1. 绘制贴合当前尺寸与主题配色的圆角纸卡底板
            val bgBmp = renderCardBackground(context, widthDp, heightDp, bgColor, borderColor)
            views.setImageViewBitmap(R.id.widget_bg_image, bgBmp)

            // 2. 着色顶栏强调圆点与「换一首」矢量图标
            views.setInt(R.id.widget_dot, "setColorFilter", accentColor)
            views.setInt(R.id.widget_next_btn, "setColorFilter", withAlpha(textColor, 0.48f))
            views.setTextColor(R.id.widget_tag, secondaryColor)
            views.setTextColor(R.id.widget_meta_text, secondaryColor)
            views.setTextColor(R.id.widget_hint_text, accentColor)

            // 3. 按用户配置的切换间隔槽位（对齐本地时区） + 实例手动偏移挑出展示条目
            val entries = timeline?.entries.orEmpty()
            val intervalMinutes = normalizeIntervalMinutes(timeline?.refreshIntervalMinutes ?: DEFAULT_INTERVAL_MINUTES)
            val entry = if (entries.isNotEmpty()) {
                val slot = currentSlot(System.currentTimeMillis(), intervalMinutes)
                val manualOffset = prefs(context).getInt(KEY_OFFSET_PREFIX + appWidgetId, 0)
                val idx = ((slot + manualOffset) % entries.size + entries.size) % entries.size
                entries[idx]
            } else {
                null
            }

            val poemText: String
            val poemTitleText: String
            val metaText: String
            val tagText: String
            val hintText: String
            val deeplink: String

            if (entry != null) {
                val rawBody = if (isLargeCard && entry.largeText.isNotBlank()) {
                    entry.largeText.trim()
                } else {
                    entry.displayText.trim()
                }
                poemText = rawBody
                poemTitleText = if (isLargeCard) entry.poemTitle.trim() else ""
                metaText = formatMeta(entry, omitPoemTitle = poemTitleText.isNotEmpty())
                tagText = if (entry.entryType == "quote") "散页" else "诗笺"
                hintText = if (entry.deeplink.startsWith("stillpoets://quote/")) "看散页 ›" else "读全诗 ›"
                deeplink = entry.deeplink
                views.setViewVisibility(R.id.widget_next_btn, if (entries.size > 1) View.VISIBLE else View.GONE)
            } else {
                poemText = if (isLargeCard) {
                    context.getString(R.string.stillpoets_widget_empty_poem_large)
                } else {
                    context.getString(R.string.stillpoets_widget_empty_poem)
                }
                poemTitleText = ""
                metaText = context.getString(R.string.stillpoets_widget_empty_meta)
                tagText = "StillPoets"
                hintText = "打开 ›"
                deeplink = ""
                views.setViewVisibility(R.id.widget_next_btn, View.GONE)
            }

            views.setTextViewText(R.id.widget_tag, tagText)
            views.setTextViewText(R.id.widget_meta_text, metaText)
            views.setTextViewText(R.id.widget_hint_text, hintText)

            if (poemTitleText.isNotEmpty()) {
                views.setViewVisibility(R.id.widget_poem_title, View.VISIBLE)
                views.setTextViewText(R.id.widget_poem_title, poemTitleText)
                views.setTextColor(R.id.widget_poem_title, textColor)
                val titleSizeSp = if (heightDp >= 280 && widthDp >= 280) 18f else 16.5f
                views.setTextViewTextSize(R.id.widget_poem_title, TypedValue.COMPLEX_UNIT_SP, titleSizeSp)
            } else {
                views.setViewVisibility(R.id.widget_poem_title, View.GONE)
            }

            // 4. 自适应字号、行数与对齐方式：
            //    - 4×4 大号卡片按真实可用高度动态计算最大行数，多诗节顶部舒展排布，不再缩成中间小块；
            //    - 仅 1~3 行短金句在大号卡片上自动放大字号，避免四周空洞。
            val lineCount = poemText.split('\n').size
            val textSizeSp = when {
                isLargeCard && lineCount <= 3 && poemText.length <= 72 -> 19.5f
                isLargeCard && heightDp >= 290 && widthDp >= 270 -> 16.5f
                isLargeCard -> 15.5f
                heightDp >= 160 -> 15.5f
                widthDp <= 210 -> 13.5f
                else -> 15f
            }
            val titleOverheadDp = if (poemTitleText.isNotEmpty()) 34 else 0
            val chromeOverheadDp = 64 + titleOverheadDp
            val availBodyHeightDp = max(heightDp - chromeOverheadDp, 48)
            val lineMultiplier = if (isLargeCard) 1.42f else 1.38f
            val estLineHeightDp = textSizeSp * lineMultiplier
            val maxLines = when {
                isLargeCard -> (availBodyHeightDp / estLineHeightDp).toInt().coerceIn(5, 18)
                heightDp >= 165 -> (availBodyHeightDp / estLineHeightDp).toInt().coerceIn(4, 7)
                heightDp <= 110 -> 3
                else -> 4
            }
            // 大号卡片中多行诗文（≥4 行）从顶部自然展开，短金句或小卡片保持垂直居中
            val alignTop = isLargeCard && lineCount >= 4

            // 5. 若配置了本地自定义 .ttf/.otf 字体，或设置页选择了非默认字重，使用高清 StaticLayout 渲染以呈现真实字形与字重
            val customTypeface = resolveWidgetTypeface(context, theme.fontFile, theme.fontWeight)
            val renderedPoemBmp = if (customTypeface != null) {
                renderCustomFontText(
                    context = context,
                    text = poemText,
                    typeface = customTypeface,
                    textColor = textColor,
                    textSizeSp = textSizeSp,
                    lineMultiplier = lineMultiplier,
                    maxLines = maxLines,
                    alignTop = alignTop,
                    contentWidthDp = max(widthDp - 36, 120),
                    contentHeightDp = availBodyHeightDp,
                )
            } else {
                null
            }

            if (renderedPoemBmp != null) {
                views.setViewVisibility(R.id.widget_poem_text, View.GONE)
                views.setViewVisibility(R.id.widget_poem_image, View.VISIBLE)
                views.setImageViewBitmap(R.id.widget_poem_image, renderedPoemBmp)
            } else {
                views.setViewVisibility(R.id.widget_poem_image, View.GONE)
                views.setViewVisibility(R.id.widget_poem_text, View.VISIBLE)
                views.setTextViewText(R.id.widget_poem_text, poemText)
                views.setTextColor(R.id.widget_poem_text, textColor)
                views.setTextViewTextSize(R.id.widget_poem_text, TypedValue.COMPLEX_UNIT_SP, textSizeSp)
                views.setInt(R.id.widget_poem_text, "setMaxLines", maxLines)
                val gravity = if (alignTop) {
                    Gravity.TOP or Gravity.START
                } else {
                    Gravity.CENTER_VERTICAL or Gravity.START
                }
                views.setInt(R.id.widget_poem_text, "setGravity", gravity)
            }

            // 6. 绑定主体点击（stillpoets:// Deeplink 直达原诗/散页）
            val openPending = buildOpenAppPendingIntent(context, appWidgetId, deeplink)
            views.setOnClickPendingIntent(R.id.widget_body, openPending)

            // 7. 绑定右上角「换一首」广播点击（零延迟切换下一条，不拉起主 App）
            val nextIntent = Intent(context, StillPoetsWidgetProvider::class.java).apply {
                action = ACTION_NEXT_ENTRY
                putExtra(AppWidgetManager.EXTRA_APPWIDGET_ID, appWidgetId)
            }
            val nextPending = PendingIntent.getBroadcast(
                context,
                appWidgetId * 10 + 2,
                nextIntent,
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
            )
            views.setOnClickPendingIntent(R.id.widget_next_btn, nextPending)

            manager.updateAppWidget(appWidgetId, views)
        }

        private fun buildOpenAppPendingIntent(context: Context, appWidgetId: Int, deeplink: String): PendingIntent {
            val intent = if (deeplink.isNotBlank()) {
                Intent(Intent.ACTION_VIEW, Uri.parse(deeplink)).apply {
                    setPackage(context.packageName)
                    addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
                }
            } else {
                (context.packageManager.getLaunchIntentForPackage(context.packageName)
                    ?: Intent(Intent.ACTION_MAIN).apply { setPackage(context.packageName) }).apply {
                    addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
                }
            }
            return PendingIntent.getActivity(
                context,
                appWidgetId * 10 + 1,
                intent,
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
            )
        }

        private fun resolveWidgetDimensionsDp(context: Context, options: Bundle): Pair<Int, Int> {
            val isPortrait = context.resources.configuration.orientation == Configuration.ORIENTATION_PORTRAIT
            val minW = options.getInt(AppWidgetManager.OPTION_APPWIDGET_MIN_WIDTH, 0)
            val maxW = options.getInt(AppWidgetManager.OPTION_APPWIDGET_MAX_WIDTH, 0)
            val minH = options.getInt(AppWidgetManager.OPTION_APPWIDGET_MIN_HEIGHT, 0)
            val maxH = options.getInt(AppWidgetManager.OPTION_APPWIDGET_MAX_HEIGHT, 0)

            var widthDp = if (isPortrait) {
                if (minW > 0) minW else maxW
            } else {
                if (maxW > 0) maxW else minW
            }
            var heightDp = if (isPortrait) {
                if (maxH > 0) maxH else minH
            } else {
                if (minH > 0) minH else maxH
            }

            // Android 12+ 桌面可直接提供真实布局尺寸集合，用于纠正个别定制 ROM 的 min/max 偏差
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                @Suppress("DEPRECATION")
                val sizes = options.getParcelableArrayList<SizeF>(AppWidgetManager.OPTION_APPWIDGET_SIZES)
                if (!sizes.isNullOrEmpty()) {
                    val chosen = if (isPortrait) {
                        sizes.maxByOrNull { it.height }
                    } else {
                        sizes.maxByOrNull { it.width }
                    }
                    if (chosen != null && chosen.width > 0f && chosen.height > 0f) {
                        widthDp = max(widthDp, chosen.width.roundToInt())
                        heightDp = max(heightDp, chosen.height.roundToInt())
                    }
                }
            }

            if (widthDp <= 0) widthDp = 280
            if (heightDp <= 0) heightDp = 128
            return Pair(max(widthDp, 160), max(heightDp, 96))
        }

        private fun formatMeta(entry: WidgetEntry, omitPoemTitle: Boolean = false): String {
            val parts = mutableListOf<String>()
            val title = entry.poemTitle.trim()
            val author = entry.author.trim()
            val source = entry.sourceTitle.trim()

            if (!omitPoemTitle && title.isNotEmpty()) {
                parts.add("《$title》")
            } else if (source.isNotEmpty()) {
                parts.add("《$source》")
            }
            if (author.isNotEmpty()) {
                parts.add(author)
            }
            if (parts.isEmpty() && title.isNotEmpty()) {
                parts.add("《$title》")
            }
            return if (parts.isEmpty()) "StillPoets" else parts.joinToString(" · ")
        }

        private fun loadTimeline(context: Context, explicitPath: String?): WidgetTimeline? {
            val file = resolveTimelineFile(context, explicitPath) ?: return null
            return try {
                val raw = file.readText(Charsets.UTF_8)
                val root = JSONObject(raw)
                val themeObj = root.optJSONObject("theme")
                val theme = WidgetTheme(
                    preset = themeObj?.optString("preset").orEmpty(),
                    followSystemDark = themeObj?.optBoolean("followSystemDark", false) ?: false,
                    bgColor = themeObj?.optString("bgColor").orEmpty(),
                    textColor = themeObj?.optString("textColor").orEmpty(),
                    accentColor = themeObj?.optString("accentColor").orEmpty(),
                    darkBgColor = themeObj?.optString("darkBgColor").orEmpty(),
                    darkTextColor = themeObj?.optString("darkTextColor").orEmpty(),
                    darkAccentColor = themeObj?.optString("darkAccentColor").orEmpty(),
                    fontFamily = themeObj?.optString("fontFamily").orEmpty(),
                    fontFile = themeObj?.optString("fontFile").orEmpty(),
                    fontWeight = themeObj?.optInt("fontWeight", 400) ?: 400,
                )
                val arr = root.optJSONArray("entries")
                val entries = mutableListOf<WidgetEntry>()
                if (arr != null) {
                    for (i in 0 until arr.length()) {
                        val o = arr.optJSONObject(i) ?: continue
                        val text = o.optString("displayText").trim()
                        if (text.isEmpty()) continue
                        entries.add(
                            WidgetEntry(
                                entryType = o.optString("entryType", "poem"),
                                displayText = text,
                                largeText = o.optString("largeText", "").trim(),
                                poemTitle = o.optString("poemTitle", ""),
                                author = o.optString("author", ""),
                                sourceTitle = o.optString("sourceTitle", ""),
                                deeplink = o.optString("deeplink", ""),
                            )
                        )
                    }
                }
                WidgetTimeline(
                    generatedAt = root.optLong("generatedAt", 0L),
                    poolRevision = root.optString("poolRevision", "").trim(),
                    refreshIntervalMinutes = normalizeIntervalMinutes(root.optInt("refreshIntervalMinutes", DEFAULT_INTERVAL_MINUTES)),
                    theme = theme,
                    entries = entries,
                )
            } catch (_: Throwable) {
                null
            }
        }

        private fun normalizeIntervalMinutes(raw: Int): Int {
            if (raw <= 0) return DEFAULT_INTERVAL_MINUTES
            return raw.coerceIn(MIN_INTERVAL_MINUTES, MAX_INTERVAL_MINUTES)
        }

        private fun currentSlot(nowMs: Long, intervalMinutes: Int): Int {
            val clamped = normalizeIntervalMinutes(intervalMinutes)
            val tzOffsetMs = TimeZone.getDefault().getOffset(nowMs).toLong()
            val localSec = (nowMs + tzOffsetMs).toDouble() / 1000.0
            return floor(localSec / (clamped * 60.0)).toInt()
        }

        private fun nextBoundaryMillis(nowMs: Long, intervalMinutes: Int): Long {
            val clamped = normalizeIntervalMinutes(intervalMinutes)
            val cal = Calendar.getInstance().apply {
                timeInMillis = nowMs
                set(Calendar.SECOND, 0)
                set(Calendar.MILLISECOND, 0)
            }
            if (clamped >= MAX_INTERVAL_MINUTES) {
                cal.set(Calendar.HOUR_OF_DAY, 0)
                cal.set(Calendar.MINUTE, 0)
                cal.add(Calendar.DAY_OF_YEAR, 1)
            } else {
                val minutesSinceMidnight = cal.get(Calendar.HOUR_OF_DAY) * 60 + cal.get(Calendar.MINUTE)
                val nextMinutes = ((minutesSinceMidnight / clamped) + 1) * clamped
                cal.set(Calendar.HOUR_OF_DAY, 0)
                cal.set(Calendar.MINUTE, 0)
                cal.add(Calendar.MINUTE, nextMinutes)
            }
            val target = cal.timeInMillis
            return if (target <= nowMs + 5_000L) nowMs + clamped.toLong() * 60_000L else target
        }

        private fun timedRefreshPendingIntent(context: Context): PendingIntent {
            val intent = Intent(context, StillPoetsWidgetProvider::class.java).apply {
                action = ACTION_TIMED_REFRESH
            }
            return PendingIntent.getBroadcast(
                context,
                REQUEST_CODE_TIMED_REFRESH,
                intent,
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
            )
        }

        private fun scheduleNextRefresh(context: Context) {
            try {
                val appCtx = context.applicationContext
                val manager = AppWidgetManager.getInstance(appCtx) ?: return
                val ids = manager.getAppWidgetIds(ComponentName(appCtx, StillPoetsWidgetProvider::class.java))
                if (ids.isEmpty()) {
                    cancelScheduledRefresh(appCtx)
                    return
                }
                val timeline = loadTimeline(appCtx, null)
                val intervalMinutes = normalizeIntervalMinutes(timeline?.refreshIntervalMinutes ?: DEFAULT_INTERVAL_MINUTES)
                val alarmManager = appCtx.getSystemService(Context.ALARM_SERVICE) as? AlarmManager ?: return
                val triggerAt = nextBoundaryMillis(System.currentTimeMillis(), intervalMinutes)
                alarmManager.set(AlarmManager.RTC, triggerAt, timedRefreshPendingIntent(appCtx))
            } catch (_: Throwable) {
                // 忽略个别定制 ROM 的后台闹钟限制，由 appwidget-provider updatePeriodMillis 兜底刷新
            }
        }

        private fun cancelScheduledRefresh(context: Context) {
            try {
                val alarmManager = context.applicationContext.getSystemService(Context.ALARM_SERVICE) as? AlarmManager ?: return
                alarmManager.cancel(timedRefreshPendingIntent(context.applicationContext))
            } catch (_: Throwable) {
            }
        }

        private fun resolveTimelineFile(context: Context, explicitPath: String?): File? {
            val candidates = linkedSetOf<String>()
            if (!explicitPath.isNullOrBlank()) candidates.add(explicitPath)
            prefs(context).getString(KEY_TIMELINE_PATH, null)?.takeIf { it.isNotBlank() }?.let { candidates.add(it) }

            val filesDir = context.filesDir
            candidates.add(File(filesDir, "StillPoets/widget_timeline.json").absolutePath)
            candidates.add(File(filesDir, "StillPoets/StillPoets/widget_timeline.json").absolutePath)
            candidates.add(File(filesDir, "stillpoets/widget_timeline.json").absolutePath)
            candidates.add(File(filesDir, "widget_timeline.json").absolutePath)

            for (path in candidates) {
                val f = File(path)
                if (f.isFile && f.length() > 0L) return f
            }
            return null
        }

        private fun resolveWidgetTypeface(context: Context, fontFileName: String, fontWeight: Int): Typeface? {
            val base = resolveCustomTypeface(context, fontFileName)
            val normalizedWeight = if (fontWeight in 100..900) fontWeight else 400
            if (base == null) {
                if (normalizedWeight == 400 || Build.VERSION.SDK_INT < Build.VERSION_CODES.P) {
                    return null
                }
                return try {
                    Typeface.create(Typeface.SERIF, normalizedWeight, false)
                } catch (_: Throwable) {
                    null
                }
            }
            if (normalizedWeight != 400 && Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
                return try {
                    Typeface.create(base, normalizedWeight, false)
                } catch (_: Throwable) {
                    base
                }
            }
            return base
        }

        private fun resolveCustomTypeface(context: Context, fontFileName: String): Typeface? {
            val clean = fontFileName.trim()
            if (clean.isEmpty()) return null
            val dirs = linkedSetOf<File>()
            prefs(context).getString(KEY_FONTS_DIR, null)?.takeIf { it.isNotBlank() }?.let { dirs.add(File(it)) }
            resolveTimelineFile(context, null)?.parentFile?.let { dirs.add(File(it, "fonts")) }
            dirs.add(File(context.filesDir, "StillPoets/fonts"))

            for (dir in dirs) {
                val f = File(dir, clean)
                if (f.isFile && f.length() > 0L) {
                    return try {
                        Typeface.createFromFile(f)
                    } catch (_: Throwable) {
                        null
                    }
                }
            }
            return null
        }

        private fun renderCardBackground(
            context: Context,
            widthDp: Int,
            heightDp: Int,
            bgColor: Int,
            borderColor: Int,
        ): Bitmap {
            val density = min(context.resources.displayMetrics.density, 2.0f)
            val w = max(180, min(960, (widthDp * density).roundToInt()))
            val h = max(100, min(960, (heightDp * density).roundToInt()))
            val bmp = Bitmap.createBitmap(w, h, Bitmap.Config.ARGB_8888)
            val canvas = Canvas(bmp)
            val radius = 20f * density
            val stroke = max(1f, 1f * density)
            val half = stroke / 2f
            val rect = RectF(half, half, w - half, h - half)

            val fillPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
                style = Paint.Style.FILL
                color = bgColor
            }
            canvas.drawRoundRect(rect, radius, radius, fillPaint)

            val borderPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
                style = Paint.Style.STROKE
                strokeWidth = stroke
                color = borderColor
            }
            canvas.drawRoundRect(rect, radius, radius, borderPaint)
            return bmp
        }

        private fun renderCustomFontText(
            context: Context,
            text: String,
            typeface: Typeface,
            textColor: Int,
            textSizeSp: Float,
            lineMultiplier: Float,
            maxLines: Int,
            alignTop: Boolean,
            contentWidthDp: Int,
            contentHeightDp: Int,
        ): Bitmap? {
            return try {
                val dm = context.resources.displayMetrics
                var scale = min(dm.density, 2.25f)
                val estPixels = (contentWidthDp * scale) * (contentHeightDp * scale)
                if (estPixels > 1_100_000f) {
                    scale *= kotlin.math.sqrt(1_100_000f / estPixels)
                }
                val maxW = max(160, min(1080, (contentWidthDp * scale).roundToInt()))
                val maxH = max(64, min(1100, (contentHeightDp * scale).roundToInt()))

                val paint = TextPaint(Paint.ANTI_ALIAS_FLAG or Paint.SUBPIXEL_TEXT_FLAG).apply {
                    color = textColor
                    textSize = textSizeSp * scale
                    this.typeface = typeface
                }

                val layout = StaticLayout.Builder.obtain(text, 0, text.length, paint, maxW)
                    .setAlignment(Layout.Alignment.ALIGN_NORMAL)
                    .setLineSpacing(0f, lineMultiplier)
                    .setIncludePad(false)
                    .setMaxLines(maxLines)
                    .setEllipsize(TextUtils.TruncateAt.END)
                    .build()

                val bmpH = maxH
                val bmp = Bitmap.createBitmap(maxW, bmpH, Bitmap.Config.ARGB_8888)
                val canvas = Canvas(bmp)
                val dy = if (alignTop) {
                    0f
                } else {
                    max(0f, (bmpH - layout.height) / 2f)
                }
                canvas.save()
                canvas.translate(0f, dy)
                layout.draw(canvas)
                canvas.restore()
                bmp
            } catch (_: Throwable) {
                null
            }
        }

        private fun parseColorOr(hex: String, fallback: Int): Int {
            val s = hex.trim()
            if (s.isEmpty()) return fallback
            return try {
                Color.parseColor(if (s.startsWith("#")) s else "#$s")
            } catch (_: Throwable) {
                fallback
            }
        }

        private fun withAlpha(color: Int, alphaFraction: Float): Int {
            val a = (255f * alphaFraction).roundToInt().coerceIn(0, 255)
            return Color.argb(a, Color.red(color), Color.green(color), Color.blue(color))
        }

        private fun isSystemDarkMode(context: Context): Boolean {
            return (context.resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK) == Configuration.UI_MODE_NIGHT_YES
        }

        private fun isDarkColor(color: Int): Boolean {
            val lum = 0.299 * Color.red(color) + 0.587 * Color.green(color) + 0.114 * Color.blue(color)
            return lum < 128.0
        }
    }

    private data class WidgetTimeline(
        val generatedAt: Long,
        val poolRevision: String,
        val refreshIntervalMinutes: Int,
        val theme: WidgetTheme,
        val entries: List<WidgetEntry>,
    ) {
        fun effectivePoolRevision(): String =
            poolRevision.ifBlank { if (generatedAt > 0L) generatedAt.toString() else "" }
    }

    private data class WidgetTheme(
        val preset: String,
        val followSystemDark: Boolean,
        val bgColor: String,
        val textColor: String,
        val accentColor: String,
        val darkBgColor: String,
        val darkTextColor: String,
        val darkAccentColor: String,
        val fontFamily: String,
        val fontFile: String,
        val fontWeight: Int = 400,
    ) {
        companion object {
            fun defaultArtbook() = WidgetTheme(
                preset = "artbook",
                followSystemDark = true,
                bgColor = "#F4F4EF",
                textColor = "#1F2320",
                accentColor = "#3A6B5C",
                darkBgColor = "#151517",
                darkTextColor = "#ECE9E2",
                darkAccentColor = "#D0735E",
                fontFamily = "",
                fontFile = "",
                fontWeight = 400,
            )
        }
    }

    private data class WidgetEntry(
        val entryType: String,
        val displayText: String,
        val largeText: String = "",
        val poemTitle: String,
        val author: String,
        val sourceTitle: String,
        val deeplink: String,
    )
}
