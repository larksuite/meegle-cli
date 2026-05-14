# MQL 语法规范参考

> **重要：`workitem query` 的 `mql` 参数必须是完整的 SQL 查询语句**
> - 必须包含 `SELECT` 和 `FROM` 子句
> - 不接受 JSON 对象、简写条件或不完整片段
> - 正确: \`SELECT \`工作项ID\`, \`名称\` FROM \`project_key\`.\`需求\` WHERE \`状态\` = \`进行中\`\`
> - 错误: `{"status": "进行中"}` / `status = '进行中'` / `WHERE status = '进行中'`

## 基础语法

```sql
SELECT fieldList                            -- 指定查询的字段列表
FROM `空间名`.`工作项类型名`                   -- 指定数据来源
WHERE conditionExpression                    -- 查询条件（可选）
[ORDER BY fieldOrderByList [{ASC|DESC}]]     -- 排序（可选）
[LIMIT [offset,] row_count]                  -- 分页（可选）
```

**标识符规则**：
- **不支持 `SELECT *`，必须显式指定字段**
- 所有字段名和表名必须使用反引号包裹，如 `` `工作项ID` ``、`` `空间名`.`需求` ``，**带 target 修饰符时必须包裹整个字段**：`` `name<target:all>` ``
- SELECT/FROM/WHERE/ORDER BY 中既可使用 key 也可使用名称。**优先使用 key**，从 `list_workitem_field_config` 返回值中获取：系统字段 key 为单词（如 `priority`、`status`），自定义字段 key 为 `field_x23bd` 格式，工作项类型 key 如 `story`、`issue`。名称是用户自定义的 UGC 内容（语言不定），仅作为找不到 key 时的兜底
- 字符串用单引号：`'value'`
- 数组用 JSON 格式：`'["a","b"]'`
- 枚举值优先用 label（如 `'通过'`），不要用 id
- **禁止** `count()`、`SUM()`、`GROUP BY`。总数从返回结果的 `count` 字段读取

---

## 数据类型

| MQL 类型 | 说明 | 对应的工作项字段类型 |
|-----------|------|-------------------|
| bool | 真假值，取值 TRUE/FALSE/1/0 | bool |
| bigint | 整数类型 | number 类型下的 work_item_id 和 auto_number |
| double | 浮点数类型 | 除 work_item_id/auto_number 外的其他 number |
| varchar | 字符串类型 | text、multi-pure-text、multi-text、select、tree-select、radio、user、link、signal、workitem_related_select |
| date | 日期类型，格式 `YYYY-MM-DD` 或 `YYYY-MM-DD+TZD`（如 `2025-12-24+08:00`） | date |
| datetime | 日期时间类型，格式 `YYYY-MM-DDThh:mm:ss` 或 `YYYY-MM-DDThh:mm:ssTZD` | schedule、precise_date |
| array(varchar) | 字符串数组 | multi-select、tree-multi-select、multi-user、link_cloud_doc、workitem_related_multi_select、multi-file |
| array(struct) | 结构体数组 | compound_field |
| lambda expression | 返回 bool 的函数表达式，写法：`x -> x > 10`、`x -> x in ('a', 'b')` | — |

---

## 常用运算符

### BETWEEN ... AND ...

时间区间查询，仅适用于 date 字段类型和 precise_date 字段类型

```sql
WHERE `创建时间` BETWEEN '2025-01-01' AND '2025-10-01'
```

### IN

集合查询，适用于 varchar 数据类型

```sql
-- 查询名称为 "测试1" 或 "测试2" 或 "测试3"
WHERE `名称` IN ('测试1', '测试2', '测试3')
```

### LIKE / NOT LIKE

模糊匹配，`%` 匹配任意字符：

```sql
WHERE `缺陷名` LIKE '%性能问题%'
WHERE `缺陷名` NOT LIKE '%后端性能问题%'
```

---

## 常用函数

### 数组函数

| 函数 | 说明 |
|------|------|
| `array_cardinality(array_col)` | 获取数组长度 |
| `array_contains(array_col, element [, element2, ...])` | 数组是否包含某元素（多值表示包含其中之一） |
| `any_match(array_col, predicate)` | 是否有任一元素满足条件 |
| `all_match(array_col, predicate)` | 是否所有元素满足条件 |
| `none_match(array_col, predicate)` | 是否所有元素都不满足条件 |
| `array_filter(array_col, predicate)` | 根据条件过滤数组，返回新数组 |

**示例**：
```sql
-- 当前负责人包含张三
array_contains(`当前负责人`, '张三')

-- 标签数组与给定数组有交集
array_intersect(`标签`, '["标签A","标签B"]') 

-- 处理人中是否有张三
any_match(`处理人`, x -> x = '张三')

-- 处理人中是否有至少一个开放平台团队的用户
any_match(`处理人`, x -> x in (team(true, '开放平台团队')))

-- 优先级包含 P0 或 P1
array_contains(`优先级`, 'P0', 'P1')

-- 当前负责人包含当前登录用户或李四
any_match(`当前负责人`, x -> x in (current_login_user(), '李四'))

-- 所有处理人都在后端团队中
all_match(`处理人`, usr -> usr in team(true, '后端开发团队'))

-- 标签数组为空
array_cardinality(`标签`) = 0
```

### 时间函数

支持的函数：`RELATIVE_DATETIME_EQ`、`RELATIVE_DATETIME_GT`、`RELATIVE_DATETIME_GE`、`RELATIVE_DATETIME_LT`、`RELATIVE_DATETIME_LE`、`RELATIVE_DATETIME_BETWEEN`

函数签名：`RELATIVE_DATETIME_*(col_name, 'date_para', ['days'])`

**date_para 枚举值**：

| 枚举 | 含义 | 是否支持 days 参数 |
|------|------|-------------------|
| today | 当天 | 支持（正值向后偏移，负值向前偏移） |
| tomorrow | 明天 | 不支持 |
| yesterday | 昨天 | 不支持 |
| current_week | 当周 | 不支持 |
| next_week | 下周 | 不支持 |
| last_week | 上周 | 不支持 |
| current_month | 当月 | 不支持 |
| next_month | 下月 | 不支持 |
| last_month | 上月 | 不支持 |
| future | 从今天起的未来范围 | 支持 |
| past | 从今天起的过去范围 | 支持 |

**days 参数**：仅 `today`、`future`、`past` 支持。格式为 `'Nd'` 或 `'-Nd'`。

**示例**：
```sql
-- 今天创建的工作项
RELATIVE_DATETIME_EQ(`创建时间`, 'today')

-- 3天内到期的工作项
RELATIVE_DATETIME_LE(`截止时间`, 'future', '3d')

-- 上周创建的需求
RELATIVE_DATETIME_BETWEEN(`创建时间`, 'last_week')

-- 本月更新的任务
RELATIVE_DATETIME_BETWEEN(`更新时间`, 'current_month')

-- 今天后 3 天
RELATIVE_DATETIME_EQ(`创建时间`, 'today', '3d')

-- 今天前 3 天
RELATIVE_DATETIME_EQ(`创建时间`, 'today', '-3d')

-- 排期开始时间在过去 30 天内
RELATIVE_DATETIME_BETWEEN(`__需求排期_开始时间`, 'past', '30d')
```

### 人员与角色函数

| 函数 | 说明 |
|------|------|
| `current_login_user()` | 返回当前登录用户的 userkey |
| `team(include_manager, '团队名')` | 返回团队成员 userkey 数组（第一个参数 true 表示包含管理者） |
| `all_participate_persons()` | 返回所有参与当前工作项的人员 userkey 数组 |
| `participate_roles()` | 返回所有参与角色的 rolekey 数组（如 RD、QA、PM） |

**示例**：
```sql
-- 当前负责人是当前登录用户
array_contains(`当前负责人`, current_login_user())

-- 返回当前工作项的参与人 userkey 数组是否包含张三
array_contains(participate_persons(), '张三')

-- 返回所有参与当前工作项的人员（全部参与人员/全部人员）userkey 数组是否包含李四
array_contains(all_participate_persons(), '李四')

-- 指派给产品团队（含管理者）
any_match(`当前负责人`, x -> x in team(true, '产品团队'))

-- 查询有 RD 和 QA 角色参与的工作项
array_contains(participate_roles(), 'RD', 'QA')
```

### 节点函数

- **all_nodes_name()**：返回所有流程节点名称数组
  ```sql
  -- 流程节点包含"开始"
  WHERE array_contains(all_nodes_name(), '开始')
  ```

- **in_progress_nodes_name()**：返回当前进行中的节点名称数组
  ```sql
  -- 进行中节点不为空
  WHERE in_progress_nodes_name() is not null
  ```

- **risk_label()**：返回节点延期状态标识数组（如 `["延期/开始","今日到期/结束"]`）
  ```sql
  -- 开始节点已延期且结束节点今日到期
  WHERE risk_label() = '["延期/开始","今日到期/结束"]'
  ```

- **get_node_attribute(node, attribute)**：获取指定节点的属性值。node 可为节点名、`__ALL`（全部节点）、`__BELONGING`（所属节点，即当前工作项所在的节点）。**语义涉及"所属节点"时，必须使用 `__BELONGING`**

  **可用属性**：排期、估分、节点时间、节点完成结论、节点完成意见、负责人、当前负责人、状态

  **排期/节点时间子字段**：
  - 节点排期：`get_node_attribute('节点名','__排期_开始时间')`、`get_node_attribute('节点名','__排期_结束时间')`
  - 节点时间：`get_node_attribute('节点名','__节点时间_开始时间')`、`get_node_attribute('节点名','__节点时间_结束时间')`

  ```sql
  -- 开始节点排期在过去 30 天内
  WHERE RELATIVE_DATETIME_BETWEEN(get_node_attribute('开始','__排期_开始时间'), 'past','30d')
  -- 全部节点估分大于 10
  WHERE get_node_attribute('__ALL','估分') > 10
  -- 调研节点负责人等于小李（等于语法也适用于节点属性）
  WHERE get_node_attribute('调研','负责人') = '小李'
  -- 所属节点当前负责人（必须使用 __BELONGING）
  WHERE any_match(get_node_attribute('__BELONGING','当前负责人'), x -> x in ('张三'))
  -- 所属节点状态为进行中
  WHERE any_match(get_node_attribute('__BELONGING','状态'), x -> x in ('进行中'))
  -- 所属节点排期在未来 7 天内
  WHERE RELATIVE_DATETIME_BETWEEN(get_node_attribute('__BELONGING','排期'), 'future','7d')
  ```

### 关系函数

- **relation(relation_name)**：通过关系名称获取关系，关系名称从 `list_workitem_relations` 获取
  ```sql
  WHERE any_relation_match(relation('父-子'), x -> x.`名称` like '%需求%')
  ```

- **parent_work_item(relation(relation_name))**：获取父工作项 ID
  ```sql
  WHERE parent_work_item(relation('父-子')) = '12345'
  ```

- **relation_field_chain(relation1 [, relation2 [, relation3]])**：关联工作项链式查询（最多 3 跳）。一级关系使用 `relation_field_chain('关系1')` 或 `relation_field_chain('字段1')` 即可。**子任务的父工作项必须写作 `'__父工作项'`（双下划线前缀）**
  ```sql
  -- 一级关系
  WHERE any_relation_match(relation_field_chain('关联需求'), x -> x.`优先级` = 'P0')
  -- 多级关系：子任务→父工作项→软件
  WHERE any_relation_match(relation_field_chain('__父工作项','需求关联软件'), x -> x.`名称` = '某软件')
  ```

- **association()**：跨空间关联实例 ID
  ```sql
  WHERE association() = '实例ID'
  ```

- **linked_work_item()**：子任务特有，获取来源控件（父工作项 ID）
  ```sql
  WHERE linked_work_item() = '12345'
  ```

### 关系判断函数

用于对关系对端进行条件判断：

- **any_relation_match(relation, x -> expr)**：存在一个/一组对端满足条件
- **all_relation_match(relation, x -> expr)**：每一个对端都满足条件
- **none_relation_match(relation, x -> expr)**：每一个对端都不满足条件
- **not all_relation_match(relation, x -> expr)**：存在一个/一组对端不满足条件

**嵌套筛选规则**：对关系对端的属性进行进一步筛选时，使用关系函数（如 `relation_field_chain`）获取关系后，外层必须嵌套关系判断函数。

```sql
-- 示例：子任务其父工作项的当前负责人全部不属于小李
WHERE all_relation_match(relation_field_chain('__父工作项'), x -> none_match(x.`当前负责人<target:all>`, y -> y in ('小李')))

-- 示例：多级关系 + 节点属性 — 子任务关联的任务所关联的需求的"开始"节点负责人包含小李
WHERE all_relation_match(relation_field_chain('需求-二级工作项关联','二级工作项-子任务关联'), x -> any_match(get_node_attribute('开始','负责人'), y -> y in ('小李')))
```

**关系参数三种形式**：

1. **关联字段**：`` `字段key` `` — 如 ``any_relation_match(`关联需求`, x -> x.`优先级` = 'P0')``
2. **relation 函数**：`relation('关系名')` — 如 `any_relation_match(relation('父-子'), x -> x.`名称` like '%需求%')`
3. **relation_field_chain 函数**：`relation_field_chain('关系1','关系2')` — 用于多级关系链

**多级关系名处理规则**：默认按照用户的输入直接进行查询。如果找不到匹配的关系，可让用户二次输入确认。不主动猜测或转换用户输入的关系名。

**子任务 `__父工作项` 规则**：
- 子任务的父工作项关系名固定为 `'__父工作项'`（双下划线前缀），不是 `'父工作项'`
- 常见报错：`object[父工作项-关联XXX]: relation chain err:relationNode not found, label:父工作项`
- 原因：父工作项写法缺少双下划线前缀
- **修复：仅将 `'父工作项'` 改为 `'__父工作项'`**，其他部分保持不变
- 修正示例：`relation_field_chain('__父工作项','关联XXX')`

**跨空间/多端字段引用**：``x.`字段名<target:空间key::工作项类型key>` `` 或 `` x.`字段名<target:all>` ``（通用字段）

**关联工作项通用字段**（查询关联工作项信息时，以下属性必须使用 `<target:all>`）：标题、创建人、创建时间、业务线、优先级、当前负责人、所属工作项、所属空间、工作项ID、工作项类型、状态

```sql
-- 多选关联字段：对端优先级为 P0（使用 <target:all>）
WHERE any_relation_match(`多选关联字段`, x -> x.`优先级<target:all>` = 'P0')
-- 多选关联字段全部满足条件（使用指定空间和类型）
WHERE all_relation_match(`多选关联字段`, x -> x.`描述<target:空间key::类型key>` like '%123%')
-- 关联工作项创建时间（通用字段必须用 <target:all>）
WHERE any_relation_match(relation_field_chain('__父工作项'), x -> x.`创建时间<target:all>` >= '2026-03-24')
-- 多级关系结合节点属性（__父工作项要指明 target 才可以串起来关系）
WHERE all_relation_match(relation_field_chain('__父工作项<target:681b228d7401707f87414afa::story>','关联字段2'), x -> any_match(get_node_attribute('开始','负责人'), y -> y in ('小李')))
-- 关联工作项的节点属性：XX 关联的实例，其 AA 节点负责人不包含小李
WHERE any_relation_match(`XX关联`, x -> not array_contains(get_node_attribute('AA', '负责人'), '小李'))
```

### 状态函数

- **status_time(status_name)**：返回进入指定状态的时间
  ```sql
  -- 状态时间在区间内
  WHERE status_time('开始') between '2025-03-10' and '2025-04-10'
  ```

- **status_time('\_\_状态名\_\_开始时间') / status_time('\_\_状态名\_\_结束时间')**：获取状态的开始/结束时间
  ```sql
  -- 状态累计持续时间
  WHERE status_time('__结束状态__结束时间') - status_time('__开始状态__开始时间') > 86400
  ```

---

## 名称消歧（`<id:xxxx>` 语法）

当人名、团队名等存在重复时，MQL 会因无法唯一标识而报错。此时使用 `<id:xxxx>` 语法指定唯一 ID：

```sql
-- 人名消歧：张三对应多人时，指定 userkey
WHERE `创建人` = '张三<id:1234>'

-- 团队名消歧：指定团队唯一 key
WHERE any_match(`负责人`, x -> x in (team(true, '开放平台团队<id:3455>')))
```

遇到 MQL 返回"名称重复"类错误时，需获取对应的唯一 ID 后使用此语法重试。

**枚举值和 userkey 也可使用 `<id:xxx>` 格式**：

```sql
-- 枚举值指定 option id
WHERE x.`priority` = '<id:option_2>'
-- userkey 指定
WHERE `负责人` = '<id:7290442683267497985>'
```

---

## 特殊字段查询规则

### 日期区间类型字段

日期区间类型（如"需求排期"）不能直接查询，必须拆分为子字段，格式：`` `__排期名_开始时间` `` / `` `__排期名_结束时间` ``

```sql
-- 正确：使用子字段
WHERE `__开发周期_开始时间` > '2025-01-01' AND `__开发周期_结束时间` < '2025-01-31'
WHERE RELATIVE_DATETIME_BETWEEN(`__需求排期_开始时间`, 'past', '30d')

-- 错误：直接查询日期区间字段
WHERE RELATIVE_DATETIME_BETWEEN(`需求排期`, 'past', '30d')
```

### 角色字段

角色可作为 MQL 属性查询，使用 `__角色名` 格式（加 `__` 前缀以区分普通自定义字段）：

```sql
-- 查询 RD 包含某人的工作项
WHERE array_contains(`__RD`, '张三')

-- 查询多个角色条件
WHERE array_contains(`__RD`, '张三') AND array_contains(`__PM`, '李四')
```

---

## 关键词映射

> 当用户输入中包含以下关键词时，优先匹配对应的函数或语法。

### 控件关键词 → 函数映射

**当用户输入中包含「控件」二字时，务必对照下表选择正确函数。**

| 用户关键词 | 使用函数/语法 | 说明 |
|-----------|-------------|------|
| 参与人员、全部参与人员、全部人员 | `all_participate_persons()` | 当前工作项的全部参与人 |
| 参与人员、当前参与人 | `participate_persons()` | 当前工作项的参与人 |
| 流程节点、所有节点 | `all_nodes_name()` | 获取所有节点名称 |
| 进行中节点 | `in_progress_nodes_name()` | 获取当前进行中的节点 |
| 节点排期、节点时间、节点估分、所属节点信息 | `get_node_attribute('节点名\|__ALL\|__BELONGING','属性名')` | 获取节点具体属性，所属节点用 `__BELONGING` |
| 节点延期标识 | `risk_label()` | 获取节点延期状态标识 |
| 关联工作项信息 | `relation_field_chain('关联字段1','关联字段2','关联字段3')` | 关联工作项链式查询（最多 3 跳） |
| 子任务父工作项 | `relation_field_chain('__父工作项')` | 子任务特有：获取父工作项 |
| 关系 | `relation('关系名')` | 通过关系名称获取 |
| 来源 | `linked_work_item()` | **子任务特有**，获取来源控件 |
| 父工作项 | `parent_work_item(relation('关系名'))` | 获取父工作项 |
| 状态时间窗口 | `status_time('状态名') between 'a' and 'b'` | 状态时间在区间内 |
| 状态累计进行时间 | `status_time('__结束状态__结束时间') - status_time('__开始状态__开始时间')` | 计算状态累计持续时间 |

### 操作符关键词 → 语法映射

| 用户关键词 | MQL 语法 | 示例 |
|-----------|---------|------|
| 存在选项属于 | `any_match(field, x -> x in ('a','b'))` | 多选字段中存在任一选项 |
| 全部选项均不属于 | `none_match(field, x -> x in ('a','b'))` | 多选字段中不存在任何选项 |
| 包含 | `array_contains(field, 'a','b')` | 数组包含指定值 |
| 不包含 | `not array_contains(field, 'a','b')` | 数组不包含指定值 |
| 等于 | `field = 'value'` | 等于指定值 |
| 不等于 | `field != 'value'` | 不等于指定值 |
| 为空 | `field is null` | 字段为空 |
| 不为空 | `field is not null` | 字段不为空 |
| 在区间 | `field between 'a' and 'b'` | 字段在区间内（日期/数字） |

### 关系语境关键词 → 函数映射

| 用户关键词 | 使用函数 | 说明 |
|-----------|---------|------|
| 每一个 | `all_relation_match(关系, x -> expr)` | 所有关系对端都满足条件 |
| 存在一个、存在一组 | `any_relation_match(关系, x -> expr)` | 任意关系对端满足条件 |
| 每一个不满足 | `none_relation_match(关系, x -> expr)` | 所有关系对端都不满足条件 |
| 存在一个不满足、存在一组不满足 | `not all_relation_match(关系, x -> expr)` | 并非所有都满足（即至少有一个不满足） |

---

## 完整查询示例

> 以下示例展示语法模式。`<尖括号>` 为占位符，实际值需从对应工具获取：字段 key 从 `list_workitem_field_config`，工作项类型从 `list_workitem_types`，状态/优先级等选项值从字段配置的 options 中读取。

### 示例 1：数组包含 + 当前用户

```sql
SELECT <所需字段列表>
FROM `空间名`.`<工作项类型>`
WHERE array_contains(`<数组字段>`, '<匹配值>')
  AND array_contains(all_participate_persons(), current_login_user())
```

### 示例 2：相对时间查询

```sql
SELECT <所需字段列表>
FROM `空间名`.`<工作项类型>`
WHERE RELATIVE_DATETIME_BETWEEN(`<日期字段>`, 'past', '<天数>d')
```

### 示例 3：逾期未完成（排期子字段 + 状态过滤）

```sql
-- 排期子字段格式：`__<排期名>_开始时间` / `__<排期名>_结束时间`，具体名称从 list_workitem_field_config 确认
SELECT <所需字段列表>
FROM `空间名`.`<工作项类型>`
WHERE RELATIVE_DATETIME_LT(`__<排期名>_结束时间`, 'today')
  AND `status` != '<已完成状态值>'
```

### 示例 4：团队角色匹配

```sql
SELECT <所需字段列表>
FROM `空间名`.`<工作项类型>`
WHERE any_match(`__<角色名>`, x -> x in (team(true, '<团队名>')))
```

### 示例 5：等值条件 + 排序分页

```sql
SELECT <所需字段列表>
FROM `空间名`.`<工作项类型>`
WHERE `<字段>` = current_login_user()
  AND `<字段>` = '<条件值>'
ORDER BY `<排序字段>` DESC
LIMIT <按需设置>
```

### 示例 6：模糊匹配 + 多条件组合

```sql
SELECT <所需字段列表>
FROM `空间名`.`<工作项类型>`
WHERE `name` LIKE '%<关键词>%'
  AND array_contains(`<数组字段>`, '<匹配值>')
  AND `<字段>` = current_login_user()
ORDER BY `<排序字段>` ASC
LIMIT <按需设置>
```

### 示例 8：节点属性查询（所属节点 + 延期标识）

```sql
-- 所属节点当前负责人是张三且进行中
SELECT `工作项ID`, `名称`, `状态`
FROM `project_key`.`需求`
WHERE any_match(get_node_attribute('__BELONGING','当前负责人'), x -> x in ('张三'))
  AND any_match(get_node_attribute('__BELONGING','状态'), x -> x in ('进行中'))

-- 开始节点已延期
SELECT `工作项ID`, `名称`
FROM `project_key`.`需求`
WHERE risk_label() = '["延期/开始"]'
```

### 示例 9：关系查询（关系链 + 跨空间字段）

```sql
-- 子任务的父工作项名称包含"登录"
SELECT `工作项ID`, `名称`
FROM `project_key`.`子任务`
WHERE any_relation_match(relation_field_chain('__父工作项'), x -> x.`标题<target:all>` like '%登录%')

-- 多级关系：子任务→父工作项→关联软件，软件名称等于某值
SELECT `工作项ID`, `名称`
FROM `project_key`.`子任务`
WHERE any_relation_match(relation_field_chain('__父工作项','需求关联软件'), x -> x.`名称` = '某软件')
```

### 示例 10：状态时间查询

```sql
-- "开始"状态时间在 2025-03-10 至 2025-04-10 之间
SELECT `工作项ID`, `名称`, `状态`
FROM `project_key`.`需求`
WHERE status_time('开始') between '2025-03-10' and '2025-04-10'
```

### 示例 11：节点负责人 + 人员综合查询

```sql
-- 开始节点负责人包含某人
SELECT `工作项ID`, `名称`
FROM `project_key`.`需求`
WHERE array_contains(get_node_attribute('开始','负责人'), '李应凡')

-- 处理人属于某团队
SELECT `工作项ID`, `名称`, `处理人`
FROM `project_key`.`缺陷`
WHERE any_match(`处理人`, x -> x in (team(true, '开放平台团队')))
```
