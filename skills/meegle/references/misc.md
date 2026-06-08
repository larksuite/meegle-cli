# 其它低频命令

低频/单命令小域的参数表汇总。涵盖团队、图表、子任务、关系、评论查询、工时记录、交付物、资源库、WBS 辅助命令。

---

## 团队

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

## 度量图表

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

## 子任务

### subtask update
创建/修改/完成/回滚子任务。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --node-id | string | 是 | 节点 ID |
| --work-item-id | string | 是 | 工作项 ID |
| --action | string | 是 | create/update/confirm/rollback |

---

## 关系

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

## 评论查询

### comment list
查看评论列表。添加评论用 `comment add`（见 SKILL.md 主文件）。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-id | string | 是 | 工作项 ID |

---

## 工时记录

### workhour list-records
查看工作项的工时登记记录。团队排期用 `workhour list-schedule`（见 SKILL.md 主文件）。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-type | string | 是 | 工作项类型 |
| --work-item-id | string | 是 | 工作项 ID |

---

## 交付物

### deliverable list
查看交付物详情及其所属根工作项 / 来源工作项。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-ids | string[] | 否 | 工作项 ID 列表；URL 自动解析；提供名称需先调 `workitem get` 拿 ID |

---

## 资源库

### resource create
在已启用资源库的工作项类型下创建资源模板（资源实例）。先调 `resource meta-fields` 取字段 / 角色配置。

**创建资源实例 vs 从资源实例创建普通工作项**：
- 创建资源实例：使用 `resource create`；`work_item_type_key` 表示资源库启用的工作项类型，`template_id` 表示流程模板，`fields` / `roles` 描述新资源实例自身。
- 从资源实例创建普通工作项：这是“基于已有资源实例派生/创建业务工作项”的语义，参数通常需要源资源实例标识。当前命令参数必须以 `inspect` / schema 为准；若没有显式源资源实例参数，不要把源资源实例 ID 塞进 `work_item_type_key`、`template_id` 或普通字段里猜测调用。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-type-key | string | 是 | 工作项类型 key 或名称；失败时先调 `workitem meta-types` |
| --fields | object[] | 否 | 资源字段列表，每项含字段 key 与字段值 |
| --roles | object[] | 否 | 角色人员；为空则不指定 |
| --template-id | string | 否 | 工作流模板 ID 或名称；未传则取该工作项类型的第一个流程模板 |

### resource meta-fields
查看资源库的字段 / 角色配置。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-type-key | string | 是 | 工作项类型 key 或名称 |

---

## WBS 辅助命令

> 计划表（WBS）的核心查询 / 编辑 / 发布命令见 [wbs.md](wbs.md)。本节仅收 4 个辅助命令：草稿生命周期管理（create-draft / reset-draft）、异步操作进度查询（get-draft-progress）、流程资源库元素查询（list-element-templates）。

### wbs create-draft
为指定工作项实例创建新的计划表草稿。当需要编辑计划表但当前不存在草稿时，先调本工具创建草稿，再配合 `wbs edit-draft` 进行编辑。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-id | string | 是 | 工作项 ID，单值；URL 自动解析 |
| --project-key | string | 是 | 空间 key |

### wbs reset-draft
将草稿重置为线上实例状态，**放弃所有未发布的修改**。不传 `uuids` 时全量重置。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-id | string | 是 | 工作项 ID |
| --project-key | string | 是 | 空间 key |
| --uuids | string[] | 否 | 要重置的行 uuid 列表；为空则全量重置 |

### wbs get-draft-progress
查询计划表草稿异步操作（create / edit / publish / reset）的执行进度。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-id | string | 是 | 工作项 ID 或名称 |
| --op-type | string | 是 | 操作类型：`create` / `edit` / `publish` / `reset` |
| --operation-id | string | 是 | 操作 ID（由 create-draft / edit-draft / publish-draft / reset-draft 返回） |

### wbs list-element-templates
列出流程资源库中的资源节点（node）或资源任务（task）模板。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --element-type | string | 是 | 资源库类型：`node` 或 `task` |
| --project-key | string | 是 | 空间 key |
| --work-item-type | string | 是 | 工作项类型 key 或名称 |
| --page-size | number | 否 | 页大小 |
| --page-no | number | 否 | 页码 |
