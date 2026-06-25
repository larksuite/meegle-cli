# Meegle CLI

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Node.js](https://img.shields.io/badge/node-%3E%3D16-brightgreen.svg)](https://nodejs.org/)
[![npm version](https://img.shields.io/npm/v/@lark-project/meegle.svg)](https://www.npmjs.com/package/@lark-project/meegle)

[English](./README.md) | [简体中文](./README.zh-CN.md)

飞书项目（[Meegle](https://meegle.com?utm_source=github&utm_medium=readme&utm_campaign=meegle_cli) / [Lark Project](https://project.feishu.cn?utm_source=github&utm_medium=readme&utm_campaign=meegle_cli)）命令行工具。在终端中管理工作项、查看排期、搜索数据，无需打开浏览器。

[安装](#安装) · [快速开始](#快速开始人类用户) · [Agent Skill](#ai-agent-skill) · [命令](#命令一览) · [认证](#认证) · [配置](#配置) · [安全](#安全与风险提示) · [贡献](#贡献)

## 为什么选择 Meegle CLI？

- **Agent 友好** — 附带一份 [AI Agent Skill](#ai-agent-skill)，一条命令即可把 Meegle 操作手册喂给 Trae、Claude Code、Cursor、Windsurf、Gemini CLI 等主流 Agent。CLI 命令同时面向人类和 Agent 设计，结构化 JSON 输出、`--dry-run` 预览、`--device-code` 无 TTY 流程
- **覆盖完整** — 16 个业务域（工作项、工作流、子任务、评论、工时、关联、我的工作、视图、图表、团队、用户、空间、附件、交付物、资源库、WBS 计划表），50+ 命令映射到 Meegle 核心能力
- **两层参数模型** — 日常用 `--flag-name` 轻便直接，复杂载荷（如 `fields[]`）用 `--params <json>` 兜底 —— 按场景选择合适粒度
- **输出格式灵活** — 支持 `json` / `table` / `ndjson` / `raw`，配合 `--select` 点路径投影可直接 pipe 给其他工具
- **默认安全** — 凭证存进系统 keychain、`${VAR}` 环境变量模板让 secret 不落地到 config 文件、多 profile 分离 staging / prod

## 功能概览

| 域                                                 | 能力                                                                                     |
| -------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| 📋 [工作项](#workitem--工作项域)                   | 创建、查看、更新、批量读、MQL 查询、查看操作记录、读元数据                               |
| 🔀 [工作流](#workflow--工作流域)                   | 节点和状态流转、更新节点字段、列出可用流转和必填字段                                     |
| ✅ [子任务](#subtask--子任务)                      | 创建、更新、完成、回滚子任务                                                             |
| 💬 [评论](#comment--评论域)                        | 在工作项上添加和列出评论                                                                 |
| ⏱️ [工时](#workhour--工时域)                       | 列出工时记录、查看团队成员排期                                                           |
| 🔗 [关联](#relation--关系域)                       | 列出关联工作项、查看关联类型定义                                                         |
| 📌 [我的工作](#mywork--工作台域)                   | 查看本周 / 已完成 / 逾期待办                                                             |
| 👁️ [视图](#view--视图域)                           | 创建和更新固定视图、按名称搜索视图                                                       |
| 📊 [图表](#chart--度量域)                          | 列出视图下的图表、查看图表详情                                                           |
| 👥 [团队与用户](#team--user--人员域)               | 列出团队、团队成员、搜索用户、查看当前登录身份                                           |
| 🗂️ [空间](#project--空间域)                        | 按关键词搜索空间                                                                         |
| 📎 [附件](#attachment--附件域)                     | 两段式上传/下载协议 —— `prepare-*` 基础命令 + `+upload` / `+download` 端到端快捷命令     |
| 📦 [交付物](#deliverable--交付物域)                | 列出交付物及所属根工作项、来源工作项                                                     |
| 🧩 [资源库](#resource--资源库)                     | 创建资源模板、查看资源库配置                                                             |
| 🗓️ [WBS 计划表](#wbs--wbs-计划表)                  | 查询草稿 / 已发布计划行、创建 / 编辑 / 发布 / 重置草稿、查询草稿进度、列流程资源库元素   |
| 🔐 [认证与配置](#认证)                             | OAuth 登录、Device Code 流程、多 profile 配置、环境变量注入                              |
| 🔗 [URL 解析](#url--url-解析)                      | 离线解析飞书项目 / Meegle URL，输出 `url_kind` + 结构化字段                              |
| 🤖 [Agent Skill](#ai-agent-skill)                  | 内置 skill 供 Trae / Claude Code / Cursor / Windsurf / Gemini / Copilot 使用             |

## 安装

### 前置条件

- Node.js >= 16（自带 `npm` / `npx`）

### 安装命令

```bash
npm install -g @lark-project/meegle
```

## 快速开始（人类用户）

> **给 AI Agent 的提示：** 如果你是在替用户完成这套安装的 AI Agent，请直接跳到 [快速开始（AI Agent）](#快速开始ai-agent--ci--无头环境) —— 那里有你需要的非交互步骤。

```bash
# （可选）持久化 host，后续登录就不用再选菜单
meegle config set host <host>

# 1. 登录（上下选择 host + 浏览器 OAuth）
meegle auth login

# 2. 查看本周待办
meegle mywork todo --action this_week --page-num 1

# 3. 查看帮助
meegle --help
meegle workitem --help

# 4. 查看命令参数详情
meegle inspect workitem.create
```

## 快速开始（AI Agent / CI / 无头环境）

`meegle auth login` 默认走上下选择菜单 + 浏览器 OAuth 回调，依赖真实 TTY，在 CI Runner、管道、Claude Code 这类没有 TTY 的环境里会挂起或报错。这些场景请改用 Device Code 流程 —— 命令会输出授权 URL，让用户在浏览器里完成授权。

**Step 1 — 安装 CLI**

```bash
npm install -g @lark-project/meegle
```

**Step 2 — 安装 Agent Skill**

> 推荐用于 AI Agent 场景；如果只是手动使用 CLI，则是可选步骤。Skill 会教 Agent 正确选择和执行 `meegle` 命令。

```bash
npx skills add larksuite/meegle-cli -y -g
```

**Step 3 — 持久化 host**

```bash
meegle config set host <host>
```

`<host>` 示例：`project.feishu.cn`、`meegle.com`，或自建租户域名（如 `your-tenant.example.com`）。

**Step 4 — Device Code 登录**

> 建议后台执行。命令会输出授权 URL —— 提取后发给用户，用户在浏览器完成授权后命令自动退出。

```bash
meegle auth login --device-code
```

如不想持久化 host，也可以每次显式传入：

```bash
meegle auth login --device-code --host <host>
```

**Step 5 — 验证**

```bash
meegle auth status
```

完全无人值守的 CI（没有真人参与）请改用环境变量注入 token，参见 [沙盒 / CI](#沙盒--ci直接注入环境变量)。

## AI Agent Skill

`skills/meegle/` 是一份可直接加载的 **skill**，用来教 AI Agent —— Trae、Claude Code、Cursor、Windsurf、Gemini CLI、GitHub Copilot CLI —— 通过本 CLI 操作 Meegle。它把命令目录、MQL 语法、字段值写法、富文本 Markdown 规则，以及常见写入流程的 SOP 全部打包好。

### 安装

```bash
# 先装 CLI（skill 底层会调用 meegle 命令）
npm install -g @lark-project/meegle

# 再加载 skill —— 会自动探测已装的 Agent，并在各自的 skill 目录里登记
npx skills add larksuite/meegle-cli -y -g
```

`npx skills add` 会从仓库读取 `skills/meegle/SKILL.md`，写入本机所有 Agent CLI 的 skill 目录。每次升级重跑一遍即可拉取最新内容。

### 覆盖内容

- **命令参考** — 每个 `meegle` resource / method 的必填参数与示例
- **MQL 搜索** — `workitem query` 的语法、运算符、作用域关键字
- **字段值** — 复杂字段载荷（数组、嵌套 JSON、日期区间）的写法
- **富文本** — Meegle 富文本编辑器支持的 Markdown 子集
- **SOP** — 创建工作项、节点流转、状态流转、更新字段等场景的分步剧本
- **授权守护** — 所有业务命令前强制先过 `meegle auth status`

完整内容见 [skills/meegle/SKILL.md](./skills/meegle/SKILL.md) 和 [skills/meegle/references/](./skills/meegle/references/)。

### 使用方式

安装后直接用自然语言和 Agent 对话。例如 Trae：

```
帮我看一下 PROJ 空间本周待办里的 P0 需求
```

Agent 会参考 skill，自动选择合适的 `meegle` 命令执行。配合 `--dry-run`（见 [安全](#安全与风险提示)）可以在 Agent 真正执行副作用前先预览请求。

> 小提示：skill 的名字 `meegle` 和 CLI 二进制同名。文档里提到 **`meegle` skill** 时指 `skills/meegle/` 里的文件；提到 **`meegle` CLI** 时指你 PATH 上的 `meegle` 命令。

## 命令一览

### workitem — 工作项域

| 命令 | 说明 |
|------|------|
| `workitem create` | 创建工作项 |
| `workitem get` | 查看工作项概况 |
| `workitem +batch-get` | 按 ID 批量读取工作项（客户端侧在 `workitem get` 之上做扇出；`+` 前缀代表场景/语法糖命令） |
| `workitem update` | 修改工作项字段 |
| `workitem query` | 使用 MQL 搜索工作项 |
| `workitem list-op-records` | 查看操作记录 |
| `workitem meta-types` | 查看工作项类型列表 |
| `workitem meta-create-fields` | 查看创建时可用字段 |
| `workitem meta-fields` | 查看字段配置 |
| `workitem meta-roles` | 查看角色配置 |

### workflow — 工作流域

| 命令 | 说明 |
|------|------|
| `workflow transition` | 流转或回滚节点 |
| `workflow transition-state` | 流转状态流状态 |
| `workflow get-node` | 查看节点详情 |
| `workflow update-node` | 修改节点 |
| `workflow meta-node-fields` | 查看节点字段配置 |
| `workflow list-state-transitions` | 查看可流转状态 |
| `workflow list-state-required` | 查看流转必填信息 |

### subtask — 子任务

| 命令 | 说明 |
|------|------|
| `subtask update` | 创建/修改/完成/回滚子任务 |

### comment — 评论域

| 命令 | 说明 |
|------|------|
| `comment add` | 添加评论 |
| `comment list` | 查看评论列表 |

### workhour — 工时域

| 命令 | 说明 |
|------|------|
| `workhour list-records` | 查看工时登记记录 |
| `workhour list-schedule` | 查看人员排期 |

### relation — 关系域

| 命令 | 说明 |
|------|------|
| `relation list` | 查看关联的工作项 |
| `relation meta-definitions` | 查看关联关系定义 |

### mywork — 工作台域

| 命令 | 说明 |
|------|------|
| `mywork todo` | 查看我的待办/已办 |

### view — 视图域

| 命令 | 说明 |
|------|------|
| `view create-fixed` | 创建固定视图 |
| `view get` | 查看视图详情 |
| `view update-fixed` | 更新固定视图 |
| `view search` | 按名称搜索视图 |
| `view list-multi-project-workitems` | 查看全景视图（multiProjectView）下的工作项列表 |

### chart — 度量域

| 命令 | 说明 |
|------|------|
| `chart get` | 查看图表详情 |
| `chart list` | 查看视图下的图表列表 |

### team / user — 人员域

| 命令 | 说明 |
|------|------|
| `team list` | 查看空间下的团队列表 |
| `team list-members` | 查看团队成员 |
| `user me` | 查看当前登录用户信息 |
| `user search` | 搜索用户信息 |

### project — 空间域

| 命令 | 说明 |
|------|------|
| `project search` | 搜索空间信息 |

### attachment — 附件域

| 命令 | 说明 |
|------|------|
| `attachment prepare-upload` | 上传预处理 —— 返回带签名的对象存储 URL 与分片计划 |
| `attachment prepare-download` | 下载预处理 —— 返回带签名的对象存储 URL 与分片计划 |
| `attachment +upload` | 端到端上传：预处理 + 签名 HTTP POST，返回 `file_token` 与文件元信息 |
| `attachment +download` | 端到端下载：预处理 + 签名 HTTP GET + 原子写文件，用于消费 `workitem get` / `comment list` 返回的 `file_url` |

### deliverable — 交付物域

| 命令 | 说明 |
|------|------|
| `deliverable list` | 列出交付物及其根工作项、来源工作项 |

### resource — 资源库

| 命令 | 说明 |
|------|------|
| `resource create` | 在启用资源库的工作项类型下创建资源模板（资源实例） |
| `resource meta-fields` | 查看资源库配置（资源字段、资源角色） |

### wbs — WBS 计划表

| 命令 | 说明 |
|------|------|
| `wbs list-draft-rows` | 在计划表草稿中按条件筛选行并返回指定字段 |
| `wbs list-instance-rows` | 在已发布的线上计划表实例中按条件筛选行并返回指定字段 |
| `wbs create-draft` | 为指定工作项实例创建新的计划表草稿 |
| `wbs edit-draft` | 对计划表草稿单行执行一次原子操作（新增 / 删除 / 恢复 / 排序 / 改名 / 改负责人 / 改排期），操作类型通过 `--params` 指定 |
| `wbs publish-draft` | 将编辑完成的草稿发布到线上 |
| `wbs reset-draft` | 将草稿重置为线上实例状态，放弃所有未发布的修改 |
| `wbs get-draft-progress` | 查询计划表草稿操作（创建 / 编辑 / 发布）的执行进度 |
| `wbs list-element-templates` | 列出流程资源库中的资源节点与资源任务模板 |

### auth — 认证域

| 命令 | 说明 |
|------|------|
| `auth login` | 登录（浏览器或 `--device-code`） |
| `auth logout` | 登出 |
| `auth status` | 查看登录状态（会向服务端发一次校验请求，确认 token 真实可用） |

### config — 配置域

| 命令 | 说明 |
|------|------|
| `config init` | 初始化配置 |
| `config show` | 查看当前配置 |
| `config set` | 设置配置项 |
| `config get` | 读取配置项 |
| `config profile create\|list\|use\|current\|delete` | 管理多环境 profile |

### url — URL 解析

离线、纯本地的 URL 解析工具，不发起任何网络调用。Skill 或 pipeline 解析飞书项目/Meegle URL 时优先走本命令，拿 `url_kind` 做分支，避免从原始路径猜字段。

| 命令 | 说明 |
|------|------|
| `url decode --url <URL>` | 将 URL 解析为 `url_kind` + `simple_name` / `work_item_type` / `work_item_id` / `view_id` / `chart_id` / `query` / `redirected_from` 等字段；未识别的 URL 返回 `url_kind: "unknown"`。 |

### 其他命令

| 命令 | 说明 |
|------|------|
| `inspect [command]` | 查看命令参数详情 |
| `completion bash\|zsh\|fish` | 生成 Shell 补全脚本 |
| `completion install` | 自动安装 Shell 补全 |

## 常用示例

### 查看待办

```bash
# 本周待办
meegle mywork todo --action this_week --page-num 1

# 已办事项
meegle mywork todo --action done --page-num 1

# 逾期事项
meegle mywork todo --action overdue --page-num 1
```

如果 `mywork todo` 返回 `get action info fail`，先刷新命令元数据：
`meegle --refresh mywork todo --action this_week --page-num 1`。如果账号同时属于多个工作区，
请显式传入工作区 key：
`meegle mywork todo --action this_week --page-num 1 --asset-key Asset_xxx`。

### 查询工作项

```bash
# 查看工作项概况
meegle workitem get --work-item-id 12345

# 查看工作项的节点详情
meegle workflow get-node --work-item-id 12345 --need-sub-task
```

### 批量读取工作项

`workitem +batch-get` 会对每个 ID 分别调用 `workitem get` 并把结果聚合成一条响应。
共享 flag（如 `--project-key`）会应用到每一次 per-item 调用上。命令以 `+` 开头，
表示它是客户端侧的场景/语法糖命令，CLI 在 `get` 之上组合出批量语义，没有对应的
单一后端接口。

```bash
# 在一次调用里传入逗号分隔的多个 ID
meegle workitem +batch-get --project-key PROJ --work-item-ids "12345,12346,12347"

# 从文件读取 ID（每行一个，以 '#' 开头的行视为注释）
meegle workitem +batch-get --project-key PROJ --ids-file ./ids.txt

# 每个 item 输出一行 JSON，summary 作为最后一行
meegle workitem +batch-get --project-key PROJ --work-item-ids "12345,12346" -o ndjson
```

响应结构（JSON）：

```json
{
  "summary": { "total": 3, "succeeded": 2, "failed": 1 },
  "results": [
    { "work_item_id": 12345, "data": { /* ... */ } },
    { "work_item_id": 12346, "data": { /* ... */ } },
    { "work_item_id": 12347, "error": { "code": "...", "message": "..." } }
  ]
}
```

约束：单次调用最多 200 个 ID，固定 3 worker 并发。部分失败**不会**中断整批——
请通过 `summary.failed` 或 per-item 的 `error` 字段判断；服务端返回 401 会终止整批。

### 创建工作项

```bash
# 通过 --params 传 fields[]
meegle workitem create --project-key PROJ --work-item-type story \
  --params '{"fields":[
    {"field_key":"name","field_value":"优化登录流程"},
    {"field_key":"priority","field_value":"P1"}
  ]}'

# 复杂字段值（数组、嵌套 JSON）也走 --params
meegle workitem create --project-key PROJ --work-item-type story \
  --params '{"fields":[
    {"field_key":"name","field_value":"排期任务"},
    {"field_key":"schedule","field_value":[1722182400000,1722355199999]}
  ]}'
```

### 修改字段

```bash
# 修改工作项名称
meegle workitem update --work-item-id 12345 \
  --params '{"fields":[{"field_key":"name","field_value":"新标题"}]}'

# 同时修改多个字段
meegle workitem update --work-item-id 12345 \
  --params '{"fields":[
    {"field_key":"name","field_value":"新标题"},
    {"field_key":"priority","field_value":"P0"}
  ]}'
```

### 附件上传 / 下载

`attachment` 域把飞书项目的两段式附件协议拆成两层暴露：

- **基础命令**（`attachment prepare-upload` / `attachment prepare-download`）
  返回带签名的 URL 预处理结果——适合自己写脚本做 HTTP 传输或调试分片计划。
- **快捷命令**（`attachment +upload` / `attachment +download`）把基础预处理与
  签名后的对象存储 HTTP POST/GET 在客户端组合成端到端流程。`+` 前缀表示这是
  场景命令——CLI 在客户端把预处理输出与字节传输串起来。

`--resource-type` 告诉后端这次上传/下载是给什么场景用：

| `--resource-type` | 用途 |
|-------------------|------|
| `15` | 工作项附件字段 |
| `16` | 工作项富文本字段里的图片 |
| `13` | 评论附件 |
| `14` | 评论正文里的图片 |

**上传作用域**：每次上传需要 `--work-item-id` 或 `--work-item-type` 之一。
**只要工作项已经存在，就优先用 `--work-item-id`**（update / comment 场景）；
只有创建时带附件这种"工作项还不存在"的场景才用 `--work-item-type`。两者都传时，
`--work-item-id` 胜出，`--work-item-type` 被静默丢弃。

```bash
# 为工作项附件字段上传文件 (resource-type 15)
meegle attachment +upload ./a.pdf \
  --resource-type 15 \
  --project-key PROJ --work-item-id 12345 --field-key files_field

# 创建时带附件场景 —— 工作项还不存在，用 --work-item-type
meegle attachment +upload ./a.pdf \
  --resource-type 15 \
  --project-key PROJ --work-item-type story --field-key files_field

# 为富文本字段上传图片 (resource-type 16)
meegle attachment +upload ./diagram.png \
  --resource-type 16 \
  --project-key PROJ --work-item-id 12345 --field-key spec_field

# 为评论上传附件 (resource-type 13)
meegle attachment +upload ./report.pdf \
  --resource-type 13 \
  --project-key PROJ --work-item-id 12345

# 为评论上传图片 (resource-type 14)
meegle attachment +upload ./screen.png \
  --resource-type 14 \
  --project-key PROJ --work-item-id 12345

# 下载：把其他命令响应里的 opaque file_url 直接传进来。
URL=$(meegle workitem get --project-key PROJ --work-item-id 12345 \
        --fields files_field --format json \
      | jq -r '.fields.files_field[0].url')
meegle attachment +download "$URL" \
  --project-key PROJ --work-item-id 12345 \
  --output ./local.pdf --overwrite
```

**完整性校验（`+download`）**：`+download` 会对下载的文件做一次额外的完整性校验，
若校验失败或无法校验则中止下载、不写任何文件。校验不通过时返回
`CLIENT_FILE_SIGN_MISMATCH` 错误（无法校验时为 `CLIENT_FILE_SIGN_UNVERIFIED`）；
两者都是临时问题，重试即可。

**自定义头 / 环境路由**：当前 profile 配置的自定义头会同时作用于下载 GET 与预处理调用，
从而用环境路由头把整条下载固定到同一环境。GET 前会剥掉 auth 相关头，
确保 token 不会发到对象存储所在的主机。

`+upload` 输出 JSON 对象，包含 file_token 与文件元信息：

```json
{
  "file_token": "...",
  "file_url": "https://...",
  "name": "a.pdf",
  "size": 12345,
  "mime_type": "application/pdf"
}
```

要把结果拼到下游命令里，用 `jq` 或脚本语言提取所需字段：

```bash
# 评论附件 —— comment add 直接接收 file_token
TOKEN=$(meegle attachment +upload ./report.pdf --resource-type 13 \
        --project-key PROJ --work-item-id 12345 | jq -r '.file_token')
meegle comment add --work-item-id 12345 --content "看附件" --file-token "$TOKEN"
```

**字段级回灌格式**（拼接 `--fields` 时参考）：

- **工作项附件字段** (`--resource-type 15`) —— `field_value` 是 JSON *字符串*，
  解析后是 `[{"name","type","size","fileToken"}]`。注意：`fileToken` 是驼峰（其他
  后端字段都是蛇形），`size` 是字符串不是数字。
- **富文本字段 / 评论图片** (`--resource-type 16` / `14`) —— 图片用
  `![name](file_url) <!-- file_token -->` 的 markdown 嵌入。
- **评论附件** (`--resource-type 13`) —— `comment add --file-token` 直接接收
  `file_token`。

### MQL 搜索

```bash
# 查询空间下的 P0 需求
meegle workitem query --project-key PROJ \
  --mql "SELECT \`name\`, \`priority\` FROM \`空间名\`.\`需求\` WHERE \`priority\` = 'P0'"
```

### 查看排期

```bash
# 查看团队成员排期
meegle workhour list-schedule --project-key PROJ \
  --start-time 2026-03-01 --end-time 2026-03-31 \
  --user-keys "张三,李四,王五"
```

### 查看用户信息

```bash
meegle user search --user-keys "张三,李四" --project-key PROJ
```

## 参数传递

### 基本 flag

每个命令的参数通过 `--flag-name` 传递：

```bash
meegle workitem get --work-item-id 12345 --project-key PROJ
```

### --set key=value（通用）

`--set` 是普通 flag 的**替代写法**，只影响**顶层参数**：`--set key=value` 等价于 `--key value`。适合在脚本里用统一的 key=value 语法，或通过 dot-path 写嵌套顶层对象。值自动类型推断（int / float / bool / string）。

```bash
# 下面两种等价：
meegle mywork todo --action this_week --page-num 1
meegle mywork todo --set action=this_week --set page_num=1

# dot-path 嵌套（Meegle 很少用到，但支持）：
--set extra.flag=true          # 变成 {"extra":{"flag":true}}
```

`--set` 只写**顶层参数**，**不会**写到工作项的 `fields[]`。写 `fields[]` 请用 `--params '{"fields":[...]}'`（见下）。

### --params JSON

`--params` 接收一个 JSON 对象，**每个顶层 key 都会作为 CLI flag 合并进来**。
顶层 key 必须是当前命令的合法 flag —— 它不是一份自由结构的载荷。

```bash
# 下面两条等价：
meegle workitem get --work-item-id 12345 --project-key PROJ
meegle workitem get --params '{"work_item_id":12345,"project_key":"PROJ"}'
```

什么时候用 `--params`：

- 值是嵌套对象或数组（`fields[]`、`schedule{}`），直接写 flag 太别扭
- 想一次性批量传多个参数，或者从文件加载载荷（见下方 `@file.json`）

```bash
meegle workitem create --project-key PROJ --work-item-type story \
  --params '{"fields":[{"field_key":"name","field_value":"标题"}]}'
```

#### 常见误用：不是所有名字都是顶层 flag

有些**看起来像**顶层字段的值，其实是工作项的字段值，必须包在 `fields[]`
里，而不是放在顶层。比如 `workitem update` 时，`priority` 属于工作项字段，
不是命令的 flag：

```bash
# ❌ "priority" 不是 workitem update 的合法 flag —— CLI 会在 stderr 输出 warning，后端忽略
meegle workitem update --work-item-id 12345 --params '{"priority":"P1"}'

# ✓ 字段值要包在 fields[] 里
meegle workitem update --work-item-id 12345 \
  --params '{"fields":[{"field_key":"priority","field_value":"P1"}]}'
```

不在工具声明 flag 列表里的顶层 key，在 `--dry-run` 输出里会以
`validation.unknown_params` 列出，运行时则会向 stderr 打一行
warning。它们仍然会被原样转发给后端（防止本地工具 schema 缓存陈旧
误判，需要时用 `--refresh` 刷新）。

通过 `meegle workitem meta-fields --project-key PK --work-item-type TK`
查询某种工作项类型下合法的 `field_key`。

#### 从文件读取（`@file.json`）

Windows 上内联 JSON 体验很糟：CMD 必须把 `"` 写成 `\"`，PowerShell 把转义反斜杠传给原生命令时还会再吞一层。给 `--params` 加 `@` 前缀即可改为从文件读取，macOS、Linux、Windows 各种 shell 都能一致工作：

```bash
# body.json:
# {"fields":[{"field_key":"name","field_value":"优化登录流程"}]}

meegle workitem create --project-key PROJ --work-item-type story \
  --params @body.json

# 绝对路径同样支持
meegle workitem update --work-item-id 12345 --params @/tmp/patch.json

# PowerShell 用法相同，无需任何转义
meegle workitem create --project-key PROJ --work-item-type story --params '@body.json'
```

读取走系统默认编码，相对路径与绝对路径都接受。文件不存在会返回 `PARAM_INVALID`；文件内容不是合法 JSON 会返回 `INVALID_PARAMS_JSON`。

### 优先级

当 `--set`、`--params` 和普通 flag 同时使用时：

1. 普通 CLI flag 覆盖 `--params` / `--set` 中同名顶层 key
2. `--set` 覆盖 `--params` 中同名顶层 key

### 数组参数

多个值用逗号分隔：

```bash
--user-keys "张三,李四,王五"
--field-keys "name,status,priority"
```

### 布尔参数

直接加 flag 即为 true，不加即为 false：

```bash
meegle workflow get-node --work-item-id 12345 --need-sub-task
```

## 全局 Flag

| Flag | 缩写 | 说明 |
|------|------|------|
| `--format` | `-o` | 输出格式：`json`（默认）、`table`、`ndjson`、`raw` |
| `--select` | | 字段投影（支持 dot path） |
| `--set` | | 设置嵌套参数（可重复） |
| `--params` | `-P` | 完整 JSON 参数体；以 `@` 开头改为从文件读取（如 `--params @body.json`） |
| `--dry-run` | | 只渲染请求，不实际执行 |
| `--envelope` | | 将成功输出包装为 `{data, meta, error}` —— 后端返回 logid 时挂在 `meta.logid` |
| `--verbose` | `-v` | 详细输出 |
| `--profile` | | 指定配置 profile |
| `--refresh` | | 从服务端刷新本地命令缓存（旁路 24 小时缓存） |

## 进阶用法

### 输出格式

```bash
# JSON（默认）
meegle workitem get --work-item-id 12345

# NDJSON（适合管道处理）
meegle mywork todo --action this_week --page-num 1 -o ndjson

# 表格
meegle mywork todo --action this_week --page-num 1 -o table
```

### `--select` 字段投影

`--select` 按 `.` 分段做字段投影；数组之后的下一段会对数组中每个元素广播（broadcast）剩余路径，并保留原来的嵌套结构。

| 表达式 | 响应 | 投影结果 |
|---|---|---|
| `list` | `{"list":[{"a":1}], "total":1}` | `{"list":[{"a":1}]}` |
| `list.a` | `{"list":[{"a":1,"b":2},{"a":3,"b":4}]}` | `{"list":[{"a":1},{"a":3}]}` |
| `list.a,list.b` | 同上 | `{"list":[{"a":1,"b":2},{"a":3,"b":4}]}`（多路径按下标合并） |
| `list.work_item_info.work_item_name` | `{"list":[{"work_item_info":{"work_item_name":"x"}}]}` | `{"list":[{"work_item_info":{"work_item_name":"x"}}]}` |
| `nodes.0` | `{"nodes":[{"id":"a"},{"id":"b"}]}` | `{"nodes":{"0":{"id":"a"}}}`（数字段按索引） |

```bash
# 顶层选择
meegle workitem get --work-item-id 12345 --select "id,name,status"

# 跨数组广播 —— 从嵌套记录里提取字段
meegle mywork todo --action done --page-num 1 \
  --select "list.work_item_info.work_item_name,list.state_info.end_state_key_name"

# 顶层元数据 + 广播混合 —— total 和投影后的 list 记录会同时保留
meegle mywork todo --action done --page-num 1 \
  --select "total,list.work_item_info.work_item_name"
```

### 元数据保留

默认渲染下，响应的完整结构在所有 `--format` 中都会保留：列表接口返回的 `{"list":[...], "total":N, "pagination":{...}}` 原样呈现 —— 即使你没有在 `--select` 中点名，`total` / `pagination` 这些元数据字段依然可见。要钻到具体记录就显式用 `--select`（配合上面的广播语法）。`--format table` 和 `--format ndjson` 下，形如 `{"list":[...]}` 的单键包装（没有兄弟元数据）仍然会被无损展开成行展示。

### 通过 `--envelope` 拿到 logid

遇到"返回 success 但实际没生效"、"输出不符合预期"等情况，需要让 oncall
按 logid 反查具体调用时，加上 `--envelope`：

```bash
meegle workflow update-node --work-item-id 12345 \
  --set node_schedule.points=10 --envelope
```

成功输出会被包装为 `{data, meta, error}`，后端返回 logid 时挂在
`meta.logid` 里——把这个 id 交给 oncall 就能在 argos 定位到这次请求。
不加 `--envelope` 时 logid 会被抑制，保持默认输出干净便于管道处理。

### Dry Run

有副作用的命令先用 `--dry-run` 预览请求再执行：

```bash
meegle workitem create --project-key PROJ --work-item-type story \
  --params '{"fields":[{"field_key":"name","field_value":"测试"}]}' --dry-run
```

### 命令内省

用 `inspect` 查看任意命令的完整参数信息：

```bash
# 列出所有命令
meegle inspect

# 查看特定命令的参数详情
meegle inspect workitem.create
```

## 认证

### 浏览器登录（默认）

```bash
meegle auth login
```

自动打开浏览器完成 OAuth 授权。如果浏览器未打开，终端会显示授权地址，手动复制即可。

### Device Code 登录（无浏览器环境）

```bash
meegle auth login --device-code
```

终端显示二维码和授权码，使用手机扫码授权。适用于 SSH 远程服务器等无浏览器环境。

### 其他认证命令

```bash
# 查看登录状态（会向服务端发一次轻量 tools/list 调用校验 token，可作为
# cron 任务的 preflight）
meegle auth status

# 登出
meegle auth logout
```

`auth status` 退出码与 `reason` 字段配合，让 cron / CI preflight 脚本
不用解析人类可读文本即可分支处理：

| 退出码 | `reason` | 含义 | 建议动作 |
|--------|----------|------|----------|
| 0      | —        | 本地有 token 且服务端校验通过 | 继续执行 |
| 1      | `no local token` | 本地无 token | 执行 `meegle auth login` |
| 1      | `token rejected by server` | token 已过期或被撤销（refresh 也救不回来） | 执行 `meegle auth login` |
| 2      | `server unreachable: <err>` | 网络、超时、5xx —— 调用本身失败 | 稍后重试，不要重新登录 |

JSON 输出示例（`auth status --format json`），token 被拒：

```json
{"authenticated": false, "host": "meegle.com", "reason": "token rejected by server"}
```

## 配置

### 配置文件

配置存储在 `~/.meegle/config.json`：

```bash
# 初始化配置
meegle config init

# 查看当前配置
meegle config show

# 修改单个配置项
meegle config set host project.feishu.cn

# 查看单个配置项
meegle config get host
```

主要配置项：

| 字段 | 说明 | 示例 |
|------|------|------|
| `host` | 站点域名 | `project.feishu.cn`、`meegle.com` |
| `user_access_token` | 用户访问令牌，支持 `${VAR}` 从环境变量读取 | `${CI_MEEGLE_TOKEN}` |
| `access_token_header` | 自定义承载 token 的 HTTP header 名；置空则用默认的 `Authorization: Bearer <token>` | `x-meegle-auth` |
| `user_agent` | 追加到 `User-Agent` 的 caller 段（形如 `meegle-cli/<ver> <user_agent>`）；支持 `${VAR}` 模板；被环境变量 `MEEGLE_USER_AGENT` 覆盖 | `my-service/1.0` |

### 沙盒 / CI：直接注入环境变量

`MEEGLE_HOST` 和 `MEEGLE_USER_ACCESS_TOKEN` 两个约定名环境变量会在 CLI 启动时被直接读取，覆盖 profile 中的同名字段，无需任何 `config set`：

```bash
export MEEGLE_HOST=project.feishu.cn
export MEEGLE_USER_ACCESS_TOKEN=<your-user-token>
export MEEGLE_USER_AGENT=ci-runner  # 可选；追加到 User-Agent，优先级高于 config.user_agent
meegle workitem get --work-item-id 123
```

任一变量可以单独设置。当 `MEEGLE_USER_ACCESS_TOKEN` 设置时，CLI 不访问 keychain，401 错误不会自动 refresh，由调用方自行轮转。仅设置 `MEEGLE_HOST`（不带 token）时仍走 keychain 中存储的凭证。

### 自定义 Auth Header

默认情况下 token 通过标准 `Authorization: Bearer <token>` 发送。若后端要求把 token 放到别的 header 里（且不允许带 `Authorization`），用 `access_token_header` 切换：

```bash
meegle config set access_token_header x-meegle-auth
```

或通过环境变量临时覆盖：

```bash
export MEEGLE_ACCESS_TOKEN_HEADER=x-meegle-auth
```

启用后 CLI 会以 `<header>: <token>` 发送裸值（无 `Bearer ` 前缀），并且**完全不发送** `Authorization` header —— 适用于对两种 header 共存敏感的后端。

### 环境变量模板

如果平台使用的环境变量名不是 `MEEGLE_*`，可以在 config.json 中用 `${VAR}` 占位符绑定任意字符串字段，运行时会用同名环境变量的值替换。这样既能避免在 config.json 里明文写敏感值，也能适配不同容器/CI 平台各自的变量命名习惯。

```json
{
  "current": "prod",
  "profiles": {
    "prod":    { "host": "project.feishu.cn", "user_access_token": "${PROD_CI_TOKEN}" },
    "staging": { "host": "staging.feishu.cn", "user_access_token": "${STAGING_CI_TOKEN}" }
  }
}
```

规则：
- 仅识别**整串形态**的占位符。`"${X}"` 会被展开；`"Bearer ${X}"` 按字面量处理，不展开。
- 引用的环境变量未设置或为空时 CLI **fail fast**，错误信息会带上字段路径和变量名。
- 当配置了 `user_access_token` 时，它会覆盖 `meegle auth login` 在本地 keychain 里写入的令牌。由于这种模式下没有本地 refresh 能力，服务端返回 401 时需要自行轮转环境变量值。

### 多环境 Profile

支持管理多个环境配置（不同站点、不同账号），每个 profile 独立存储 host 和认证信息。

```bash
# 创建新环境（交互式引导 host 选择 + 登录）
meegle config profile create staging

# 查看所有环境
meegle config profile list

# 切换默认环境
meegle config profile use staging

# 查看当前环境
meegle config profile current

# 临时使用其他环境（不改变默认）
meegle mywork todo --action this_week --page-num 1 --profile staging

# 删除环境
meegle config profile delete staging
```

## 常见问题

### 命令列表为空

CLI 启动时会从服务端获取可用命令列表。如果网络不通或未登录，动态命令不会注册。请先确保已登录：

```bash
meegle auth login
```

命令列表会自动缓存，过期后在后台静默刷新，不影响使用。
当服务端命令发现失败且没有可用缓存时，`auth`、`config`、`inspect`、`completion`、`url` 等本地命令仍会正常启动；动态业务命令会返回 `TOOL_DISCOVERY_FAILED` 服务端错误，直到网络恢复。

## 安全与风险提示

本工具被设计为可由 AI Agent 调用来自动化 Meegle 操作，这本身就带有模型幻觉、执行不可控、提示词注入等固有风险。一旦你授予了 Meegle 访问权限，Agent 就会在授权范围内以你的用户身份行事，可能完成字段更新、状态流转、工作项创建等高影响操作，使用前请充分评估。

推荐的风险控制手段：

- 有副作用的命令先用 `--dry-run` 预览请求再正式执行
- 给 Agent 专用 profile（`meegle config profile create`），便于单独审计和吊销
- CI 或共享环境优先使用短期环境变量 token（`MEEGLE_USER_ACCESS_TOKEN`），401 时自行轮换；不要放松默认安全设置

使用本工具即视为你自愿承担由此带来的全部相关责任。

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=larksuite/meegle-cli&type=Date)](https://star-history.com/#larksuite/meegle-cli&Date)

## 贡献

欢迎社区贡献。Bug 反馈或功能建议请提 [Issue](https://github.com/larksuite/meegle-cli/issues) 或 [Pull Request](https://github.com/larksuite/meegle-cli/pulls)。对于较大改动，建议先通过 Issue 讨论。

## License

本项目基于 **MIT 许可证** 开源。

该软件运行时会调用 Lark/飞书开放平台的 API，使用这些 API 需要遵守如下协议和隐私政策：

- [飞书用户服务协议](https://www.feishu.cn/terms)
- [飞书隐私政策](https://www.feishu.cn/privacy)
- [飞书开放平台应用服务商安全管理规范](https://open.feishu.cn/document/uAjLw4CM/uMzNwEjLzcDMx4yM3ATM/management-practice/app-service-provider-security-management-specifications)
- [Lark User Terms of Service](https://www.larksuite.com/user-terms-of-service)
- [Lark Privacy Policy](https://www.larksuite.com/privacy-policy)
