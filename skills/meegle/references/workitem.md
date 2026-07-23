# 工作项元数据命令

查询工作项类型、字段、角色配置的辅助命令。在 `workitem create` / `workitem update` / `workitem query` 之前用来确认合法 key。

## workitem meta-types
获取指定空间下所有工作项类型列表。用户描述模糊时用此命令确认合法 type_key。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 projectKey |

## workitem meta-fields
获取指定空间和工作项类型的可用字段配置（不含禁用字段和角色配置）。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-type | string | 是 | 工作项类型 key 或名称 |
| --page-num | number | 是 | 页数，每页 50 条，从 1 开始 |
| --field-keys | array | 否 | 精确匹配字段 key 或名称 |
| --field-query | string | 否 | 模糊查询字段 key 和名称 |
| --field-types | array | 否 | 按字段类型筛选 |

## workitem meta-roles
获取指定工作项类型的角色列表。用于查询/创建/更新工作项前确认合法 role_key。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-type | string | 是 | 工作项类型 key 或名称 |
| --page-num | number | 是 | 页数，每页 50 条，从 1 开始 |
| --role-keys | array | 否 | 精确匹配角色 key 或名称 |
| --role-query | string | 否 | 模糊查询角色 key 和名称 |

## workitem meta-create-fields
查看创建工作项时可用的字段及类型。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-type | string | 是 | 要创建的工作项类型 key |
