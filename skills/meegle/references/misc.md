# 其它低频命令

低频/单命令小域的参数表汇总。涵盖团队、图表、子任务、关系、评论查询、工时记录。

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
