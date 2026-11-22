package stillpoets.modules.widgetbridge

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.RectF
import android.graphics.Typeface
import android.net.Uri
import android.os.Bundle
import android.text.Layout
import android.text.StaticLayout
import android.text.TextPaint
import android.text.TextUtils
import android.util.TypedValue
import android.view.View
import android.widget.RemoteViews
import java.io.File
import org.json.JSONObject
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
        if (intent.action == ACTION_NEXT_ENTRY) {
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
    }

    companion object {
        const val ACTION_NEXT_ENTRY = "stillpoets.modules.widgetbridge.ACTION_NEXT_ENTRY"

        private const val PREFS_NAME = "stillpoets_widget_prefs"
        private const val KEY_TIMELINE_PATH = "timeline_path"
        private const val KEY_FONTS_DIR = "fonts_dir"
        private const val KEY_LAST_GENERATED_AT = "last_generated_at"
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
            // 读取最新快照时间戳：若主 App 重新生成了新时间线（例如切换诗集/模式），归零手动偏移以立即呈现新池首条
            val snap = loadTimeline(appCtx, timelinePath.ifBlank { null })
            if (snap != null && snap.generatedAt > 0L && snap.generatedAt != p.getLong(KEY_LAST_GENERATED_AT, 0L)) {
                editor.putLong(KEY_LAST_GENERATED_AT, snap.generatedAt)
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
            if (ids.isEmpty()) return
            for (id in ids) {
                updateSingleWidget(appCtx, manager, id)
            }
        }

        private fun updateSingleWidget(context: Context, manager: AppWidgetManager, appWidgetId: Int) {
            val views = RemoteViews(context.packageName, R.layout.stillpoets_widget)
            val timeline = loadTimeline(context, null)
            val theme = timeline?.theme ?: WidgetTheme.defaultArtbook()

            val options = manager.getAppWidgetOptions(appWidgetId)
            val widthDp = max(options.getInt(AppWidgetManager.OPTION_APPWIDGET_MIN_WIDTH, 280), 180)
            val heightDp = max(options.getInt(AppWidgetManager.OPTION_APPWIDGET_MIN_HEIGHT, 128), 96)

            val bgColor = parseColorOr(theme.bgColor, Color.parseColor("#F4F4EF"))
            val textColor = parseColorOr(theme.textColor, Color.parseColor("#1F2320"))
            val accentColor = parseColorOr(theme.accentColor, Color.parseColor("#3A6B5C"))
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

            // 3. 按当前小时槽位 + 实例手动偏移挑出展示条目
            val entries = timeline?.entries.orEmpty()
            val entry = if (entries.isNotEmpty()) {
                val hourBucket = (System.currentTimeMillis() / 3_600_000L).toInt()
                val manualOffset = prefs(context).getInt(KEY_OFFSET_PREFIX + appWidgetId, 0)
                val idx = ((hourBucket + manualOffset) % entries.size + entries.size) % entries.size
                entries[idx]
            } else {
                null
            }

            val poemText: String
            val metaText: String
            val tagText: String
            val hintText: String
            val deeplink: String

            if (entry != null) {
                poemText = entry.displayText.trim()
                metaText = formatMeta(entry)
                tagText = if (entry.entryType == "quote") "散页" else "诗笺"
                hintText = if (entry.deeplink.startsWith("stillpoets://quote/")) "看散页 ›" else "读全诗 ›"
                deeplink = entry.deeplink
                views.setViewVisibility(R.id.widget_next_btn, if (entries.size > 1) View.VISIBLE else View.GONE)
            } else {
                poemText = context.getString(R.string.stillpoets_widget_empty_poem)
                metaText = context.getString(R.string.stillpoets_widget_empty_meta)
                tagText = "StillPoets"
                hintText = "打开 ›"
                deeplink = ""
                views.setViewVisibility(R.id.widget_next_btn, View.GONE)
            }

            views.setTextViewText(R.id.widget_tag, tagText)
            views.setTextViewText(R.id.widget_meta_text, metaText)
            views.setTextViewText(R.id.widget_hint_text, hintText)

            // 4. 自适应字号与行数：大尺寸平板卡片更舒展，小卡片更紧凑
            val textSizeSp = when {
                heightDp >= 210 && widthDp >= 320 -> 18f
                heightDp >= 160 -> 16.5f
                widthDp <= 210 -> 13.5f
                else -> 15f
            }
            val maxLines = when {
                heightDp >= 220 -> 7
                heightDp >= 170 -> 5
                heightDp <= 110 -> 3
                else -> 4
            }

            // 5. 若配置了本地自定义 .ttf/.otf 字体，使用高清 StaticLayout 渲染以呈现真实字形；否则走原生清晰衬线 TextView
            val customTypeface = resolveCustomTypeface(context, theme.fontFile)
            val renderedPoemBmp = if (customTypeface != null) {
                renderCustomFontText(
                    context = context,
                    text = poemText,
                    typeface = customTypeface,
                    textColor = textColor,
                    textSizeSp = textSizeSp,
                    maxLines = maxLines,
                    contentWidthDp = max(widthDp - 36, 120),
                    contentHeightDp = max(heightDp - 64, 48),
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

        private fun formatMeta(entry: WidgetEntry): String {
            val parts = mutableListOf<String>()
            val title = entry.poemTitle.trim()
            val author = entry.author.trim()
            val source = entry.sourceTitle.trim()

            if (title.isNotEmpty()) {
                parts.add("《$title》")
            } else if (source.isNotEmpty()) {
                parts.add("《$source》")
            }
            if (author.isNotEmpty()) {
                parts.add(author)
            }
            if (parts.isEmpty() && source.isNotEmpty()) {
                parts.add("《$source》")
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
                    bgColor = themeObj?.optString("bgColor").orEmpty(),
                    textColor = themeObj?.optString("textColor").orEmpty(),
                    accentColor = themeObj?.optString("accentColor").orEmpty(),
                    fontFamily = themeObj?.optString("fontFamily").orEmpty(),
                    fontFile = themeObj?.optString("fontFile").orEmpty(),
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
                    theme = theme,
                    entries = entries,
                )
            } catch (_: Throwable) {
                null
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
            val w = max(180, min(720, (widthDp * density).roundToInt()))
            val h = max(100, min(540, (heightDp * density).roundToInt()))
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
            maxLines: Int,
            contentWidthDp: Int,
            contentHeightDp: Int,
        ): Bitmap? {
            return try {
                val dm = context.resources.displayMetrics
                val scale = min(dm.density, 2.25f)
                val maxW = max(160, min(720, (contentWidthDp * scale).roundToInt()))
                val maxH = max(64, min(420, (contentHeightDp * scale).roundToInt()))

                val paint = TextPaint(Paint.ANTI_ALIAS_FLAG or Paint.SUBPIXEL_TEXT_FLAG).apply {
                    color = textColor
                    textSize = textSizeSp * scale
                    this.typeface = typeface
                }

                val layout = StaticLayout.Builder.obtain(text, 0, text.length, paint, maxW)
                    .setAlignment(Layout.Alignment.ALIGN_NORMAL)
                    .setLineSpacing(0f, 1.38f)
                    .setIncludePad(false)
                    .setMaxLines(maxLines)
                    .setEllipsize(TextUtils.TruncateAt.END)
                    .build()

                val bmpH = max(1, min(maxH, layout.height + 4))
                val bmp = Bitmap.createBitmap(maxW, bmpH, Bitmap.Config.ARGB_8888)
                val canvas = Canvas(bmp)
                val dy = max(0f, (bmpH - layout.height) / 2f)
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

        private fun isDarkColor(color: Int): Boolean {
            val lum = 0.299 * Color.red(color) + 0.587 * Color.green(color) + 0.114 * Color.blue(color)
            return lum < 128.0
        }
    }

    private data class WidgetTimeline(
        val generatedAt: Long,
        val theme: WidgetTheme,
        val entries: List<WidgetEntry>,
    )

    private data class WidgetTheme(
        val preset: String,
        val bgColor: String,
        val textColor: String,
        val accentColor: String,
        val fontFamily: String,
        val fontFile: String,
    ) {
        companion object {
            fun defaultArtbook() = WidgetTheme(
                preset = "artbook",
                bgColor = "#F4F4EF",
                textColor = "#1F2320",
                accentColor = "#3A6B5C",
                fontFamily = "",
                fontFile = "",
            )
        }
    }

    private data class WidgetEntry(
        val entryType: String,
        val displayText: String,
        val poemTitle: String,
        val author: String,
        val sourceTitle: String,
        val deeplink: String,
    )
}
