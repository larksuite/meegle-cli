# MQL 完整示例与关键词映射

示例中的字段 key、角色、options 均以 `workitem meta-fields` / `workitem meta-roles` 返回为准（两者都同时传 `--project-key` 与 `--work-item-type`）。

## 1. 数组包含 + 当前用户 + 未归档

```sql
SELECT `work_item_id`, `name`, `work_item_status`, `priority`
FROM `project_key`.`story`
WHERE array_contains(`current_status_operator`, current_login_user()) AND `archiving_date` IS NULL
```

## 2. 相对时间；逾期未完成（日期区间字段 + 状态过滤）

```sql
SELECT `work_item_id`, `name`, `start_time` FROM `project_key`.`story`
WHERE RELATIVE_DATETIME_BETWEEN(`start_time`, 'past', '30d')
-- 逾期未完成
SELECT `work_item_id`, `name`, `work_item_status` FROM `project_key`.`story`
WHERE RELATIVE_DATETIME_LT(`__field_xxxxxx_结束时间`, 'today') AND `work_item_status` != '<完成态label>'
```

## 3. 团队角色（`team list` 前置）

`'真实团队名'` 是占位，必须先 `team list --project-key` 拉取真实团队名，否则报 `attribute_value not found`。

```sql
SELECT `work_item_id`, `name`, `priority` FROM `project_key`.`story`
WHERE any_match(`__后端开发`, x -> x IN (team(true, '真实团队名')))
-- fallback（仅角色名冲突时）
WHERE any_match(`__role_<project_key>_story_<role_id>`, x -> x IN (team(true, '真实团队名')))
```

## 4. 综合（模糊 + 数组 + 排序截断；要全量走 `--session-id` 翻页）

```sql
SELECT `work_item_id`, `name`, `work_item_status`, `priority` FROM `project_key`.`issue`
WHERE `name` LIKE '%性能优化%' AND array_contains(`current_status_operator`, current_login_user()) AND `priority` = 'P0'
ORDER BY `updated_at` DESC LIMIT 50
```

## 5. 节点属性 + 延期标识

```sql
-- 所属节点当前负责人（单条件；`__BELONGING` 属性禁 AND 组合，见 mql-nodes-relations.md §4）
SELECT `work_item_id`, `name`, `work_item_status` FROM `project_key`.`story`
WHERE array_contains(get_node_attribute('__BELONGING','当前负责人'), '<id:userkey>')
-- 所属节点状态：必须 = 顶层比较，禁止 array_contains 包裹
SELECT `work_item_id`, `name` FROM `project_key`.`story`
WHERE get_node_attribute('__BELONGING','状态') = '<进行中状态label>'
-- 开始节点已延期（集合完全相等，JSON 数组字符串例外）
SELECT `work_item_id`, `name` FROM `project_key`.`story` WHERE risk_label() = '["延期/开始"]'
```

## 6. 关系查询（链式 + 跨空间字段）

```sql
-- 子任务的父工作项名称包含「登录」
SELECT `work_item_id`, `name` FROM `project_key`.`sub_task`
WHERE any_relation_match(relation_field_chain('__父工作项'), x -> x.`name<target:all>` like '%登录%')
-- 多级关系：子任务 → 父工作项 → 关联软件
SELECT `work_item_id`, `name` FROM `project_key`.`sub_task`
WHERE any_relation_match(relation_field_chain('__父工作项','需求关联软件'), x -> x.`name<target:all>` = '某软件')
```

## 7. 状态时间 + 节点负责人

```sql
-- issue（状态流）：状态窗口
SELECT `work_item_id`, `name`, `work_item_status` FROM `project_key`.`issue`
WHERE status_time('<状态名>') between '2025-01-01' and '2025-12-31'
-- story（节点流）：开始节点负责人
SELECT `work_item_id`, `name` FROM `project_key`.`story`
WHERE array_contains(get_node_attribute('开始','负责人'), '<id:userkey>')
```

## 8. 关键词 → 语法映射

| 用户关键词 | 语法 |
|-----------|------|
| 参与人员、全部参与人员 | `all_participate_persons()` |
| 当前参与人 | `participate_persons()` |
| 流程节点、所有节点 | `all_nodes_name()` |
| 进行中节点 | `in_progress_nodes_name()` |
| 节点排期、节点估分、所属节点 | `get_node_attribute(node, attr)`（所属节点用 `__BELONGING`） |
| 节点延期标识 | `risk_label()` |
| 关联工作项字段 | `relation_field_chain('rel1',...)`（≤ 3 跳） |
| 子任务父工作项 | `relation_field_chain('__父工作项')` |
| 子任务来源 | `linked_work_item()` |
| 状态时间窗口（状态流） | `status_time('状态名') between ...` |
| 状态累计时长（状态流） | `status_time('__<状态名>_结束时间') - status_time('__<状态名>_开始时间')`（仅 SELECT） |
| 关系语境「每一个」/「存在一个」/「每一个不满足」/「存在一个不满足」 | `all_relation_match` / `any_relation_match` / `none_relation_match` / `not all_relation_match` |
| 包含已终止的、全部（含已终止） | 传 `--features '{"FindAborted":"true"}'`，WHERE 不加终止条件（见 [mql-basics.md §11](mql-basics.md)） |
| 已终止的、被终止的 | MQL 的 WHERE 加 `` `abort_reason` IS NOT NULL ``；调用时另传参数 `--features '{"FindAborted":"true"}'` |
| 数组语义 | 语法 |
|------|------|
| 存在选项属于（OR） | `IN + 元组`（首选）；控件函数用 `any_match` |
| 全部选项均不属于 | `NOT IN`（首选）；`none_match` 备选 |
| 同时包含（AND） | `array_contains(field, 'a', 'b')` |
| 集合完全相等 | `` `field` = ('v1','v2') ``（禁 JSON 数组字符串，控件函数除外） |
