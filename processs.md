# StillPoets 开发进展记录

> 最后更新：2026-11-22  
> 当前阶段：**Phase 2 双端（Android + iOS）原生桌面小组件全部真机验证通过；进入 Android + iOS 双端 WebDAV 跨设备同步联调与 Phase 3（V1.1）规划阶段**

---

## 一、里程碑总览

| 阶段 | 状态 | 说明 |
| --- | --- | --- |
| **Phase 0：骨架、文档与同步协议** | ✅ 已完成（`3869078`） | 项目骨架、`docs/prd.md`、`docs/architecture.md`、核心协议模型初版 |
| **Phase 1：V1.0 核心引擎与 V3 原生 UI** | ✅ 已提交（`622121d`） | EPUB 解析、SQLite+FTS5、WebDAV 多端同步引擎、应用编排层、V3 手机/平板自适应原生 UI、全仓单元测试与无头渲染测试通过 |
| **Android 真机首包验证（OPPO Pad Mini）** | ✅ 已装机运行 | 成功产出 `android/arm64` APK（17.2 MB）并通过 ADB 安装启动 |
| **Phase 1 真机体验打磨与联调** | 🚧 进行中 | 已开始真机实测并收集交互/动画问题；待完成实机 WebDAV 联调 |
| **Phase 2：双端原生桌面小组件联动** | ✅ 已完成并真机验证（`0d7d66d`） | Android 原生 AppWidget + `widgetbridge` 已在 OPPO Pad Mini 验证；iOS WidgetKit（小/中/大号 + iOS 17 原地换诗）+ Swift Keychain/AppGroup 桥接已通过 GitHub Actions `macos-latest` 产出未签名 `.ipa`（6.5 MB）并在 iPhone 自签安装、小组件运行成功 |
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
4. **双端桌面小组件与云端构建（`modules/widgetbridge/`、`native/ios/`、`.github/workflows/ios-build.yml`）**
   - **Android 侧**：`modules/widgetbridge/kotlin/` 实现原生 `StillPoetsWidgetProvider`（4×2 诗笺自由缩放、3 套主题与自定义 `.ttf/.otf` 渲染、桌面右上角换诗、`stillpoets://` 冷/热启动直达高亮原诗），已在 OPPO Pad Mini 装机验证。
   - **iOS 侧**：`modules/widgetbridge/swift/` + `native/ios/StillPoetsWidget/` 实现小/中/大号 WidgetKit 扩展与 iOS 17 `NextPoemIntent` 原地换诗，采用 App Group 优先 + 自动探测证书前缀的 Keychain Access Group 共享通道，零配置兼容企业通配符证书。
   - **GitHub Actions 云端打包**：`.github/workflows/ios-build.yml`（`macos-latest`）支持 `unsigned`（默认免 Secrets 产出真机未签名 `.ipa`，供全能签/轻松签自签）、`signed`（云端直签）与 `simulator` 三模式；实测一次构建成功（1 分 41 秒，6.5 MB），iPhone 实机安装及桌面小组件均已验证通过。
5. **测试与真机构建**
   - `go vet ./...`、`mygo vet .`、`mygo native check modules/widgetbridge` 与 7 个 Go 测试包全部通过。
   - 已通过 Gradle init script（`D:\dev-environments\gradle-user-home\init.d\fix-mygo-sqlitecore-cmake.gradle`）绕过 Windows 下 MyGo `sqlite` 插件的 CMake 反斜杠路径兼容性问题，成功打包并安装到 **OPPO Pad Mini（`arm64-v8a`）**。

---

## 三、实机测试已知问题与修复状态

> 以下问题来自 OPPO Pad Mini 真机实测反馈：

1. **[交互 / 动效] App 整体动画打磨 — ✅ 已修复**
   - 新增统一动效 Token（`internal/ui/theme/motion.go`），确立零过冲纸页弹簧与柔和过渡节奏。
   - 补齐主导航（书架/统计/我的）淡入微浮过渡、手机端全屏阅读器右侧推入/滑出转场、划选诗行操作条平滑折叠展开、底部云同步胶囊浮现/淡出以及书封/卡片/导航按钮微缩按压反馈。
   - 将新增菜单、诗集目录、排版 `Aa` 升级为可下拉关闭的原生底部手势抽屉（`mobile.Sheet`，平板居中限宽）；将记散页、散页详情、分组管理、全局检索升级为带遮罩淡入淡出与卡片缩放上浮的动效弹窗。
2. **[阅读器 / 手势] 左右滑动切换上一首 / 下一首诗 — ✅ 已修复**
   - 在阅读器正文区域实现 O(1) 窗口化横向跟手翻诗轨道（`internal/ui/views/reader.go`），支持左右滑动跟手切换上一首/下一首诗、首尾橡皮筋阻尼回弹、松手即刻同步顶栏/收藏/页码与已读进度。
   - 妥善处理手势边界：长诗纵向滚动互不冲突；进入「划选诗行」模式时自动锁定横向切诗防止误翻；点击底栏按钮走平滑滑切，目录远距离跨诗跳转走柔和淡入过渡。
3. **[手势] 诗集 / 散页手势切换 — ✅ 已修复**
   - 手机端与平板端均支持横向跟手滑动在「诗集」与「散页」间平滑切换，且顶部下划线指示条随手势进度实时滑移。
   - 平板端做好双层手势隔离：右栏阅读器横滑专职切诗，左栏书架左滑可切至全宽散页瀑布流、散页右滑回双栏工作区，且不与内部横向分组条/出处筛选条冲突。

---

## 四、后续待办队列

- [x] 修复真机实测问题：补齐阅读器左右滑动切诗手势。
- [x] 修复真机实测问题：支持诗集 / 散页之间手势切换。
- [x] 打磨 App 过渡与交互动画。
- [x] 在 OPPO Pad Mini 上完成真实 EPUB 导入验证。
- [x] 进入 Phase 2（Android）：实现 Android 原生桌面小组件（`modules/widgetbridge` + `StillPoetsWidgetProvider`）、App 内实时预览与一键添加桌面引导，以及点击小组件经 `stillpoets://` 冷/热启动唤起 App 高亮原诗或展开散页。
- [x] 进入 Phase 2（iOS）：实现 iOS WidgetKit Extension（`native/ios/StillPoetsWidget/`）、Swift `widgetbridge` 双通道共享与 GitHub Actions `macos-latest` 打包工作流（支持默认 `unsigned` 免证书未签名真机 `.ipa` 及 `signed` 云端直签模式），已完成云端构建并在 iPhone 真机自签安装与小组件实测通过。
- [x] 完成 `gpt-6.2-sol` 全仓深度 CR 并清零全部 P0 及核心 P1/P2 隐患（UI 主线程 `win.Update` 状态收口、单调请求代次防串书与乱序覆盖、导书+待传单事务原子化与同快照阻断幽灵书目、退役强覆盖校验、WebDAV 跨域 HTTPS 无凭据直链与写后 404 退避、可取消同步锁与优雅退出、小组件唯一临时文件原子写等），全仓测试通过并已重装平板。
- [x] Phase 3 前体验细节打磨：桌面小组件自动换诗频率设置（15 分钟 ~ 每天，iOS WidgetKit 与 Android AppWidget 同步本地时区槽位调度）、iOS 液态玻璃悬浮胶囊底栏与设置页开关（iOS 默认开启，可切换经典贴底栏）、刊物风清亮纸白背景（`#FFFFFF`）及原生窗口底色实时联动、设置页「使用原书封面」统一开关（支持 EPUB 原书封面按需加载与无封面自动回退色块）。
- [ ] 完成 iPhone + OPPO Pad Mini 双端 WebDAV（AList / 坚果云）同步闭环联调（书目、阅读进度、收藏、散页双向合并与正文按需拉取）。
- [ ] 黑夜模式下自动切换到黑夜主题
- [ ] 解决进度追踪问题，从小组件点过来跳转到的某首诗，使得进度跳跃，无法跳回顺序阅读时的进度
- [ ] 推进 Phase 3（V1.1 扩展功能）：单诗微批注与长书评、完整统计看板（完读墙/热力图/作者云）、高清分享卡片导出、古诗词竖排阅读。
