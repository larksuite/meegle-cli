# 命令调用示例

> 参数占位符（`{{xxx}}`）表示按需填入；未使用的可选参数可省略。所有命令统一加 `--format json` 以获得结构化输出。

---

## 空间域

### project search

按名称或 key 搜索：

```bash
meegle project search --project-key 空间名或key --page-num 1 --format json
```

省略 project_key 可列出当前用户可访问的空间（按最近访问排序）。

---

## 工作项域

### workitem meta-types

```bash
meegle workitem meta-types --project-key 空间key --format json
```

### workitem meta-fields

```bash
meegle workitem meta-fields --page-num 1 --project-key 空间key --work-item-type story --field-types '{{field_types}}' --field-keys '{{field_keys}}' --field-query '{{field_query}}' --format json
```

### workitem meta-roles

```bash
meegle workitem meta-roles --page-num 1 --project-key 空间key --work-item-type story --role-keys '{{role_keys}}' --role-query '{{role_query}}' --format json
```

> **MQL 查询前必须先执行**：字段/枚举/树状字段用 `workitem meta-fields` 查 key 与 options；涉及角色并行调用 `workitem meta-roles` 取 `role_id` / `role_name`。字段与角色配置由「空间 + 工作项类型」双维度决定。业务线等树状字段直接传中文时必须传叶子节点名，禁拼完整路径。

### workitem query

首查：

```bash
meegle workitem query --project-key 空间key --mql 'SELECT `work_item_id`, `name`, `work_item_status` FROM `空间key`.`story` WHERE `archiving_date` IS NULL' --format json
```

无分组翻页时 group_id 传 `"1"`：

```bash
meegle workitem query --project-key 空间key --session-id 首查返回的session_id --mql '' --group-pagination-list '[{"group_id":"1","page_num":2}]' --format json
```

有分组翻页时，group_id 从首查 `list[].group_infos[].group_id` 取：

```bash
meegle workitem query --project-key 空间key --session-id 首查返回的session_id --mql '' --group-pagination-list '[{"group_id":"分组ID","page_num":3}]' --format json
```

### workitem get

基础查询：

```bash
meegle workitem get --work-item-id 工作项ID --fields '{{fields}}' --project-key 空间key --format json
```

只取拉群方式：

```bash
meegle workitem get --work-item-id 工作项ID --fields '["group_type"]' --project-key 空间key --format json
```

全量字段分页时 fields 传 `["_all"]`。Meegle CLI 的 page_size / page_token 必须通过 `--params` 传，避免被序列化为字符串：

```bash
meegle workitem get --work-item-id 工作项ID --fields '["_all"]' --project-key 空间key --params '{"page_size":100}' --format json
```

```bash
meegle workitem get --work-item-id 工作项ID --fields '["_all"]' --project-key 空间key --params '{"page_size":100,"page_token":"<next_page_token>"}' --format json
```

### workitem create

基础创建（仅标量）：

```bash
meegle workitem create --work-item-type story --fields '[{"field_key":"template","field_value":"模板ID"},{"field_key":"name","field_value":"需求标题"}]' --project-key 空间key --format json
```

创建并指定报告人/经办人（role_owners 是 stringified JSON）：

```bash
meegle workitem create --work-item-type issue --fields '[{"field_key":"name","field_value":"示例缺陷"},{"field_key":"template","field_value":"模板ID"},{"field_key":"role_owners","field_value":"[{\"role\":\"reporter\",\"owners\":[\"userkey1\"]},{\"role\":\"operator\",\"owners\":[\"userkey2\"]}]"}]' --project-key 空间key --format json
```

> **`role_owners` 中 `role` 字段填 `role_id`**（`workitem meta-roles` 返回的英文 key，如 `operator` / `reporter` / `role_xxxxxx`），不是 `role_name`。系统默认 `role_id`：报告人 `reporter`、经办人 `operator`。自定义角色的 `role_id` 形式多样，**必须先调 `workitem meta-roles` 确认**。创建时可在 `fields` 传 `role_owners`；更新已有工作项角色**必须**用 `workitem update` 的 `role_operate`。

### workitem update

更新普通字段：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --fields '[{"field_key":"priority","field_value":"option_id"}]' --format json
```

更新 multi-user（复合值 stringified）：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --fields '[{"field_key":"current_status_operator","field_value":"[\"userkey1\",\"userkey2\"]"}]' --format json
```

普通复合字段分别使用 `add` / `update` / `delete` action：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --fields '[{"field_key":"复合字段key","field_value":"{\"action\":\"add\",\"fields\":[[{\"field_key\":\"子字段key\",\"field_value\":\"示例值\"}]]}"}]' --format json
```

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --fields '[{"field_key":"复合字段key","field_value":"{\"action\":\"update\",\"group_uuid\":\"读回的组标识\",\"fields\":[[{\"field_key\":\"子字段key\",\"field_value\":\"新值\"}]]}"}]' --format json
```

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --fields '[{"field_key":"复合字段key","field_value":"{\"action\":\"delete\",\"group_uuid\":\"读回的组标识\"}"}]' --format json
```

多人复合字段必须整体覆盖且不可用于新增人员：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --fields '[{"field_key":"多人复合字段key","field_value":"{\"userkey1\":[{\"field_key\":\"子字段key\",\"field_value\":\"示例值\"}],\"userkey2\":[]}"}]' --format json
```

更新拉群方式（group_type 逻辑字段）：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --fields '[{"field_key":"group_type","field_value":"{\"type\":\"auto\"}"}]' --format json
```

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --fields '[{"field_key":"group_type","field_value":"{\"type\":\"bind\",\"group_id\":\"oc_xxx\"}"}]' --format json
```

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --fields '[{"field_key":"group_type","field_value":"{\"type\":\"disabled\"}"}]' --format json
```

`auto` / `disabled` 不得携带 group_id。

> **多人复合字段**：目标 userkey 不在读回 map 中时停止自动更新，向用户说明需先通过页面配置人员范围；接口空成功后必须回读确认。

---

## 人员域

### user search

默认仅在职：

```bash
meegle user search --user-keys '["张三","李四"]' --project-key {{project_key}} --need-all-status false --format json
```

包含离职/停用：

```bash
meegle user search --user-keys '["张三"]' --project-key {{project_key}} --need-all-status true --format json
```

### user me

```bash
meegle user me --format json
```

---

## 工作台域

### mywork todo

```bash
meegle mywork todo --action todo --page-num 1 --asset-key {{asset_key}} --format json
```

`--action`：`todo` / `done` / `overdue` / `this_week`。

---

## 工时域

### workhour list-schedule

```bash
meegle workhour list-schedule --start-time 2025-03-01 --end-time 2025-03-31 --project-key 空间key --user-keys '["张三","李四"]' --work-item-type-keys '{{work_item_type_keys}}' --format json
```

### workhour list-records

```bash
meegle workhour list-records --project-key 空间key --work-item-type story --work-item-id 工作项ID --page-num 1 --format json
```

---

## 视图域

### view get

```bash
meegle view get --view-id 视图ID --project-key 空间key --fields '{{fields}}' --page-num {{page_num}} --format json
```

---

## 工作流域

### workflow get-node

```bash
meegle workflow get-node --work-item-id 工作项ID --field-key-list '{{field_key_list}}' --need-sub-task {{need_sub_task}} --page-num {{page_num}} --project-key 空间key --node-id-list '["节点ID或_all"]' --format json
```

### workflow transition（节点流）

```bash
meegle workflow transition --work-item-id 工作项ID --project-key 空间key --node-id 节点ID --action confirm --rollback-reason '{{rollback_reason}}' --format json
```

### workflow transition-state（状态流）

```bash
meegle workflow transition-state --work-item-id 工作项ID --project-key 空间key --transition-id 流转ID --format json
```

### workflow list-state-transitions

```bash
meegle workflow list-state-transitions --work-item-id 工作项ID --work-item-type story --user-key userkey --project-key 空间key --format json
```

### workflow list-state-required

```bash
meegle workflow list-state-required --work-item-id 工作项ID --state-key 目标状态key --project-key 空间key --mode unfinished --format json
```

---

## 评论域

### comment add

```bash
meegle comment add --work-item-id 工作项ID --content '评论内容' --project-key {{project_key}} --format json
```

### comment list

```bash
meegle comment list --work-item-id 工作项ID --project-key 空间key --page-num {{page_num}} --start-time {{start_time}} --end-time {{end_time}} --format json
```

---

## 关系域

### relation meta-definitions

```bash
meegle relation meta-definitions --project-key 空间key --work-item-type {{work_item_type}} --relation-work-item-type {{relation_work_item_type}} --format json
```

### relation list

```bash
meegle relation list --project-key 空间key --work-item-id 工作项ID --page-size {{page_size}} --relation-field-key {{relation_field_key}} --node-id {{node_id}} --relation-id {{relation_id}} --page-num {{page_num}} --format json
```

---

## 子任务域

### subtask update

```bash
meegle subtask update --node-id 节点ID --project-key {{project_key}} --task-id {{task_id}} --assignee '{{assignee}}' --work-item-id 工作项ID --role-assignee '{{role_assignee}}' --fields '{{fields}}' --schedule '{{schedule}}' --action create --deliverable '{{deliverable}}' --format json
```
