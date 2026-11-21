# StillPoets 开发进展记录

> 最后更新：2026-11-22  
> 当前阶段：**Phase 1 已提交并完成 Android 真机首包装机，进入真机体验问题记录与打磨阶段**

---

## 一、里程碑总览

| 阶段 | 状态 | 说明 |
| --- | --- | --- |
| **Phase 0：骨架、文档与同步协议** | ✅ 已完成（`3869078`） | 项目骨架、`docs/prd.md`、`docs/architecture.md`、核心协议模型初版 |
| **Phase 1：V1.0 核心引擎与 V3 原生 UI** | ✅ 已提交（`622121d`） | EPUB 解析、SQLite+FTS5、WebDAV 多端同步引擎、应用编排层、V3 手机/平板自适应原生 UI、全仓单元测试与无头渲染测试通过 |
| **Android 真机首包验证（OPPO Pad Mini）** | ✅ 已装机运行 | 成功产出 `android/arm64` APK（17.2 MB）并通过 ADB 安装启动 |
| **Phase 1 真机体验打磨与联调** | 🚧 进行中 | 已开始真机实测并收集交互/动画问题；待完成实机 WebDAV 联调 |
| **Phase 2：双端原生桌面小组件联动** | ⏳ 待开始 | Android Jetpack Glance 微件 + iOS WidgetKit Extension + `widgetbridge` + Deeplink 高亮跳转 |
| **Phase 3：V1.1 扩展功能** | ⏳ 规划中 | 单诗微批注与长书评、完整统计看板（完读墙/热力图/作者云）、高清卡片导出、古诗词竖排 |

---

## 二、已完成交付详情

1. **文档与工程规范**
   - 新增 `AGENTS.md` 规范新对话上下文与核心安全/架构红线。
   - 完善 `docs/architecture.md` 与 `docs/prototypes/design-direction.md`（V3 视觉规范与双形态截图基准）。
   - 更新 `.gitignore`，排除本地测试版权书籍 `test-books/` 与构建产物。
2. **核心后端与同步引擎（`internal/`）**
   - `internal/domain/`：完整 V1.0 领域模型（`BookGroup`、`Book`、`SPBook`、`Poem`、`FavoritePoem`、`Quote`、`ReadingProgress`、`Settings`、`WidgetTimeline`）。
   - `internal/parser/`：EPUB 诗集解析器，支持标题/章节分类、题记与落款提取、行内注释解析与默认节选生成（经 4 本真实 EPUB 诗集测试验证）。
   - `internal/storage/`：SQLite v2 Schema + FTS5 全文搜索，元数据常驻表与诗集正文按需缓存表物理分离，含 `pending_uploads` 待上传队列。
   - `internal/sync/`：Per-Device 单写者代次快照协议、Lamport 确定性合并引擎、兼容 AList(115)/坚果云的安全 WebDAV 客户端与防抖串行同步调度器。
   - `internal/app/`：无界面应用服务编排、`stillpoets://` Deeplink 解析器、24 小时 `widget_timeline.json` 原子预编译器。
3. **V3 原生界面（`internal/ui/` & `main.go`）**
   - 自适应双形态布局：手机底栏三导航 + 全屏阅读；平板（≥ 760 DIP）左侧窄导航轨 + 左书架 / 右阅读分栏工作区。
   - 内置 3 套主题（画册风 / 刊物风 / 暗夜极简），支持 `.ttf` / `.otf` 自定义字体导入。
   - 诗集/散页双入口、沉浸阅读器（整诗收藏、逐行划句留散页或设为本诗金句、目录抽屉、`Aa` 排版调节）、轻量统计页与「我的」设置页。
4. **测试与真机构建**
   - `go vet ./...`、`mygo vet .` 与 7 个 Go 测试包全部通过。
   - 已通过 Gradle init script（`D:\dev-environments\gradle-user-home\init.d\fix-mygo-sqlitecore-cmake.gradle`）绕过 Windows 下 MyGo `sqlite` 插件的 CMake 反斜杠路径兼容性问题，成功打包并安装到 **OPPO Pad Mini（`arm64-v8a`）**。

---

## 三、实机测试已知问题（待后续修复）

> 以下问题来自 OPPO Pad Mini 真机实测反馈，当前仅记录归档，暂未改动代码：

1. **[交互 / 动效] App 整体动画需要打磨**
   - 现状：页面切换、弹窗/抽屉展开收起、书架与阅读器切换等缺少平滑自然的过渡动画，交互手感偏生硬。
   - 预期：补齐关键导航、抽屉/弹层与切诗过渡动效，提升整体阅读沉浸感与跟手度。
2. **[阅读器 / 手势] 无法左右滑动切换上一首 / 下一首诗**
   - 现状：当前阅读器仅能通过底部「上一首 / 下一首」按钮切换诗篇，无法在正文区域通过横向滑动手势翻诗。
   - 预期：在阅读器正文区域支持左右滑动切换上一首/下一首诗（需处理好与长诗纵向滚动以及划句选行模式的冲突边界）。

---

## 四、后续待办队列

- [ ] 修复真机实测问题：补齐阅读器左右滑动切诗手势。
- [ ] 打磨 App 过渡与交互动画。
- [ ] 在 OPPO Pad Mini 上完成真实 EPUB 导入、自定义字体切换与 WebDAV（AList / 坚果云）多端同步闭环验证。
- [ ] 进入 Phase 2：实现 Android Jetpack Glance 中号桌面小组件、`widgetbridge` 原生模块及点击小组件经 `stillpoets://` 唤起 App 高亮原诗。
- [ ] 规划 iOS / iPadOS WidgetKit Extension 与 macOS 真机签名构建。
