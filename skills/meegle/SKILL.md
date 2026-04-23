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
搜索空间信息，将空间名转换为 project_key 或验证空间是否存在。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 projectKey、simpleName 或空间名称 |

---

## WorkItem 工作项域

### workitem create
创建工作项实例。**务必先用 `workitem meta-fields` 获取字段信息，`workitem meta-roles` 获取角色信息。模板 ID 是必填项。**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-type | string | 是 | 工作项类型 |
| --project-key | string | 否 | 空间标识 |
| --fields | array | 否 | 字段值列表，每项含 field_key 和 field_value |

### workitem get
按 ID/名称查询工作项概况。不传 fields 时仅返回固定基础字段；如需自定义字段数据，先调 `workitem meta-fields` 获取字段 key 后传入 fields。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-id | string | 是 | 工作项 ID 或名称 |
| --project-key | string | 否 | 空间 key |
| --fields | array | 否 | 要查询的 field_key 或 field_name |

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

### workitem query
使用 MQL 查询工作项数据。语法详见 [references/mql-syntax.md](references/mql-syntax.md)。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间标识（支持名称、simpleName、projectKey） |
| --mql | string | 是（翻页时可用 session_id 替代） | MQL 查询语句（完整 SQL） |
| --session-id | string | 否 | 分页会话 ID，传入后不解析 MQL 直接翻页 |
| --group-pagination-list | array | 否 | 分页信息，首次查询可不传 |

**要点**：
- 先用 `workitem meta-fields` / `workitem meta-roles` 获取字段与角色配置；查不到直接报错不要继续
- SELECT 后属性不宜过多，**优先使用字段 key**（如 `name`、`priority`、`status`）；返回按页返回，需全量时使用翻页参数

### workitem list-op-records
查看工作项操作记录。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-id | string | 是 | 工作项 ID |

### workitem meta-types
获取指定空间下所有工作项类型列表。用户描述模糊时用此命令确认合法 type_key。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 projectKey |

### workitem meta-fields
获取指定空间和工作项类型的可用字段配置（不含禁用字段和角色配置）。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-type | string | 是 | 工作项类型 key 或名称 |
| --page-num | number | 是 | 页数，每页 50 条，从 1 开始 |
| --field-keys | array | 否 | 精确匹配字段 key 或名称 |
| --field-query | string | 否 | 模糊查询字段 key 和名称 |
| --field-types | array | 否 | 按字段类型筛选 |

### workitem meta-roles
获取指定工作项类型的角色列表。用于查询/创建/更新工作项前确认合法 role_key。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-type | string | 是 | 工作项类型 key 或名称 |
| --page-num | number | 是 | 页数，每页 50 条，从 1 开始 |
| --role-keys | array | 否 | 精确匹配角色 key 或名称 |
| --role-query | string | 否 | 模糊查询角色 key 和名称 |

### workitem meta-create-fields
查看创建工作项时可用的字段及类型。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |

---

## WorkFlow 工作流域

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

### workflow list-state-transitions
查看工作项可流转的状态列表。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-id | string | 是 | 工作项 ID |
| --work-item-type | string | 是 | 工作项类型 |
| --user-key | string | 是 | 用户标识 |

### workflow list-state-required
查看流转所需的必填信息（节点流传 node_key，状态流传 state_key）。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-id | string | 是 | 工作项 ID |
| --state-key | string | 是 | 节点流的 node_key 或状态流的 state_key |
| --mode | string | 否 | 默认查所有必填项；传 `unfinished` 仅查未完成必填项 |

### workflow meta-node-fields
查看节点字段配置。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-type | string | 是 | 工作项类型 |

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

### workhour list-records
查看工作项的工时登记记录。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-type | string | 是 | 工作项类型 |
| --work-item-id | string | 是 | 工作项 ID |

---

## UserGroup 人员域

### user search
批量查询用户基础信息。用于将姓名/邮箱转换为 userkey。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --user-keys | array | 是 | userKey、Email 或名字，最多 20 个 |
| --project-key | string | 否 | 空间 key |

### user me
查看当前用户信息。无需参数。

> **MQL 中**可直接用 `current_login_user()` 函数，无需提前获取用户信息。如需获取当前用户的 userkey/姓名等详细信息，可用 `user search` 传入 `current_login_user()` 作为参数。

### team list
查看空间下的团队列表。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 否 | 空间 key |

### team list-members
查看团队成员列表。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --team-id | string | 是 | 团队 ID |

---

## View 视图域

### view get
根据视图 ID 获取该视图下的工作项列表。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --view-id | string | 是 | 视图 ID |
| --project-key | string | 否 | 空间 key |
| --page-num | number | 否 | 分页页数起点 |
| --fields | array | 否 | 要查询的字段 |

### view search
按名称搜索视图。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --view-scope | string | 是 | 视图范围 |
| --key-word | string | 是 | 关键词 |

### view create-fixed
创建固定视图。上限 200 个工作项。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --name | string | 是 | 视图名称 |
| --work-item-type | string | 是 | 工作项类型 |
| --work-item-id-list | array | 是 | 工作项 ID 列表 |

### view update-fixed
更新固定视图。add/remove_work_item_ids 二选一。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --view-id | string | 是 | 视图 ID |
| --work-item-type | string | 是 | 工作项类型 |

---

## Chart 度量域

### chart get
查看图表详情。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --chart-id | string | 是 | 图表 ID |

### chart list
查看视图下的图表列表。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --view-id | string | 是 | 视图 ID |

---

## Comment 评论域

### comment add
添加评论。支持富文本 Markdown，语法详见 [references/rich-text-editor-markdown-syntax.md](references/rich-text-editor-markdown-syntax.md)（含 @提及、对齐、链接预览、字号/颜色等扩展语法）。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-id | string | 是 | 工作项 ID |
| --comment-content | string | 是 | 评论内容 |

### comment list
查看评论列表。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-id | string | 是 | 工作项 ID |

---

## SubTask 子任务域

### subtask update
创建/修改/完成/回滚子任务。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --node-id | string | 是 | 节点 ID |
| --work-item-id | string | 是 | 工作项 ID |
| --action | string | 是 | create/update/confirm/rollback |

---

## Relation 关系域

### relation list
查看关联的工作项列表。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-id | string | 是 | 工作项 ID |
| --relation-field-key | string | 否 | 关联关系字段 key，从 `relation meta-definitions` 获取 |
| --relation-id | string | 否 | 关联关系 ID，从 `relation meta-definitions` 获取 |
| --node-id | string | 否 | 节点 ID，查询某节点下的关联时传入 |
| --page-num | number | 否 | 分页页码，从 1 开始 |
| --page-size | number | 否 | 每页数量，最大 50 |

### relation meta-definitions
查看空间下的关联关系定义。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |

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

1. **参数提取**：从自然语言中提取空间名、工作项类型、时间、人员、筛选条件；含 URL 时直传 `url` 参数并跳过参数确认。注意区分空间名与筛选维度（如「XX空间下YY业务线的缺陷」中 XX 才是空间名）。

2. **参数确认**（禁止猜测）：用探测命令校验空间（`project search`）、类型（`workitem meta-types`）、人员（`user search`）。**探测结果不唯一时必须展示并询问用户**，禁止自行选择；缺失必填合并为一条消息询问。个人待办（`mywork todo`）和 URL 直接操作可跳过。

3. **元数据收集**（无需用户参与）：调用 `workitem meta-fields` 获取字段定义（需要特定字段用 `field_keys`，模糊查询用 `field_query`）；涉及角色时并行调 `workitem meta-roles`。关键字段识别：状态字段 type=`_work_item_status`（含「完成/关闭/终止」的值为完成态）、排期字段 type=`schedule`（MQL 用 `__字段名_开始时间` / `__字段名_结束时间`）、优先级字段 key=`priority`。简单直调场景（仅需 project_key + work_item_id，如 `comment add`）可跳过本步。

4. **执行**：调用目标命令，遵循 [references/performance.md](references/performance.md) 的并行/翻页规则。

### 并行与大结果

详见 [references/performance.md](references/performance.md)：并行调用（必须串行的链路、可并行的组合）、大结果分批与翻页规则。

### 错误处理

**总则**：失败后从返回的 `err_msg` / `inner_err` 中提取错误原因，针对性修正后重试；**最多自动重试 2 次**，连续 3 次同类失败后停止并向用户说明。

**熔断条件**（立即终止，禁止盲目重试）：
- 空间未找到（`project search` 连续 3 次失败）
- Permission Denied（当前用户对该空间无访问权限）

**自愈规则**（按报错特征匹配修复后重试）：

| 报错特征 | 自愈动作 |
|---------|---------|
| `need STRING type, but got: LIST` / `MAP` | field_value 从原生 JSON 改为 JSON.stringify 后的字符串（见「字段值格式」） |
| `cannot unmarshal object...` | 仅改变格式（数字↔字符串、单值↔数组、对象↔纯字符串），值不变 |
| `不满足层级配置`（级联层级错误） | 查 `children` 树，展示末级叶子节点让用户选择 |
| `invalid select option(s)`（枚举不合法） | 从 `possible values` 匹配；唯一匹配则修正重试，否则询问用户 |

**错误速查**：

| 现象 | 排查/修复 |
|------|---------|
| 找不到空间 / 中文名匹配多个空间 | `project search` 验证，取 project_key 精确调用 |
| 找不到工作项类型 | `workitem meta-types` 确认合法 type_key |
| 字段名错误 / MQL 返回为空但数据存在 | `workitem meta-fields` 确认字段 key 与类型 |
| MQL 查询失败 | FROM 用 `` `空间名`.`工作项类型` ``；数组字段改用 `array_contains` / `any_match` |
| 日期区间字段查询失败 | 用子字段 `` `__字段名_开始时间` `` |
| 角色查询无结果 | MQL 角色名用 `` `__{角色名}` `` 格式 |
| 人名/团队名重复 | MQL 用 `<id:xxxx>` 消歧（见 MQL 语法参考） |
| 人名→userkey 失败 | `user search` 批量查询 |
| 人员字段写入失败 | user 传单个 userkey 字符串；multi-user 必须 stringified 如 `"[\"k1\",\"k2\"]"` |
| node not found | 先 `workitem get` 获取真实 node_id，禁止猜测 |
| 节点流转失败 | 节点流用 `workflow transition`；状态流用 `workflow transition-state`（先 `workflow list-state-transitions` 取 transition_id，再 `workflow list-state-required` 查必填项） |
| 创建工作项缺少模板 | `workitem meta-fields(field_keys=["template"])` 获取 |
| 角色更新失败 | 改用 `workitem update` 的 `role_operate` 参数（不走 fields） |
| mywork.todo 需选择工作区 | 按报错中的列表把 `asset_key`（Asset_xxx）传入重试 |

---

## 操作指南（SOP）

具体操作的完整流程、字段转换和自愈机制见对应 SOP：

- [创建工作项](references/sop-create-workitem.md) — 创建需求、任务、缺陷
- [更新工作项](references/sop-update-workitem.md) — 修改字段、更新角色、追加内容
- [流转节点（节点流）](references/sop-transition-node.md) — 完成/回滚节点、批量流转
- [流转状态（状态流）](references/sop-transition-state.md) — 流转缺陷/issue、关闭 bug
