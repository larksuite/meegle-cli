# 视图辅助命令

视图搜索与固定视图管理。读取视图数据用 `view get`（见 SKILL.md 主文件）。

## view search
按名称搜索视图。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --view-scope | string | 是 | 视图范围 |
| --key-word | string | 是 | 关键词 |

## view create-fixed
创建固定视图。上限 200 个工作项。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --name | string | 是 | 视图名称 |
| --work-item-type | string | 是 | 工作项类型 |
| --work-item-id-list | array | 是 | 工作项 ID 列表 |

## view update-fixed
更新固定视图。add/remove_work_item_ids 二选一。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --view-id | string | 是 | 视图 ID |
| --work-item-type | string | 是 | 工作项类型 |

## view list-multi-project-workitems
查看全景视图（multiProjectView）下当前用户有权限的工作项列表。全景视图的链接上带有 `multiProjectView` 关键字，可从中提取 `view_id`。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --view-id | string | 是 | 全景视图 ID |
| --page-num | number | 否 | 分页页码，每页 50 条，从 1 开始 |
