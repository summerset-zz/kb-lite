# 09 · 路线图

每个阶段都有**可验收的产出**，做完才进下一阶段。不要并行铺开。

## v0.1 · 协议跑通（无 UI、无落盘）

**目标**：自己（summerset）能用它替代现有的知识库调用，跑满一周。

范围：

- `internal/core`：切片 + 打分（完整实现 02 / 03）
- `internal/daemon`：内存索引（启动时全扫）、HTTP 检索接口（`search` / `entry` / `toc` / `section` / `nearby`）
- `cmd/kb-lite` + `internal/cli`：`serve` / `search --explain` / `read` / `toc`
- `GET /skill.md`（模板见 [skill-template](./skill-template.md)）
- token + Host 校验

**不做**：SQLite、UI、管理接口、git。

**验收**：

1. 对 `D:\notes`（≥ 300 篇中文笔记）启动到可服务 < 3 s；
2. `search` 的 `--explain` 输出能解释每一条排序；
3. 15 个手工查询的排序看起来是对的（尤其：命中文档标题的排在只命中全文的前面）；
4. 一个真实 agent 用 `skill.md` 接上后，能独立完成“查某件事，读对应章节”而不需要提示。

> 这一阶段结束就应该能回答“这套协议是不是真的比 basic-memory 好用”。**如果答案是否，后面全都不用做了。**

## v0.2 · 落盘与增量

- SQLite（`modernc.org/sqlite`，纯 Go；无驱动时降级纯内存）
- sha1 跳过未变文件；`--force`
- 索引期间提供旧数据 + `/api/status` 的 `state`
- `manifest`（预算式概览）

**验收**：1000 篇的库二次启动 < 500 ms；删掉 `index.db` 后首次启动结果完全一致。

## v0.3 · 管理接口、Web UI 与托盘

- 管理接口：`/api/kbs` 全套、`/api/fs/list`、`/api/settings`、`/api/auth/token`、`/api/skill/install`、`/api/shutdown`；
  `/api/*` 用 **admin 凭据 + 同源信号**，读取面 token 默认关（ADR-015）
- UI（`web/`，`//go:embed` 进二进制）：向导、知识库页、设置页、作者页、两份 skill 的安装按钮、**Agent 视角页**、搜索调试页
- Windows 托盘（纯 Go，ADR-014）：两态图标 + 右键菜单（暂停/启动、设置、在命令行中打开、退出）
- CLI 平权：`settings` / `skill --install` / `start` / `stop` / `quit` / `doctor`（`docs/06` 第十节是验收清单）
- `serve --open` 打开 UI

**验收**：

1. 一个完全不懂命令行的人，从双击 exe 到跑通向导 < 3 分钟（找 3 个真人试，别看自己）；
2. **界面上的每句话都能被“只会用 AI、不懂电脑”的人读懂**：抽出所有文案给他读，他能说出“下一步点哪儿”（`docs/06` 第八节）；
3. 删除 KB 之后，目录里的文件 md5 与之前完全一致；
4. 托盘暂停再启动，不需要重新整理（索引还在）。

## v0.4 · 分发

- 三平台构建 + Release（`.github/workflows/release.yml`，目前还没有）
- README / 宣传页 / 截图 / FAQ
- `kb-lite doctor` 补齐（若 v0.3 只做了骨架）
- 开机自启命令（`kb-lite autostart enable`）

**验收**：从 Releases 下载 → 双击 → 见到界面，全程不需要安装任何东西（在一台没装过 Node / Go / Python 的 Windows 干净机器上验）。

## v0.5 · git

- `kb-lite sync <name>`（`git pull --ff-only` + reindex）
- KB 卡片上显示 git 状态
- **只读 git**：不 add / commit / push

## v1.0

- 索引格式冻结（`schema_version` 与 `PRAGMA user_version` 定版）
- 文档补全，issue 模板上线

## v2 · 桌面壳（条件触发，见 [06](./06-desktop-ui.md#触发升级到-tauri-的条件)）

托盘、单实例、原生目录选择、自动更新。

---

## 明确不在路线图里

向量检索、写入 API、PDF、多用户、云同步、内置聊天、后台维护 agent。

这些不是“以后再说”，是“不做”。加进来会毁掉这个产品唯一的卖点：**可解释、可离线、可替换**。
