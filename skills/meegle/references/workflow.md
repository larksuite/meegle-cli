# 工作流辅助命令

工作流流转之前用来查询可流转方向、必填项、节点字段配置的辅助命令。核心流转命令（`workflow transition` / `workflow transition-state` / `workflow get-node` / `workflow update-node`）见 SKILL.md 主文件。

## workflow list-state-transitions
查看工作项可流转的状态列表。状态流流转前必须先调用此命令拿 `transition_id`。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-id | string | 是 | 工作项 ID |
| --work-item-type | string | 是 | 工作项类型 |
| --user-key | string | 是 | 用户标识 |

## workflow list-state-required
查看流转所需的必填信息（节点流传 node_key，状态流传 state_key）。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-id | string | 是 | 工作项 ID |
| --state-key | string | 是 | 节点流的 node_key 或状态流的 state_key |
| --mode | string | 否 | 默认查所有必填项；传 `unfinished` 仅查未完成必填项 |

## workflow meta-node-fields
查看节点字段配置。`workflow update-node` 修改节点自定义字段前用来确认合法 field_key、字段类型与 options。查询评审结论选项时按 `field_keys=["node_finished_conclusion"]` 精确查询，并从返回配置的 options / 选项列表中取合法值。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-type | string | 是 | 工作项类型 |
| --field-keys | array | 否 | 精确匹配节点字段 key 或名称，如 `["node_finished_conclusion"]` |
| --field-types | array | 否 | 按节点字段类型筛选 |
| --query | string | 否 | 按字段 name / key 模糊搜索 |
