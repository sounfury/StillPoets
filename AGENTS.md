# StillPoets 项目开发指南（AGENTS.md）

## 1. 新对话必读上下文

在开始任何需求分析、架构设计、排障或代码修改前，**必须先阅读以下两份核心文档**，确保与当前设计基线一致：

1. **产品需求文档**：`docs/prd.md`
2. **系统架构设计文档**：`docs/architecture.md`
3. **进度文档**：`process.md`

> 补充：若任务涉及界面交互与视觉风格，可一并参考 `docs/prototypes/`（注意 `docs/prototypes/注意.md`：原型中旧名 `AnyQuote` 已统一更名为 `StillPoets`）。

---

## 2. 项目核心边界与技术栈

- **产品定位**：私有化（BYOD）多端诗集阅读、独立句子管理与桌面小组件联动应用。
- **目标平台**：
  - 第一期：**iOS（iPhone）** + **Android（优先做好平板大屏/双栏自适应布局）**。
  - 后续可选：Web / PWA。
  - **明确不做**：Windows / macOS 桌面端。
- **核心技术栈**：
  - **UI 与核心逻辑**：MyGo 纯 Go 原生 UI（`github.com/egoist/mygo` / `package ui`），单一逻辑内核下沉到 `internal/`；V1.1 复杂排版页面按需局部混用 WebView。
  - **本地存储**：SQLite（含 FTS5）+ 本地诗集/字体缓存。
  - **跨端同步**：用户自带 WebDAV（无中心化内容服务器）。
  - **桌面小组件**：iOS WidgetKit（`.appex`）+ Android Jetpack Glance，通过主 App 预编译 `widget_timeline.json` 快照与 `stillpoets://` Deeplink 联动。
- **版本范围收敛**：
  - **V1.0 基线**：EPUB 导入（含原文件备份）+ 入口 B 独立句子库 + WebDAV 元数据同步与按需下载 + 现代诗横排阅读器（单诗上下滑、左右滑切诗、自定义字体）+ 诗集/诗篇 ❤️ 收藏 + 基础小组件（手动选句设为本诗金句）。
  - **V1.1 延后**：单诗微批注与长书评、数据统计（完读墙/热力图/时段占比/作者云）、高清分享卡片导出、古诗词竖排阅读、锁屏/大号小组件。除非明确要求，不要在 V1.0 阶段提前扩散范围。

---

## 3. 架构与安全铁律

1. **元数据与正文物理分离**：多端采用 **Per-Device 单写者代次快照**（`state/device_<installId>_<rev>.json.gz`，保留最近 2 份已验证代次，基于 Lamport 逻辑时钟字段级确定性合并）仅同步收敛后的 V1.0 元数据（`groups` / `books` / `favorites` / `quotes` / `progress` 含 `readPoemIds`）；外观设置、小组件配置与自定义字体留在**本机**；诗集正文（`parsed.spbook.json.gz`）严格按需下载，不自动全量拉取。
2. **双入口互不污染**：入口 A（结构化诗集）与入口 B（散装独立句子）在数据模型、存储与筛选上保持独立。
3. **凭据安全**：WebDAV 账号密码严禁写入 `settings.json`、SQLite、同步文件或日志，必须使用系统级安全存储（iOS Keychain / Android Keystore）。
4. **网盘兼容性**：WebDAV 同步必须兼顾坚果云、Nextcloud、群晖 NAS 与 **AList/OpenList（如 115 挂载点）** 的行为差异——严禁高频细粒度写入与共享文件竞争；必须支持低并发（覆盖响应体读取）、防抖批量串行提交、遵守 `Retry-After`、禁止不安全重定向降级、强制 `Accept-Encoding: identity` 防止透明解压，并保留原子上传开关。

---

## 4. 代码目录速览

- `main.go` / `mygo.json`：应用入口、原生导航壳与平台打包配置。
- `internal/domain/`：核心领域模型（Version / BookGroup / Book / SPBook / Poem / FavoritePoem / Quote / ReadingProgress / Settings / WidgetTimeline）。
- `internal/storage/`：SQLite Schema（`schema.sql` v2）与本地仓储（常驻元数据表、待上传队列 `pending_uploads` 与按需缓存正文表分离）。
- `internal/sync/`：WebDAV 客户端（`webdav/`）、代次快照与 `.spbook` 协议（`protocol.go`）、Lamport 确定性合并引擎（`merger.go`）与串行同步调度/导书上传/旧设备退役（`engine.go`）。
- `internal/ui/`：主题 Token（`theme/`）、响应式平板双栏布局（`layout/`）与页面视图。

## 5. 协作约定

- 回复默认使用中文。
- 涉及 `internal/domain`、`internal/sync`、`internal/storage` 等核心逻辑变更时，优先补充/运行单元测试（`go test ./...`）验证边界条件。
- 如需求与 `docs/prd.md` 或 `docs/architecture.md` 存在变更，代码落地前后应同步更新对应文档。
