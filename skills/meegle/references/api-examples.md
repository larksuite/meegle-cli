# 命令调用示例

---

## 空间域

### project search
已知空间名/key：

```bash
meegle project search --project-key 空间名或key --page-num {{page_num}} --format json
```

列出当前用户可访问的空间（按最近访问排序，分页）：

```bash
meegle project search --project-key {{project_key}} --page-num 1 --format json
```

## 工作项域

### workitem meta-types
```bash
meegle workitem meta-types --project-key 空间key --format json
```

### workitem meta-fields
查询所有字段：

```bash
meegle workitem meta-fields --page-num 1 --project-key 空间key --work-item-type story --field-types '{{field_types}}' --field-keys '{{field_keys}}' --field-query '{{field_query}}' --format json
```

### workitem meta-roles
```bash
meegle workitem meta-roles --page-num 1 --project-key 空间key --work-item-type story --role-keys '{{role_keys}}' --role-query '{{role_query}}' --format json
```

### workitem query
查询空间中所有未冻结的需求：

```bash
meegle workitem query --project-key 空间key --session-id {{session_id}} --mql 'SELECT `work_item_id`, `name`, `current_owners`, `status` FROM `空间名`.`story` WHERE `is_archived` = 0' --group-pagination-list '{{group_pagination_list}}' --format json
```

### workitem get
```bash
meegle workitem get --work-item-id 工作项ID或名称 --fields '{{fields}}' --project-key 空间key --format json
```

### workitem create
基础创建（仅标量字段）：

```bash
meegle workitem create --work-item-type story --fields '[{"field_key": "template", "field_value": "模板ID"}, {"field_key": "name", "field_value": "需求标题"}]' --project-key 空间key --format json
```

创建缺陷 + 指定报告人（multi-user）+ 指定经办人（role_owners）——注意复合值必须 JSON.stringify：

```bash
meegle workitem create --work-item-type issue --fields '[{"field_key":"name","field_value":"示例缺陷"},{"field_key":"priority","field_value":"2"},{"field_key":"template","field_value":"模板ID"},{"field_key":"issue_reporter","field_value":"["userkey1"]"},{"field_key":"role_owners","field_value":"[{"role":"operator","owners":["userkey1"]}]"}]' --project-key 空间key --format json
```

> 🚨 `issue_reporter`（multi-user 类型的内置角色字段）和 `role_owners`（统一角色入口）是**两种可互换的写法**：前者走 meta-create-fields 返回的字段 key；后者用 meta-roles 返回的 role_id（如 `operator` / `reporter`，不含 `issue_` 前缀）。两者的 `field_value` 都必须是 **stringified JSON** 字符串。

### workitem update
更新普通字段：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --role-operate '{{role_operate}}' --fields '[{"field_key": "priority", "field_value": "option_id"}]' --format json
```

更新 multi-user 字段（复合值 stringified）：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --role-operate '{{role_operate}}' --fields '[{"field_key": "current_status_operator", "field_value": "["userkey1","userkey2"]"}]' --format json
```

---

## 人员域

### user search
```bash
meegle user search --user-keys '["张三", "李四"]' --project-key {{project_key}} --format json
```

### user me
```bash
meegle user me --format json
```

---

## 工作台域

### mywork todo
查询我的待办：

```bash
meegle mywork todo --action todo --page-num 1 --asset-key {{asset_key}} --format json
```

---

## 工时域

### workhour list-schedule
```bash
meegle workhour list-schedule --start-time 2025-03-01 --end-time 2025-03-31 --project-key 空间key --user-keys '["张三", "李四"]' --work-item-type-keys '{{work_item_type_keys}}' --format json
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

### workflow transition
完成节点（节点流）：

```bash
meegle workflow transition --work-item-id 工作项ID --node-ids '{{node_ids}}' --project-key 空间key --node-id 节点ID --action confirm --rollback-reason '{{rollback_reason}}' --format json
```

### workflow transition-state
流转状态（状态流）：

```bash
meegle workflow transition-state --work-item-id 工作项ID --project-key 空间key --transition-id 流转ID --format json
```

### workflow list-state-transitions
```bash
meegle workflow list-state-transitions --work-item-id 工作项ID --work-item-type story --user-key userkey --project-key 空间key --format json
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
