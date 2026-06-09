# WBS 计划表

计划表（Work Breakdown Structure）有两套数据模型：

- **草稿（draft）** — 当前用户的可编辑副本。`wbs create-draft` 创建、`wbs edit-draft` 原子编辑（单行或批量操作）、`wbs publish-draft` 发布、`wbs reset-draft` 放弃修改。
- **实例（instance）** — 已发布的线上版本。只读，用 `wbs list-instance-rows` 查询。

**常见流程**：`wbs create-draft` → 多次 `wbs edit-draft` → `wbs publish-draft`。

**辅助命令**（创建草稿 / 重置 / 进度 / 模板）请见 [misc.md](misc.md)。

---

## 共用查询能力（draft / instance）

`wbs list-draft-rows` 与 `wbs list-instance-rows` 参数完全一致，仅查询的数据集不同——前者查当前用户的草稿，后者查线上实例。**禁止混用**：例如先用 `wbs list-draft-rows` 取行 uuid，后续递归 / 编辑也必须继续用草稿系列命令。

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

对计划表草稿执行**一次原子编辑**。一次调用只能传一个 `operation_type`；该操作内部可通过 `items` / `uuids` 等数组承载单行或批量编辑。

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

`operation_value` 下的子 key 必须与 `operation_type` 匹配；优先按下表传，避免只做机械 snake_case 猜测。

| operation_type | operation_value 子 key | 用途 |
|----------------|------------------------|------|
| `AddTaskRows` | `add_task_rows` | 批量新增普通任务行 |
| `AddNodeRows` | `add_node_rows` | 批量新增资源节点行 |
| `AddSubInstanceRows` | `add_sub_instance_rows` | 批量新增子工作项行 |
| `AddResourceSubInstanceRows` | `add_resource_sub_instance_row` | 批量新增资源子实例行（注意子 key 里的 `row` 为单数） |
| `DeleteRows` | `delete_rows` | 批量删除 / 移出行 |
| `RestoreRows` | `restore_rows` | 批量恢复行 |
| `UpdateDismantleMode` | `update_dismantle_mode` | 切换拆解模式 |
| `AdjustRowOrder` | `adjust_row_order` | 调整行排序 / 父级 |
| `UpdateNodeSequence` | `update_node_sequence` | 修改资源节点前后序 |
| `UpdateNodePhases` | `update_node_phases` | 批量修改节点所属阶段 |
| `UpdateName` | `update_name` | 修改名称 |
| `UpdateRoleOwners` | `update_role_owners` | 批量修改角色负责人 |
| `UpdateDelivery` | `update_delivery` | 修改交付物 |
| `UpdatePlannedSchedule` | `update_planned_schedule` | 修改单行排期（旧单行形态） |
| `UpdatePlannedSchedules` | `update_planned_schedules` | 批量修改计划排期 |
| `UpdateSchedulePoints` | `update_schedule_points` | 批量修改估分 |
| `UpdateScheduleActualTimes` | `update_schedule_actual_times` | 批量修改实际工时 |

**选择要点**：
- 用户说普通任务 / 子任务 / 任务行时用 `AddTaskRows`；其 `parent_uuid` 必须是当前计划表中允许新增子任务的父行，并非任意 `sub_instance` / `node` / `sub_task` 都可用。若后端返回 `can not add sub task under parentUUID` 或 `wbs task not found`，说明父行不支持该新增位置，需换父行或先确认拆解结构。
- 用户说工作项类型（如"需求"、"项目活动"）时用 `AddSubInstanceRows`，并先调 `workitem meta-types` 确认 `work_item_type_key`。
- 涉及资源节点或资源子实例时，先调 `wbs list-element-templates` 查询模板 / `element_key`；资源节点通常用 `AddNodeRows`，资源子实例用 `AddResourceSubInstanceRows`。
- `UpdateRoleOwners` 与 `UpdateDelivery` 是全量覆盖语义。追加负责人或交付物前，先用 `wbs list-draft-rows` 取当前值并合并历史值；不要只传新增值。
- 排期时间写 ISO8601 带时区字符串（如 `2026-04-22T00:00:00+08:00`）；不要传毫秒时间戳，也不要在 `specify_schedule` 外额外包一层 `schedule`。

**示例：批量新增普通任务行**

```json
{
  "operation_type": "AddTaskRows",
  "operation_value": {
    "add_task_rows": {
      "parent_uuid": "<上级行 uuid，来自 wbs list-draft-rows>",
      "pre_uuid": "",
      "items": [
        { "name": "新任务名" }
      ]
    }
  }
}
```

**示例：批量新增资源节点行**

```json
{
  "operation_type": "AddNodeRows",
  "operation_value": {
    "add_node_rows": {
      "parent_uuid": "<上级行 uuid>",
      "pre_uuid": "",
      "phase": "started",
      "items": [
        { "element_key": "<来自 wbs list-element-templates 的 element_key>" }
      ]
    }
  }
}
```

**示例：批量新增子工作项行**

```json
{
  "operation_type": "AddSubInstanceRows",
  "operation_value": {
    "add_sub_instance_rows": [
      {
        "parent_uuid": "<上级行 uuid>",
        "work_item_type_key": "<来自 workitem meta-types 的工作项类型 key>",
        "fields": [
          { "field_key": "name", "field_value": "子工作项名称" }
        ]
      }
    ]
  }
}
```

**示例：批量新增资源子实例行**

```json
{
  "operation_type": "AddResourceSubInstanceRows",
  "operation_value": {
    "add_resource_sub_instance_row": [
      {
        "parent_uuid": "<上级行 uuid>",
        "work_item_type_key": "<资源工作项类型 key>",
        "resource_work_item_ids": ["<已有资源实例 ID>"]
      }
    ]
  }
}
```

**示例：批量修改计划排期**

```json
{
  "operation_type": "UpdatePlannedSchedules",
  "operation_value": {
    "update_planned_schedules": {
      "uuids": ["<行 uuid>"],
      "different_schedule": false,
      "specify_schedule": {
        "estimate_start": "2026-04-22T00:00:00+08:00",
        "estimate_finish": "2026-04-25T23:59:59+08:00"
      }
    }
  }
}
```

**示例：批量修改估分 / 实际工时**

```json
{
  "operation_type": "UpdateSchedulePoints",
  "operation_value": {
    "update_schedule_points": {
      "uuids": ["<行 uuid>"],
      "different_schedule": false,
      "schedule_point": {
        "schedule_point_value": 3,
        "schedule_point_value_unit": 8
      }
    }
  }
}
```

```json
{
  "operation_type": "UpdateScheduleActualTimes",
  "operation_value": {
    "update_schedule_actual_times": {
      "uuids": ["<行 uuid>"],
      "different_schedule": false,
      "actual_time": {
        "value": 5,
        "unit": 8
      }
    }
  }
}
```

**示例：批量删除 / 恢复行**

```json
{
  "operation_type": "DeleteRows",
  "operation_value": {
    "delete_rows": {
      "uuids": ["<行 uuid>"],
      "delete_action_type": "delete",
      "reason": "重复行"
    }
  }
}
```

```json
{
  "operation_type": "RestoreRows",
  "operation_value": {
    "restore_rows": {
      "uuids": ["<行 uuid>"]
    }
  }
}
```

**示例：批量修改阶段 / 负责人**

```json
{
  "operation_type": "UpdateNodePhases",
  "operation_value": {
    "update_node_phases": {
      "phase": "started",
      "uuids": ["<行 uuid>"]
    }
  }
}
```

```json
{
  "operation_type": "UpdateRoleOwners",
  "operation_value": {
    "update_role_owners": {
      "uuids": ["<行 uuid>"],
      "owners": ["<userkey>"]
    }
  }
}
```

调用后响应里若有 `change_uuids`，可直接拿去做下一步 `wbs edit-draft`（如改排期）或 `wbs publish-draft` 的部分发布。

**建议工作流**：
1. `wbs list-draft-rows` 取目标行 / 父行的 `uuid` 与当前字段值
2. 调 `wbs edit-draft` 一次执行一种操作
3. 如调用返回了 `operation_id`，先用 `wbs get-draft-progress` 轮询完成再进行下一次编辑——多个 `wbs edit-draft` 并发或不等异步完成就连发可能丢操作
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
