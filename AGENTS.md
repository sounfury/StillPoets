# StillPoets 项目开发指南（AGENTS.md）

## 1. 新对话必读上下文

在开始任何需求分析、架构设计、排障或代码修改前，**必须先阅读以下两份核心文档**，确保与当前设计基线一致：

1. **产品需求文档**：`docs/prd.md`
2. **系统架构设计文档**：`docs/architecture.md`

> 补充：若任务涉及界面交互与视觉风格，可一并参考 `docs/prototypes/`（注意 `docs/prototypes/注意.md`：原型中旧名 `AnyQuote` 已统一更名为 `StillPoets`）。

---

## 2. 项目核心边界与技术栈

- **产品定位**：私有化（BYOD）多端现代诗集阅读、独立散页管理、读诗统计、高清分享出卡与桌面小组件联动应用。
- **目标平台**：
  - 核心移动客户端：**iOS（iPhone，iOS 17+）** + **Android（API 26+，优先做好平板大屏/双栏自适应布局）**。
  - 桌面辅助：仅通过零部署单文件油猴脚本（`scripts/stillpoets-manager.user.js`）提供 WebDAV 书库管理与 EPUB/Z-Library 投递。
  - **明确不做**：Windows / macOS 原生桌面阅读客户端。
- **核心技术栈**：
  - **UI 与核心逻辑**：MyGo 纯 Go 原生 UI（`github.com/egoist/mygo` / `package ui`），单一逻辑内核下沉到 `internal/`；分享卡片导出采用纯 Go 3× 离屏渲染引擎（`internal/ui/sharecard/`）。
  - **本地存储**：SQLite（含 FTS5、`reading_events` 统计表、`pending_uploads` 待传队列及 `books.content_path` 幂等补列）+ 本地诗集/字体缓存。
  - **跨端同步**：用户自带 WebDAV（无中心化内容服务器）。
  - **原生桥接与扩展（`modules/` & `native/`）**：
    - `modules/widgetbridge/` + `native/ios/StillPoetsWidget/`：iOS WidgetKit（`.appex`，App Group 优先 + Keychain Access Group 回退）与 Android AppWidget（`StillPoetsWidgetProvider`），通过主 App 预编译 `widget_timeline.json` 与 `stillpoets://` Deeplink 联动。
    - `modules/appupdater/`：Android 应用内 APK 未知来源权限探测与 `FileProvider` 安装桥接。
    - `modules/photosaver/`：Android `MediaStore` / iOS `PHPhotoLibrary` 相册直存桥接。
- **当前阶段与范围收敛**：
  - **已落地功能（截至 v0.1.10）**：EPUB 解析导入与原件备份、入口 B 独立散页库、WebDAV 元数据/阅读事件同步与 `inbox/` 无感消费、现代诗阅读器（长按段落悬浮菜单、勾选多行划句、目录实时全文检索、全局检索高亮、偶遇进度保护、自定义字体/字重）、三套主题与平板双栏、双端原生小组件、Android 应用内自动更新、完整读诗统计看板（完读墙 / 热力图 / 昼夜节律 / 诗人作者云）、高清分享卡片导出、零部署油猴书库管理与 Z-Library 一键直投。
  - **待推进 V1.1 剩余项**：单诗微批注与长书评、古诗词竖排阅读。除非明确要求，不要无故扩散范围。

---

## 3. 架构与安全铁律

1. **元数据与正文物理分离**：多端采用 **Per-Device 单写者代次快照**（`StillPoets/state/device_<installId>_<rev>.json.gz`，保留最近 2 份已验证代次，基于 Lamport 逻辑时钟字段级确定性合并）同步元数据与阅读事件（`groups` / `books` 含不可变 `contentPath` / `favorites` / `quotes` / `progress` 含 `readPoemIds` / `readingEvents`）；外观设置、小组件配置与自定义字体留在**本机**；诗集正文（`parsed.spbook.json.gz`）严格按需下载，不自动全量拉取。
2. **油猴脚本与 `inbox/` 安全边界**：油猴脚本仅在独立可信入口接受 WebDAV 凭据（严禁在第三方页面暴露密码输入），写回快照必须采用 2A 累计稀疏快照仅管理 `groups` / `books`（绝不覆盖/污染收藏、散页、进度与统计）；`StillPoets/inbox/<bookId>.ready.json` 必须校验 SHA-256、写入不可变 `builds/<attemptId>/parsed.spbook.json.gz`，且仅在新代次快照回读验证包含该书后再删除清单。
3. **双入口互不污染**：入口 A（结构化诗集）与入口 B（散装独立句子）在数据模型、存储与筛选上保持独立。
4. **凭据安全**：WebDAV 账号密码严禁写入 `settings.json`、SQLite、同步文件或日志，必须使用系统级安全存储（iOS Keychain / Android Keystore）。
5. **网盘兼容性**：WebDAV 同步必须兼顾坚果云、Nextcloud、群晖 NAS 与 **AList/OpenList（如 115 挂载点）** 的行为差异——严禁高频细粒度写入与共享文件竞争；必须支持低并发（覆盖响应体读取）、防抖批量串行提交、遵守 `Retry-After`、禁止不安全重定向降级、强制 `Accept-Encoding: identity` 防止透明解压，并保留原子上传开关。

---

## 4. 代码目录速览

- `main.go` / `mygo.json` / `apps.json`：应用入口、原生导航壳、平台打包配置与 iOS 软件源清单。
- `internal/domain/`：核心领域模型（`Version` / `BookGroup` / `Book` / `SPBook` / `Poem` / `FavoritePoem` / `Quote` / `ReadingProgress` / `ReadingEvent` / `Settings` / `WidgetTimeline`）。
- `internal/parser/`：EPUB 解析器、XHTML 条目/题记/落款/注释提取与节选生成。
- `internal/storage/`：SQLite Schema（`schema.sql` + 幂等迁移）与本地仓储（常驻元数据、阅读统计查询、`pending_uploads` 与按需正文缓存分离）。
- `internal/sync/`：WebDAV 客户端与协议操作（`webdav/`）、代次快照与 `inbox` 协议（`protocol.go`、`inbox.go`）、Lamport 确定性合并引擎（`merger.go`）与串行同步调度（`engine.go`）。
- `internal/app/`：应用服务编排（`service*.go`）、`inbox` 消费、应用内更新下载校验（`updater.go`、`release.go`）、WebDAV 迁移、小组件时间线预编译（`widget.go`）与诊断日志。
- `internal/ui/`：主题 Token 与纸纹（`theme/`）、响应式双形态布局（`layout/`）、公共组件（`components/`）、高清分享卡片离屏引擎（`sharecard/`）与全部原生页面视图（`views/`）。
- `modules/` & `native/ios/`：双端原生桥接（`widgetbridge` / `appupdater` / `photosaver`）与 iOS WidgetKit Extension 工程。
- `scripts/`：零部署油猴脚本及单测（`stillpoets-manager.user.js` / `.test.js`）与双端 CI 发布辅助脚本。
- `quality/` & `.woodpecker/`：纯 Go AST 质量检查器与 8 项 Woodpecker CI 质量门禁配置。

---

## 5. 协作约定与质量门禁

- 回复默认使用简体中文，代码、命令、路径与符号名保留原样。
- 涉及 `internal/domain`、`internal/parser`、`internal/storage`、`internal/sync`、`internal/app` 或 `scripts/` 核心逻辑变更时，优先补充并运行对应单元测试验证边界条件。
- 改动代码须遵守项目 **Woodpecker 8 项质量门禁**（推送即触发）：
  - Go 源码（`quality/go-quality.json`，`main.go` / `internal` / `modules`，排除 `*_test.go` 与 `*_native.go`）：单文件 **≤ 800 行**、圈复杂度 **≤ 15**、认知复杂度 **≤ 20**、嵌套深度 **≤ 4**、参数个数 **≤ 6**、**零死代码**。
  - 全仓重复代码（`quality/jscpd.json`）：`minLines: 8`、`minTokens: 70`、**0% 重复率**。
  - 密钥扫描（`quality/gitleaks.toml`）：`gitleaks` 默认规则，**零泄露且不可跳过**。
  - 提交前建议本地执行复检：
    ```bash
    go vet ./...
    for rule in file-length cyclomatic cognitive nesting params dead-code; do
      go run ./quality/gocheck --config quality/go-quality.json "$rule"
    done
    ```
- 如需求与 `docs/prd.md` 或 `docs/architecture.md` 存在变更，代码落地前后应同步更新对应文档。
