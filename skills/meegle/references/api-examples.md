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

继续查询无分组结果的第 2 页（无分组时 `group_id` 传 `"1"`，`session_id` 使用上一次查询返回值）：

```bash
meegle workitem query --project-key 空间key --session-id 上次返回的session_id --mql '' --group-pagination-list '[{"group_id":"1","page_num":2}]' --format json
```

继续查询某个分组的第 3 页（`group_id` 来自首查返回的 `list[].group_infos[].group_id`）：

```bash
meegle workitem query --project-key 空间key --session-id 上次返回的session_id --mql '' --group-pagination-list '[{"group_id":"分组ID","page_num":3}]' --format json
```

### workitem get
```bash
meegle workitem get --work-item-id 工作项ID或名称 --fields '{{fields}}' --project-key 空间key --format json
```

只取拉群方式（`group_type` 逻辑字段，聚合自 `group_id` / `chat_group`）：

```bash
meegle workitem get --work-item-id 工作项ID --fields '["group_type"]' --project-key 空间key --format json
```

全量字段分页（`fields=["_all"]` 时按逻辑字段分页，`page_size` 默认 100，最大 200；下一页用上次响应的 `next_page_token` 传 `page_token`，token 形如字段 key 如 `"business"`）：

⚠️ meegle CLI 当前 `--page-size` / `--page-token` flag 会被序列化成字符串触发后端 `need I64 type, but got: STRING`；当前可用的写法是通过 `--params` 把整数传出去——

```bash
meegle workitem get --work-item-id 工作项ID --project-key 空间key --fields '["_all"]' --params '{"page_size":100}' --format json
meegle workitem get --work-item-id 工作项ID --project-key 空间key --fields '["_all"]' --params '{"page_size":100,"page_token":"<next_page_token>"}' --format json
```

### workitem create
基础创建（仅标量字段）：

```bash
meegle workitem create --work-item-type story --fields '[{"field_key": "template", "field_value": "模板ID"}, {"field_key": "name", "field_value": "需求标题"}]' --project-key 空间key --work-item-id {{work_item_id}} --ignore-required {{ignore_required}} --ignore-role-calculate {{ignore_role_calculate}} --format json
```

创建缺陷 + 指定报告人（multi-user）+ 指定经办人（role_owners）——注意复合值必须 JSON.stringify：

```bash
meegle workitem create --work-item-type issue --fields '[{"field_key":"name","field_value":"示例缺陷"},{"field_key":"priority","field_value":"2"},{"field_key":"template","field_value":"模板ID"},{"field_key":"issue_reporter","field_value":"[\"userkey1\"]"},{"field_key":"role_owners","field_value":"[{\"role\":\"operator\",\"owners\":[\"userkey1\"]}]"}]' --project-key 空间key --work-item-id {{work_item_id}} --ignore-required {{ignore_required}} --ignore-role-calculate {{ignore_role_calculate}} --format json
```

> 🚨 `issue_reporter`（multi-user 类型的内置角色字段）和 `role_owners`（统一角色入口）是**两种可互换的写法**：前者走 meta-create-fields 返回的字段 key；后者用 meta-roles 返回的 role_id（如 `operator` / `reporter`，不含 `issue_` 前缀）。两者的 `field_value` 都必须是 **stringified JSON** 字符串。

通过资源工作项模板实例创建：

```bash
meegle workitem create --work-item-type story --fields '{{fields}}' --project-key 空间key --work-item-id 资源模板实例ID --ignore-required {{ignore_required}} --ignore-role-calculate {{ignore_role_calculate}} --format json
```

### workitem update
更新普通字段：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --role-operate '{{role_operate}}' --fields '[{"field_key": "priority", "field_value": "option_id"}]' --format json
```

更新 multi-user 字段（复合值 stringified）：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --role-operate '{{role_operate}}' --fields '[{"field_key": "current_status_operator", "field_value": "[\"userkey1\",\"userkey2\"]"}]' --format json
```

普通复合字段新增一行（`field_value` 是 stringified action 对象）：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --role-operate '{{role_operate}}' --fields '[{"field_key":"复合字段key","field_value":"{\"action\":\"add\",\"fields\":[[{\"field_key\":\"子字段key\",\"field_value\":\"示例值\"}]]}"}]' --format json
```

普通复合字段更新 / 删除一行（`group_uuid` 必须先读取获得）：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --role-operate '{{role_operate}}' --fields '[{"field_key":"复合字段key","field_value":"{\"action\":\"update\",\"group_uuid\":\"读回的组标识\",\"fields\":[[{\"field_key\":\"子字段key\",\"field_value\":\"新值\"}]]}"}]' --format json
```

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --role-operate '{{role_operate}}' --fields '[{"field_key":"复合字段key","field_value":"{\"action\":\"delete\",\"group_uuid\":\"读回的组标识\"}"}]' --format json
```

多人复合字段更新已有人员并整体覆盖（必须先读取旧值并保留全部人员和非目标子字段；不能用来新增人员）：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --role-operate '{{role_operate}}' --fields '[{"field_key":"多人复合字段key","field_value":"{\"userkey1\":[{\"field_key\":\"子字段key\",\"field_value\":\"示例值\"}],\"userkey2\":[]}"}]' --format json
```

> 若当前值为空或目标 userkey 不在读回 map 中，停止自动更新并请用户先通过页面配置人员范围；接口空成功后仍必须回读确认。

更新拉群方式（`group_type` 逻辑字段，统一替代旧 `group_id` / `chat_group`）——三种形态：

切到自动拉群（不带 `group_id`）：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --role-operate '{{role_operate}}' --fields '[{"field_key":"group_type","field_value":"{\"type\":\"auto\"}"}]' --format json
```

绑定现有群（`type=bind` 必须带非空 `group_id`）：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --role-operate '{{role_operate}}' --fields '[{"field_key":"group_type","field_value":"{\"type\":\"bind\",\"group_id\":\"oc_xxx\"}"}]' --format json
```

关闭拉群：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --role-operate '{{role_operate}}' --fields '[{"field_key":"group_type","field_value":"{\"type\":\"disabled\"}"}]' --format json
```

---

## 人员域

### user search
```bash
meegle user search --user-keys '["张三", "李四"]' --project-key {{project_key}} --need-all-status {{need_all_status}} --format json
```

包含离职/停用等非在职用户：

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
