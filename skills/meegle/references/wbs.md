# WBS 计划表

计划表（Work Breakdown Structure）有两套数据模型：

- **草稿（draft）** — 当前用户的可编辑副本。`wbs create-draft` 创建、`wbs edit-draft` 单行原子编辑、`wbs publish-draft` 发布、`wbs reset-draft` 放弃修改。
- **实例（instance）** — 已发布的线上版本。只读，用 `wbs list-instance-rows` 查询。

**常见流程**：`create-draft` → 多次 `edit-draft` → `publish-draft`。

**辅助命令**（创建草稿 / 重置 / 进度 / 模板）请见 [misc.md](misc.md)。

---

## 共用查询能力（draft / instance）

`wbs list-draft-rows` 与 `wbs list-instance-rows` 参数完全一致，仅查询的数据集不同——前者查当前用户的草稿，后者查线上实例。**禁止混用**：例如先用 `list-draft-rows` 取行 uuid，后续递归 / 编辑也必须继续用草稿系列命令。

### condition_query 支持字段

筛选行用 `condition_query`（object）。**仅支持以下字段**：

| 字段 | 含义 | 取值 |
|------|------|------|
| `wbs_name` | 行名称 / 任务名称 / 排期项名称 / 子项名称 | string |
| `wbs_parent_id` | 父级 uuid（用于查子级 / 下级 / 子任务 / 子项） | uuid |
| `wbs_belong_status` | 所属状态 / 阶段（计划 / 开发 / 验证 / 发布等） | string |
| `wbs_states_doing` | 当前状态 / 任务状态 | `not_started` / `doing` / `finished` |
| `wbs_role` | 角色（可多人） | string |
| `wbs_owner_in_charge` | 负责人 / 责任人（可多人） | userkey；查"我负责的"先调 `user search` 取 userkey |
| `wbs_delay_label` | 延期标识 | `delay` / `normal` |
| `wbs_milestone_node_type` | 节点类型 | `milestone` / `normal_node` / `key_path_node` |
| `wbs_deletable` | 允许删除节点 | bool |

**递归查子级 SOP**：用户提到"子级 / 所有子 / 下级"时，先按条件筛出目标行取 `uuid`，再用 `wbs_parent_id` + `In` 查直接子级（多个 uuid 逗号分隔），再以下一层 uuid 继续递归直到无子级。**全流程必须使用同一工具**（草稿就一直草稿，实例就一直实例）。

### row_field_list 返回字段控制

`row_field_list`（string[]）按需指定返回字段。为空时默认返回 `base.*` + `meta.uuid`；`["_all"]` 返回全量字段。

| 通配符 | 包含字段 |
|--------|----------|
| `meta.*` | `uuid`、`parent_id`、所属工作项信息等 |
| `base.*` | `name`（行名）、`owners`（负责人）、`start_time` / `end_time`（实际开始 / 完成时间）、`schedule`（排期）、`schedule_dependency`（排期依赖）、`union_deliveries`（交付物）、`process_status`（当前状态） |
| `node_extra.*` | 普通节点扩展：里程碑、所属状态、节点唯一 id `state_key`、前序节点等 |
| `sub_instance_extra.*` | 子实例扩展：拆解模式 `dismantle_mode` |

**查工作项字段 / 节点字段**：先用 `workitem meta-fields` 判断是否为工作项字段、用 `workflow meta-node-fields` 判断是否为节点字段；再从计划表行中取对应 `workitem_id` / `state_key`，基于这些 ID 继续查字段值。

### wbs list-draft-rows

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-id | string | 是 | 工作项 ID（**字符串**）；URL 自动解析；名称需先调 `workitem get` |
| --project-key | string | 是 | 空间 key |
| --condition-query | object | 否 | 筛选条件，仅支持上表字段 |
| --need-structure | boolean | 否 | 是否返回树状层级。默认 `false`；查 / 编辑子级时设为 `true` |
| --page-no | number | 否 | 页号，从 1 开始；返回 `has_more` 时需翻页 |
| --page-size | number | 否 | 页大小，1–50，默认 25。超过 1000 行需分页合并 |
| --row-field-list | string[] | 否 | 见上表 |

### wbs list-instance-rows

参数与 `wbs list-draft-rows` 完全一致，仅查询数据集不同（线上已发布实例）。

---

## wbs edit-draft

对计划表草稿单行执行**一次原子操作**。操作类型通过 `operation`（object）参数指定。一次调用只能执行一种操作类型；批量编辑请循环调用。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --work-item-id | string | 是 | 工作项 ID |
| --project-key | string | 是 | 空间 key |
| --operation | object | 是 | 操作对象，结构因操作类型而异 |

### operation 结构

`operation` 是一个对象，统一形如：

```json
{
  "operation_type": "<动作 PascalCase>",
  "operation_value": {
    "<动作 snake_case>": { /* 动作特定字段 */ }
  }
}
```

常见操作类型（按动作分类）：

| 动作 | operation_type | operation_value 子 key | 用途 |
|------|----------------|------------------------|------|
| 新增 | `AddTaskRow` | `add_task_row` | 在指定父行下新增一行任务 |
| 删除 | （delete）| —— | 删除一行（及其子级） |
| 恢复 | （restore）| —— | 撤销删除 |
| 排序 | （sort）| —— | 调整行在同级中的位置 |
| 改名 | （rename）| —— | 修改 `name` |
| 改负责人 | （owner）| —— | 修改 `owners` |
| 改排期 | （schedule）| —— | 修改 `schedule` 或 `start_time` / `end_time` |

> 上表只有 `AddTaskRow` 一行的字段名是经验证的。其余动作的 `operation_type` 字符串和 `operation_value` 子 key 未公开 schema，使用前请先在测试环境跑一遍取得真实结构，或向 IPD 后端确认；遇到 4xx 报错时优先怀疑这两个名字。

**示例：在 `parent_uuid` 下新增一行任务**

```json
{
  "operation_type": "AddTaskRow",
  "operation_value": {
    "add_task_row": {
      "parent_uuid": "<上级行 uuid，来自 list-draft-rows>",
      "name": "新任务名"
    }
  }
}
```

调用后响应里 `change_uuids[0]` 是新行的 uuid，可直接拿去做下一步 `edit-draft`（如改排期）或 `publish-draft` 的部分发布。

**建议工作流**：
1. `wbs list-draft-rows` 取目标行 / 父行的 `uuid` 与当前字段值
2. 调 `wbs edit-draft` 一次执行一种操作
3. 如调用返回了 `operation_id`，先用 `wbs get-draft-progress` 轮询完成再进行下一次编辑——多个 `edit-draft` 并发或不等异步完成就连发可能丢操作
4. 全部改完用 `wbs publish-draft` 发布

---

## wbs publish-draft

将编辑完成的草稿发布到线上。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-id | string | 是 | 工作项 ID |
| --uuid-strings-list | string[] | 否 | 要发布的行 uuid 列表。**部分发布**：传 uuid 列表，无需二次确认。**全量发布**：不传此字段（或传 `["_all"]`），必须先用以下**固定话术**二次确认，用户同意后才执行 |

### 全量发布二次确认（固定话术）

> 本人及协同者的全部编辑内容均会被发布，请确认是否全量发布？

部分发布（传入 `uuid_strings_list`）**不需要**二次确认，直接执行。

---

## 异步操作进度

`wbs create-draft` / `wbs edit-draft` / `wbs publish-draft` / `wbs reset-draft` 返回 `operation_id` 后，需用 `wbs get-draft-progress` 轮询进度。参数表见 [misc.md](misc.md#wbs-辅助命令)。
