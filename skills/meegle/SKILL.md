---
name: meegle
description: |
  飞书项目（Meego/Meegle）操作工具。支持查询和管理工作项、节点流转、视图查询、个人待办、排期统计等功能。 Use when user needs to work with Feishu/Lark Meego project management — including querying work items, creating/updating work items, completing workflow nodes, checking views, listing todos, analyzing schedules/workloads, or searching with MQL. 关键词：飞书项目、meego、meegle、工作项、需求、任务、缺陷、排期、视图、待办、节点。
---

# 飞书项目 (Meego/Meegle) 操作指南

本技能通过 Meegle CLI来操作飞书项目数据。输出语言跟随用户输入语言，默认中文。

> 各命令的调用示例见 [references/api-examples.md](references/api-examples.md)。
> **授权流程**（所有业务命令前必须执行）：见 [references/auth-guard.md](references/auth-guard.md)
> **CLI 使用指南**（命令结构、参数传递、命令发现）：见 [references/cli-guide.md](references/cli-guide.md)

---

## Project 空间域

### project search
搜索空间信息，将空间名转换为 project_key 或验证空间是否存在；省略 --project-key 时返回当前用户最近访问过的空间列表（按访问时间由近及远）。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 否 | 空间 projectKey、simpleName 或空间名称；留空查询当前用户可访问的空间 |
| --page-num | number | 否 | 分页页码，每页 50 条，从 1 开始 |

---

## WorkItem 工作项域

> 元数据查询命令（`workitem meta-types` / `workitem meta-fields` / `workitem meta-roles` / `workitem meta-create-fields`）的参数表见 [references/workitem.md](references/workitem.md)。

### workitem create
创建工作项实例。**务必先用 `workitem meta-fields` 获取字段信息，`workitem meta-roles` 获取角色信息。模板 ID 是必填项。**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-type | string | 是 | 工作项类型 |
| --project-key | string | 否 | 空间标识 |
| --fields | array | 否 | 字段值列表，每项含 field_key 和 field_value |
| --work-item-id | string | 否 | 工作项资源库模板实例 ID；通过资源工作项创建普通工作项时必填 |
| --ignore-required | boolean | 否 | 是否忽略字段必填校验；默认 false，谨慎使用 |
| --ignore-role-calculate | boolean | 否 | 是否忽略角色计算；默认 false，谨慎使用 |

### workitem get
按 ID/名称查询工作项概况。不传 fields 时返回固定基础字段加上一组默认带出的系统字段——实测包含 `group_type` 拉群方式、`description`、`current_status_operator`、`watchers`（即便 value 为 null 也会出现）；其余字段需要通过 `workitem meta-fields` 拿到 key 后再传入 fields。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-id | string | 是 | 工作项 ID 或名称 |
| --project-key | string | 否 | 空间 key |
| --fields | array | 否 | 要查询的 field_key 或 field_name；传 `["_all"]` 时按逻辑字段分页返回全部字段；传 `["group_type"]` 时只取拉群方式 |
| --page-size | number | 否 | 仅 `fields=["_all"]` 时生效；每页字段数量，默认 100，最大 200。**meegle CLI 注意**：直接传 `--page-size N` 会被序列化成字符串触发后端 `need I64 type, but got: STRING`；当前只能走 `--params '{"page_size":N}'` 让它以数字传出 |
| --page-token | string | 否 | 仅 `fields=["_all"]` 时生效；翻页 token，首次不传，下一页传上次响应的 `next_page_token`（token 形如字段 key，例如 `"business"`）；同上，meegle CLI 当前需要走 `--params '{"page_token":"..."}'` |

> **逻辑字段聚合（重要心智模型）**：服务端把 `group_id` / `chat_group` 这类"拉群"相关的物理字段**合并**到一个逻辑字段 `group_type`。读取/更新统一走 `group_type`，**不要再单独读取 `group_id` 或 `chat_group`**。
>
> ⚠️ **读写协议不对称**：读返回结构里**判别键是 `value`**（不是 `type`），更新时**判别键是 `type`**——别照着读到的结构直接回写。
>
> 读返回（`workitem_fields[].value` 字段）的形状：
> - `auto` → `{value: "auto", label: "自动拉群", group_id: "oc_xxx"}`（自动拉群通常有 group_id；状态切换时 oc_id 可能会变）
> - `bind` → `{value: "bind", label: "绑定现有群", group_id: "oc_xxx"}`
> - `disabled` → `{value: "disabled", label: "不拉群"}`（无 group_id）
>
> 写协议（`field_value` 里的 JSON）：`{"type": "auto" | "bind" | "disabled", "group_id": "oc_xxx"}`

### workitem batch-get
批量查询工作项（Meegle CLI 客户端 fan-out：并发调用 `workitem get`）。单次 ≤ 200 个 ID，3 并发，返回 `{results, errors, summary}`；ID 量大时用 `--format ndjson` 流式输出。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-ids | array | 二选一 | 工作项 ID 列表（逗号分隔或多次传入） |
| --ids-file | string | 二选一 | 从文件读取 ID（一行一个，`#` 开头注释） |
| --fields | array | 否 | 要查询的 field_key 列表 |
| --project-key | string | 否 | 空间 key |

### workitem update
修改指定实例的字段值或角色。节点字段更新请用 `workflow update-node`。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-id | string | 是 | 工作项 ID 或名称 |
| --project-key | string | 否 | 空间 key |
| --fields | array | 否 | 要更新的字段列表，每项含 field_key 和 field_value |
| --role-operate | array | 否 | 角色操作，每项含 op(add/remove)、role_key、user_keys |

**角色更新**：不能通过 fields 更新角色，必须用 `role_operate`。role_key 通过 `workitem meta-roles` 获取，user_keys 通过 `user search` 获取。

**拉群方式更新（`group_type` 逻辑字段）**：要修改/读取拉群方式统一走 `group_type`，不要再单独操作 `group_id` / `chat_group`。写协议 `field_value` 形如：`{"type": "auto" | "bind" | "disabled", "group_id": "oc_xxx"}`（注意写用 `type` 作为判别键，**与读返回的 `value` 不对称**）。校验规则（服务端实际报错文本）：`bind` 不带 `group_id` 或带空串/纯空格 → `group_id is required when group_type=bind`；`auto`/`disabled` 同时带 `group_id` → `group_type conflicts with group_id: type=<auto|disabled>`。详细示例见 [references/sop-update-workitem.md](references/sop-update-workitem.md)。

### workitem query
使用 MQL 查询工作项数据。语法详见 [references/mql-syntax.md](references/mql-syntax.md)。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间标识（支持名称、simpleName、projectKey） |
| --mql | string | 是（翻页时可用 session_id 替代） | MQL 查询语句（完整 SQL） |
| --session-id | string | 否 | 分页会话 ID，传入后不解析 MQL 直接翻页 |
| --group-pagination-list | array | 否 | 分组分页信息，首次查询可不传；翻页时传 `[{ "group_id": "分组ID", "page_num": 页码 }]` |

**分组分页**：
- `--group-pagination-list` 是数组，当前只支持传一组分页数据；元素结构为 `{ "group_id": string, "page_num": number }`
- `group_id` 取首查返回的 `list[].group_infos[].group_id`；无分组查询返回的默认分组 ID 为 `"1"`，翻页时也传 `"1"`
- `page_num` 从 1 开始；MQL 首查不传分页参数时默认返回第一页，单页最多 50 条。当前接口没有 `page_size` / `page_token` 子字段
- 翻页时传首查返回的 `session_id` 和目标分组的分页参数；传 `session_id` 后后端不再解析 MQL，只按已有会话取对应分组页

**要点**：
- 先用 `workitem meta-fields` / `workitem meta-roles` 获取字段与角色配置；查不到直接报错不要继续
- SELECT 后属性不宜过多，**优先使用字段 key**（如 `name`、`priority`、`status`）；返回按页返回，需全量时使用翻页参数

### workitem list-op-records
查看工作项操作记录。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-id | string | 是 | 工作项 ID |

---

## Attachment 附件域

附件上传/下载分两步：先调 `attachment prepare-upload` / `attachment prepare-download` 申请带签名的对象存储 URL，再与对象存储做 HTTP 直连。Meegle CLI 提供 `attachment +upload` / `attachment +download` 一键封装。详细参数表与流程说明见 [references/attachment.md](references/attachment.md)。

---

## WorkFlow 工作流域

> 流转辅助命令（`workflow list-state-transitions` / `workflow list-state-required` / `workflow meta-node-fields`）的参数表见 [references/workflow.md](references/workflow.md)。

### workflow transition
仅用于节点流工作项，操作节点完成流转或回滚。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-id | string | 是 | 工作项 ID |
| --action | string | 否 | confirm（流转） / rollback（回滚） |
| --node-id | string | 否 | 节点 ID |
| --node-ids | array | 否 | 节点名称或节点 ID 列表 |
| --rollback-reason | string | 否 | 回滚原因，action=rollback 时需填写 |
| --project-key | string | 否 | 空间 key |

### workflow transition-state
仅用于状态流工作项，流转工作项状态。先用 `workflow list-state-transitions` 获取可流转状态及 transition_id。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-id | string | 是 | 工作项 ID |
| --transition-id | string | 否 | 状态流转 ID，从 `workflow list-state-transitions` 获取 |
| --project-key | string | 否 | 空间 key |

### workflow get-node
获取工作项中指定节点或所有节点的完整详情。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-id | string | 是 | 工作项 ID 或名称 |
| --node-id-list | array | 否 | 节点 ID 列表，传空或 `_all` 获取所有节点 |
| --field-key-list | array | 否 | 节点字段 key，传空或 `_all` 获取所有字段 |
| --need-sub-task | boolean | 否 | 是否需要节点子项（子任务） |
| --page-num | number | 否 | 节点信息一次最多 20 个，按页返回 |
| --project-key | string | 否 | 空间 key |

### workflow update-node
修改节点（排期、负责人、自定义字段等）。排期/差异化排期/负责人不要同时修改，需分多次调用。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-id | string | 是 | 工作项 ID |
| --node-id | string | 是 | 节点 ID（node_key） |
| --node-owners | array | 否 | 节点负责人 userkey 数组；清空传空数组 `[]` |
| --node-schedule | object | 否 | 节点排期，格式 `{"estimate_start_date":ms,"estimate_end_date":ms,"owners":[userkey],"points":数字}`；清空传 `{}`；不变更则不传 |
| --schedules | array | 否 | 按人差异化排期，每项细化到单个人的排期；清空某人则 `estimate_start_date`/`estimate_end_date` 传 null |
| --fields | array | 否 | 节点自定义字段，每项含 `field_key` 和 `field_value`（STRING 协议，见「字段值格式」） |
| --project-key | string | 否 | 空间 key |

---

## MyWork 工作台域

### mywork todo
按 action 类型查询当前用户的工作项列表。无需 MQL 即可查询待办/已办。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --action | string | 是 | todo(待办)/done(已办)/overdue(逾期)/this_week(本周待办) |
| --page-num | number | 是 | 页码，从 1 开始，每页 50 条 |
| --asset-key | string | 否 | 工作区 key（格式 Asset_xxx），仅在报错需要选择时传 |

需完整结果时，从 page_num=1 连续翻页直到空为止。

---

## WorkHour 工时域

> 工时记录查询（`workhour list-records`）的参数表见 [references/misc.md](references/misc.md)。

### workhour list-schedule
获取指定人员在时间区间内的排期与工作量明细。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --user-keys | array | 是 | 用户标识（名称/邮箱/userkey），**每次最多 20 个** |
| --start-time | string | 是 | 开始时间，格式 YYYY-MM-DD |
| --end-time | string | 是 | 结束时间，格式 YYYY-MM-DD，**单次跨度最大 3 个月** |
| --work-item-type-keys | array | 否 | 工作项类型列表，查询所有传入 `_all` |

**调用约束**：每次最多 20 人（多人拆批次并行）；单次跨度 ≤ 3 个月（超出按月拆分）；所有批次完成后再汇总，未完整获取前不得输出结论。

---

## UserGroup 人员域

> 团队相关命令（`team list` / `team list-members`）的参数表见 [references/misc.md](references/misc.md)。

### user search
批量查询用户基础信息。用于将姓名/邮箱转换为 userkey。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --user-keys | array | 是 | userKey、Email 或名字，最多 20 个 |
| --project-key | string | 否 | 空间 key |
| --need-all-status | boolean | 否 | 是否返回所有状态用户；默认 false，仅返回在职用户 |

### user me
查看当前用户信息。无需参数。

> **MQL 中**可直接用 `current_login_user()` 函数，无需提前获取用户信息。如需获取当前用户的 userkey/姓名等详细信息，可用 `user search` 传入 `current_login_user()` 作为参数。

---

## View 视图域

> 视图搜索与固定视图管理（`view search` / `view create-fixed` / `view update-fixed`）的参数表见 [references/view.md](references/view.md)。

### view get
根据视图 ID 获取该视图下的工作项列表。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --view-id | string | 是 | 视图 ID |
| --project-key | string | 否 | 空间 key |
| --page-num | number | 否 | 分页页数起点 |
| --fields | array | 否 | 要查询的字段 |

---

## Comment 评论域

> 评论列表查询（`comment list`）的参数表见 [references/misc.md](references/misc.md)。

### comment add
添加评论。支持富文本 Markdown，语法详见 [references/rich-text-editor-markdown-syntax.md](references/rich-text-editor-markdown-syntax.md)（含 @提及、对齐、链接预览、字号/颜色等扩展语法）。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-id | string | 是 | 工作项 ID |
| --content | string | 是 | 评论内容 |

---

## Deliverable 交付物域

> 单命令小域，参数表见 [references/misc.md](references/misc.md)。

### deliverable list
查看交付物详情及其根工作项 / 来源工作项。可按工作项 ID 列表过滤。

---

## Resource 资源库

> 资源库（资源模板）管理。`resource create` 当前对应 MCP `create_resource_work_item`，用于创建资源实例；查看资源库的字段 / 角色配置用 `resource meta-fields`。详细参数表见 [references/misc.md](references/misc.md)。

### resource create
在已启用资源库的工作项类型下创建资源模板（资源实例）。创建前先调 `resource meta-fields` 取字段 / 角色配置。

**语义边界**：
- 创建资源实例：`work_item_type_key` 是资源库启用的工作项类型；`template_id` 是该类型下的流程模板 ID/名称；`fields` / `roles` 是新资源实例自身的字段和角色。
- 从资源实例创建普通工作项：不要把已有资源实例 ID 填到 `work_item_type_key` 或 `template_id`。必须以当前 `resource create` 的 inspect/schema 为准确认是否有源资源实例参数；若当前 schema 未暴露该参数，先向用户说明无法确认自动化参数，不要猜。

---

## WBS 计划表

> 计划表（WBS）有 **草稿（draft）** 与 **已发布实例（instance）** 两套数据模型。常见编辑流程：`wbs create-draft` → 多次 `wbs edit-draft` → `wbs publish-draft`；放弃改动用 `wbs reset-draft`。详细参数表与 `wbs edit-draft` 的 operation 子类型见 [references/wbs.md](references/wbs.md)。

### wbs list-draft-rows
在计划表草稿中按条件筛选行。常用筛选字段：`wbs_name`、`wbs_parent_id`、`wbs_owner_in_charge`、`wbs_states_doing`。详见 [references/wbs.md](references/wbs.md)。

### wbs list-instance-rows
在已发布的线上计划表实例中按条件筛选行。参数同 `wbs list-draft-rows`。

### wbs edit-draft
对计划表草稿执行一次原子编辑。一次调用只能传一个 `operation_type`，但该操作内部可通过 `items` / `uuids` 等数组承载单行或批量编辑；支持新增 / 删除 / 恢复 / 排序 / 改名 / 改负责人 / 改阶段 / 改排期 / 改估分 / 改实际工时等，结构见 [references/wbs.md](references/wbs.md)。
> ⚠️ **前置**：草稿不存在时先调 `wbs create-draft`，再调 `wbs edit-draft`。判断方法：直接 `wbs list-draft-rows` 报"草稿不存在"类错误即视为缺失草稿。

### wbs publish-draft
将编辑完成的草稿发布到线上。
> ⚠️ 全量发布前必须用**固定话术**二次确认："本人及协同者的全部编辑内容均会被发布，请确认是否全量发布？"；部分发布（传入 `uuid_strings_list`）无需二次确认。

---

## 其它低频域

度量图表、子任务、关系定义查询的命令参数表见 [references/misc.md](references/misc.md)：

- **Chart 度量域** — `chart get` / `chart list`
- **SubTask 子任务域** — `subtask update`（create/update/confirm/rollback）
- **Relation 关系域** — `relation list` / `relation meta-definitions`
- **WBS 计划表 · 辅助命令** — `wbs create-draft` / `wbs reset-draft` / `wbs get-draft-progress` / `wbs list-element-templates`（见 [references/wbs.md](references/wbs.md)）

---

## 字段值格式（field_value）

> 🚨 **STRING 协议**：`field_value` 协议层固定为字符串。标量（text/number/bool/option_id/userkey/毫秒）直接作字符串；数组、对象**必须先 JSON.stringify** 再传，直接传会报 `need STRING type, but got: LIST` / `MAP`。
> 例：multi-user 正确写法为 `"[\"7509072868295085608\"]"`，错误写法为 `["7509072868295085608"]`。

| 字段类型 | 语义 | field_value 传参（已按上述约定序列化） |
|---------|------|------|
| template | 模板 ID（**创建必填**） | `"145405865"` — 用 `workitem meta-fields(field_keys=["template"])` 获取 |
| text / multi-pure-text / link / bool / number | 单个字面值 | `"测试工作项"` / `"100"` / `"true"` |
| user | 单个 userkey | `"7509072868295085608"` |
| multi-user | userkey 数组（**stringified**） | `"[\"7509072868295085608\",\"7509072868295085609\"]"` |
| select / radio / tree-select | 枚举项 option_id | `"437794"` |
| multi-select | option_id 对象数组（**stringified**） | `"[{\"option_id\":\"111\"},{\"option_id\":\"222\"}]"` |
| tree-multi-select | option_id 字符串数组（**stringified**） | `"[\"id1\",\"id2\"]"` |
| multi-text | 富文本 Markdown 字符串（语法详见 [references/rich-text-editor-markdown-syntax.md](references/rich-text-editor-markdown-syntax.md)） | `"**加粗**内容"` |
| date | 毫秒时间戳（天精度） | `"1722182400000"` |
| schedule | `[开始ms, 结束ms]`（**stringified**） | `"[1722182400000,1722355199999]"` |
| precise_date | 对象（**stringified**） | `"{\"start_time\":1722182400000,\"end_time\":1722355199999}"` |
| workitem_related_select | 关联工作项 ID | `"145405865"` |
| workitem_related_multi_select | ID 数组（**stringified**，数字元素） | `"[145405865,145405866]"` |
| role_owners（仅创建时） | 角色-人员对象数组（**stringified**） | `"[{\"role\":\"RD\",\"owners\":[\"userkey1\"]}]"` |
| signal | 纯字符串 | `"true"` / `"false"` / `"null"` |

> 更新角色时不用 fields，用 `workitem update` 的 `role_operate` 参数。

### 关联工作项字段（workitem_related_*）

用户提供名称而非 ID 时，需按名称→ID 转换流程（搜目标空间+类型，消歧，写入格式，防循环引用）：详见 [references/field-value-extras.md](references/field-value-extras.md)。

---

## 常用场景速查

| 场景 | 命令（注意点） |
|------|-------|
| 空间名 → project_key | `project search` |
| 查类型 / 字段 / 角色 | `workitem meta-types` / `workitem meta-fields` / `workitem meta-roles` |
| 人名 → userkey | `user search`（批量 ≤20） |
| 当前用户 | `user me`；MQL 内可直接 `current_login_user()` |
| 条件查询 / 个人待办 | `workitem query`（MQL） / `mywork todo` |
| 团队排期 | `workhour list-schedule`（≤20 人、≤3 月） |
| 创建 / 修改工作项 | `workitem create` / `workitem update`（字段 fields，角色 role_operate） |
| 节点流转 / 状态流转 | `workflow transition`（confirm/rollback） / `workflow transition-state`（先 `workflow list-state-transitions`） |
| 视图数据 | `view get` |


## 通用规范

### 请求处理流程

收到用户输入后依次执行：

1. **参数提取**：从自然语言中提取空间名、工作项类型、时间、人员、筛选条件；含 URL 时先调 `url decode` 解析，按 [references/url-kinds.md](references/url-kinds.md) 的 `url_kind` 分支决定进入哪个 SOP 或拒绝。**禁止**自己从 URL 截取路径段作参数。注意区分空间名与筛选维度（如「XX空间下YY业务线的缺陷」中 XX 才是空间名）。

2. **参数确认**（禁止猜测）：用探测命令校验空间（`project search`）、类型（`workitem meta-types`）、人员（`user search`）。**探测结果不唯一时必须展示并询问用户**，禁止自行选择；缺失必填合并为一条消息询问。个人待办（`mywork todo`）可跳过；URL 经 `url decode` 拿到 `simple_name` 后仍需 `project search` 转权威 `project_key`（同名空间可能有多个无权限）。

3. **元数据收集**（无需用户参与）：调用 `workitem meta-fields` 获取字段定义（需要特定字段用 `field_keys`，模糊查询用 `field_query`）；涉及角色时并行调 `workitem meta-roles`。关键字段识别：状态字段 type=`_work_item_status`（含「完成/关闭/终止」的值为完成态）、排期字段 type=`schedule`（MQL 用 `__字段名_开始时间` / `__字段名_结束时间`）、优先级字段 key=`priority`。简单直调场景（仅需 project_key + work_item_id，如 `comment add`）可跳过本步。

4. **执行**：调用目标命令，遵循 [references/performance.md](references/performance.md) 的并行/翻页规则。

### 并行与大结果

详见 [references/performance.md](references/performance.md)：并行调用（必须串行的链路、可并行的组合）、大结果分批与翻页规则。

### 错误处理

**总则**：失败后从返回的 `err_msg` / `inner_err` 中提取错误原因，针对性修正后重试；**最多自动重试 2 次**，连续 3 次同类失败后停止并向用户说明。

**熔断条件**（立即终止，禁止盲目重试）：
- 空间未找到（`project search` 连续 3 次失败）
- Permission Denied（当前用户对该空间无访问权限）

详细自愈规则与错误速查表（涵盖字段格式、节点流转、人员转换等常见报错）见 [references/error-handling.md](references/error-handling.md)。

---

## 操作指南（SOP）

具体操作的完整流程、字段转换和自愈机制见对应 SOP：

- [创建工作项](references/sop-create-workitem.md) — 创建需求、任务、缺陷
- [更新工作项](references/sop-update-workitem.md) — 修改字段、更新角色、追加内容
- [流转节点（节点流）](references/sop-transition-node.md) — 完成/回滚节点、批量流转
- [流转状态（状态流）](references/sop-transition-state.md) — 流转缺陷/issue、关闭 bug
