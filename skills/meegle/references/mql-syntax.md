# MQL 语法规范

> `workitem query` 的 `--mql` 参数必须是完整 SQL 语句，包含 `SELECT` + `FROM`。禁止 JSON 对象、条件片段、缺 SELECT 的写法。

---

## 1. 硬规则

### 1.1 禁用语法

| 禁止 | 替代 | 原因 |
|------|------|------|
| `SELECT *` | 显式列出字段 | 不支持通配符 |
| `count(*)` / `SUM()` / `GROUP BY` | 从返回 `count` 字段读总数 | 不支持聚合 |
| `REGEXP` / `regexp_like()` | `LIKE '%...%'` | 不支持正则 |
| `currentUser()` / `current_user()` | `current_login_user()` | 函数名错误 |
| `CONTAINS(field, val)` | `array_contains()` 或 `LIKE` | 无此运算符 |
| 日期无引号 `2026-06-01` | `'2026-06-01'` | 会被解析为减法 |

### 1.2 不推荐但服务端受理

以下语法**服务端行为不稳定**（部分字段类型直接报 `operator not supported`），一律禁止生成：

| 语法 | 服务端行为 | 强制改写为 |
|------|-----------|-----------|
| `NOT BETWEEN a AND b` | 语法受理 | `< a OR > b` |

### 1.3 不可查询的字段类型

以下字段类型出现在 `SELECT` / `WHERE` 中会报 `unsupported field type`，必须移除并改用 `workitem get`：

- `attachment` / `file`（附件）
- `spec_doc` / `specDocs` / `spec_documents`（文档）

**例外**：`multi-file`（多文件，如 `multi_attachment`）**仅支持** `SELECT` / `IS NULL` / `IS NOT NULL`，返回 `key_label_value_list`。不支持 `LIKE` / `array_contains` 等深度筛选。

### 1.4 常见字段名易错映射

MQL 支持字段 key 和中文名/label，**必须优先 key**。中文名多存在同名歧义。

| ❌ 写法 | ✅ 替代 |
|--------|--------|
| `state_key` / `status` | `work_item_status` |
| `archiving_status` / `is_archived` | `archiving_date`（未归档用 `IS NULL`） |
| 中文名如「名称」「当前负责人」「创建时间」 | 先 `workitem meta-fields` 查 key |

除上表外，中文名一律先用 `workitem meta-fields` 查询，并同时传 `--project-key` 与 `--work-item-type`。

### 1.5 枚举值/状态值禁止硬编码

状态、select、tree-select 等枚举 label 由「空间 + 工作项类型」自定义。**禁止硬编码** `关闭`、`已完成`、`进行中`、`OPEN`、`CLOSED` 等。违反报 `attrValueLabel not found`。

**流程**：`workitem meta-fields` 取 options → 用真实 label 或 `<id:option_id>` 写入。

### 1.6 LIKE 转义

- LIKE 通配符**仅 `%`（任意长度）**。`_` **不是**通配符：未转义会被服务端强制拒回 `Internal % and _ characters must be escaped`（含 `%foo_bar%` / `%test_case%` 等所有含裸 `_` 的模式）
- 字面量 `_` 或"任意单字符"语义**必须**写 `\_`；同理 `%` 若作字面量必须 `\%`
- 模式必须完整包含形态 `'%...%'`

```sql
✅ WHERE `name` LIKE '%性能问题%'
✅ WHERE `name` LIKE '%test\_case%'       -- 字面量下划线必须转义
✅ WHERE `name` LIKE '%100\%完成%'
❌ WHERE `name` LIKE '100%完成'           -- 缺前 %
❌ WHERE `name` LIKE '%test_case%'        -- 裸 _ 未转义，服务端拒回
❌ WHERE `name` LIKE '%100%完成%'         -- 内部 % 未转义
```

### 1.7 字符串编码

- `--mql` 参数外层用单引号包裹
- MQL 内部字符串值用单引号；嵌套单引号用 `''`（两个单引号）
- **禁止**用 `\` 转义单引号（服务端语法接受但**不做转义**：`'张\'三'` 会被当作字面量 `张\`，导致空结果）
- 中文字段名/值放在反引号或单引号内

### 1.8 多值右值语法

多值右值**统一用元组** `(v1, v2)`，适用于 `IN` / `NOT IN` / `array_contains` / `=` / lambda 内的 `IN`。**不推荐** JSON 数组字符串 `'["a","b"]'`：`IN` 直接 syntax error；`=` 服务端当前兼容但非推荐写法，统一用元组。

**唯一例外**：控件函数场景要求 JSON 数组字符串——`array_intersect(<控件函数>, '["a","b"]')`（有交集语义）与 `risk_label() = '["a","b"]'`（集合完全相等语义）。

---

## 2. 基础语法

```sql
SELECT fieldList                          -- 字段列表，禁 *
FROM `project_key`.`work_item_type`       -- 必须双段完整
WHERE conditionExpression                 -- 可选
[ORDER BY field [ASC|DESC]]
[LIMIT [offset,] row_count]   -- 也支持 LIMIT row_count OFFSET n
```

**标识符**：

- 字段/表名推荐反引号：`` `work_item_id` `` / `` `project_key`.`story` ``
- `<target:xxx>` 修饰符**必须整体放在同一对反引号内**：`` `name<target:all>` ``。写成 `` `name`<target:all> `` 或 `name<target:all>` 报 `syntax error near ':...'`
- `<target:xxx>` **仅允许在关系判断 lambda 内部**（`` x.`name<target:all>` `` 形式）。顶层 SELECT/WHERE 直接引用主表字段禁止使用，否则报 `attribute[name<target:all>] not found`
- 字符串值用单引号；枚举值优先用 label（必须先经 workitem meta-fields 确认存在），找不到时用 `<id:option_id>`

---

## 3. 数据类型

| MQL 类型 | 对应字段类型 |
|---------|-------------|
| bool | bool |
| bigint | number 中的 `work_item_id`、`auto_number` |
| double | 其它 number |
| varchar | text、multi-pure-text、multi-text、select、tree-select、radio、user、link、signal、workitem_related_select |
| date | date（格式 `YYYY-MM-DD` 或 `YYYY-MM-DD+TZD`） |
| datetime | schedule、precise_date（格式 `YYYY-MM-DDThh:mm:ss[TZD]`） |
| array(varchar) | multi-select、tree-multi-select、multi-user、link_cloud_doc、workitem_related_multi_select |
| array(struct) | compound_field |
| lambda | `x -> x IN ('a','b')` 等 |

---

## 4. 运算符与字段类型兼容性

**唯一权威表**。违反本表报 `field not supported OPERATOR` / `operator not supported`。

| 字段类型 | 支持 | 不支持 |
|---------|------|--------|
| text / multi-pure-text | `=` `!=` `IN` `NOT IN` `LIKE` `NOT LIKE` `IS NULL` `IS NOT NULL` | 比较符、`BETWEEN`、`array_contains` |
| multi-text | `LIKE` `NOT LIKE` `IS NULL` `IS NOT NULL` | 其它 |
| select / radio / workitem_related_select | `=` `!=` `IN` `NOT IN` `IS NULL` `IS NOT NULL` | `LIKE`、比较符、`BETWEEN`、`array_contains`、`any_match` |
| tree-select（单选） | `=` `!=` `IN` `NOT IN` `IS NULL` `IS NOT NULL` | `LIKE`、比较符、`BETWEEN`、`array_contains`、`any_match`（不支持父级级联，仅按叶子精确匹配） |
| user | `=` `!=` `IN` `NOT IN` `array_contains` `IS NULL` `IS NOT NULL` | `LIKE`、比较符、`BETWEEN` |
| link | `=` `!=` `LIKE` `NOT LIKE` `IS NULL` `IS NOT NULL` | `IN`、比较符、`BETWEEN`、`array_contains` |
| signal | `=` `!=` `IN` `NOT IN`（值位见下） | `IS NULL` / `IS NOT NULL` 报 `operator not supported`；其它 |
| number（含 `work_item_id`） | `=` `!=` `IN` `NOT IN` `>` `>=` `<` `<=` `IS NULL` `IS NOT NULL` | `LIKE`、`BETWEEN`、`array_contains` |
| array(varchar) | `=` `!=` `IN` `NOT IN` `array_contains` `NOT array_contains` `any_match` `none_match` `IS NULL` `IS NOT NULL` | `LIKE`、比较符、`BETWEEN` |
| date | `=` `!=` `>` `>=` `<` `<=` `BETWEEN` `RELATIVE_DATETIME_*` `IS NULL` `IS NOT NULL` | `LIKE`、`IN`、`array_contains` |
| datetime（`schedule` / `precise_date`） | `>` `>=` `<` `<=` `BETWEEN` `RELATIVE_DATETIME_*`（需先拆为子字段） `IS NULL` `IS NOT NULL` | `=` `!=`、`LIKE`、`IN`、`array_contains`；必须拆为 `` `__字段key_开始时间` `` / `` `__字段key_结束时间` `` 访问（见 §13.1） |
| bool | `=` `!=` `IS NULL` `IS NOT NULL` | `IN`、`LIKE`、`BETWEEN`、`array_contains`、比较符 |
| array(struct)（`compound_field`） | 不可直接在 WHERE 中比较 | 需通过子字段或 `workitem get` 读取 |
| `multi-file` | `SELECT` / `IS NULL` / `IS NOT NULL` | `LIKE`、比较符、`BETWEEN`、`array_contains`、`IN` |

**signal 值位（MQL 查询）**：仅接受 `option_name` label（如 `'已通过'` / `'未通过'` / `'处理中'` / `'暂无信息'`）。禁 `option_id`（`'passed'`）、`<id:option_id>`、`'true'/'false'/'null'`。写入接口（create/update/transition）的值位规则不同，见 [SKILL.md「字段值格式」](../SKILL.md)。

**数组字段语义**：`=` / `!=` 按整组精确匹配（数组完全等于右值集合）；`IN` 按元素级 OR（存在任一即匹配）；`array_contains` 多参数为 AND（同时包含所有元素）；`any_match` 按逐元素匹配。

**树状字段父级匹配**：`tree-multi-select`（数组型）父级匹配（含所有下级）用 `any_match(field, x -> x = '<父级 label>')` 或 `` `field` IN ('<父级 label>') ``；`array_contains` 仅做精确 label 匹配、**不含子级级联**。`tree-select`（单选）不支持父级级联，仅按叶子 label / option_id 精确匹配。

**tree-multi-select / workitem_related_multi_select 右值形式**：仅接受 label 或 `<id:option_id>` / `<id:work_item_id>` 包裹形式。裸 `option_id` / `work_item_id` 拒回 `metadata error`。详见 §5.1。

> **`workitem_related_select`** 值位：`= '<id:work_item_id>'` 或真实 label 命中；裸 `work_item_id` 拒回 `attrValueLabel not found`；`IS NULL` / `IS NOT NULL` 可用。

---

## 5. 数组与集合函数

### 5.1 函数清单

| 函数 | 说明 |
|------|------|
| `array_contains(field, e1 [,e2,...])` | 数组包含元素；多参数为 AND（同时包含所有元素）。对 `tree-multi-select` / `workitem_related_multi_select` 字段，右值仅接受 label 或 `<id:option_id>` / `<id:work_item_id>` 包裹形式；裸 `option_id` / `work_item_id` 拒回 `attrValueLabel not found` 或 `metadata error` |
| `any_match(field, x -> pred)` | 任一元素满足；**仅当 lambda 值列表含函数（`team()` / `current_login_user()`）或多个 `<id:userkey>` 时使用** |
| `none_match(field, x -> pred)` | 全部元素都不满足 |
| `array_intersect(field, '[...]')` | 有交集；**仅用于控件函数返回值**（`risk_label()`、`all_nodes_name()`、`in_progress_nodes_name()`）；对普通多选字段会报 `array_intersect: invalid right_array`。**第二参数必须是 JSON 数组字符串** |

### 5.2 多选/数组字段决策顺序

1. 单元素 / 多元素 OR → `IN + 元组`
2. 多元素 AND（同时包含）→ `array_contains(field, 'a', 'b')`
3. 集合完全相等 → `` `field` = ('v1','v2') ``（元组，禁 JSON 数组字符串）
4. 全不属于 → `NOT IN` 或 `none_match`；`NOT array_contains` 语义为"不同时包含"（至少缺一个），非"全不属于"
5. **树状/级联字段父级匹配**（含所有下级）：`tree-multi-select`（数组型）用 `any_match(field, x -> x = '<父级 label>')` 或 `` `field` IN ('<父级 label>') ``；`array_contains` 仅精确匹配、不含子级级联。`tree-select`（单选）不支持父级级联，仅按叶子 label / option_id 精确匹配
6. 值列表含 `team()` / `current_login_user()` 等返回集合的函数，或需在集合内做多个 `<id:userkey>` 判断 → `any_match(x -> x IN (...))`

```sql
-- 单人命中（多人字段）
array_contains(`current_status_operator`, '<id:userkey>')
-- 多人 OR
`current_status_operator` IN ('<id:k1>', '<id:k2>')
-- 多标签 AND
array_contains(`tag`, '标签A', '标签B')
-- 空判断
`tag` IS NULL
-- 值列表含团队函数
any_match(`watchers`, x -> x IN (team(true, '真实团队名')))
```

---

## 6. 时间函数 `RELATIVE_DATETIME_*`

签名：`RELATIVE_DATETIME_{EQ|GT|GE|LT|LE|BETWEEN}(col_name, 'date_para', ['days'])`

**date_para**：`today` / `tomorrow` / `yesterday` / `current_week` / `next_week` / `last_week` / `current_month` / `next_month` / `last_month` / `future` / `past`。

### 6.1 函数 × date_para × days 兼容矩阵

不在允许列的组合服务端拒回 `unexpected operator for <para>` / `invalid argument`。

| 函数 | **推荐使用** date_para | days（`'Nd'` / `'-Nd'`） | 备注 |
|------|-----------------------|--------------------------|------|
| `_EQ` | `today` / `tomorrow` / `yesterday` | ❌ 不接受 | `_EQ + future/past` **无 offset** 时报 `invalid argument: relative datetime <para> expr must have 2 params`；带 offset 时服务端受理但语义模糊，**禁止生成**（`future`/`past` 语义应改用 `_BETWEEN`） |
| `_GT` / `_GE` / `_LT` / `_LE` | 仅 `today` | ✅ 接受，可正可负 | — |
| `_BETWEEN` | `current_week` / `next_week` / `last_week` / `current_month` / `next_month` / `last_month` / `future` / `past` | 仅 `future` / `past` 接受且**必须传** `'Nd'`；其它 date_para 不接受 days | ⚠️ 服务端**拒回** `today` / `tomorrow` / `yesterday`（`unexpected operator for today`），改用 `_EQ` |

### 6.2 示例

```sql
-- 今天创建
RELATIVE_DATETIME_EQ(`start_time`, 'today')
-- 上周创建
RELATIVE_DATETIME_BETWEEN(`start_time`, 'last_week')
-- 未来 3 天到期
RELATIVE_DATETIME_BETWEEN(`field_xxxxxx`, 'future', '3d')
-- 今天前后 N 天
RELATIVE_DATETIME_GT(`start_time`, 'today', '-3d')
RELATIVE_DATETIME_LT(`start_time`, 'today', '3d')
```

字段 key 必须先经 `workitem meta-fields` 确认，且同时传 `--project-key` 与 `--work-item-type`；禁复用示例 key。

---

## 7. 人员与角色函数

| 函数 | 返回 |
|------|------|
| `current_login_user()` | 当前登录用户 userkey |
| `team(include_manager, '团队名')` | 团队成员 userkey 数组；首参 `true` 含管理者 |
| `all_participate_persons()` | 全部参与人 userkey 数组 |
| `participate_persons()` | 当前参与人 userkey 数组 |
| `participate_roles()` | 参与角色的 `role_name` label 数组 |

**关键约束**：

- 团队名必须先用 `team list` 查询，并传 `--project-key`；否则报 `attribute_value not found`
- `participate_roles()` 值位**必须**用 `role_name` label（`'后端开发'`），禁传 `role_id`（`'fe_rd'`）
- 人员字段值位**必须**用 `<id:userkey>`（详见 §11）

```sql
-- 当前负责人是我
array_contains(`current_status_operator`, current_login_user())
-- 指派给某团队（含管理者）
any_match(`current_status_operator`, x -> x IN (team(true, '真实团队名')))
-- 有指定角色参与
array_contains(participate_roles(), '后端开发', '前端开发')
```

---

## 8. 节点函数

| 函数 | 用途 |
|------|------|
| `all_nodes_name()` | 全部节点名数组 |
| `in_progress_nodes_name()` | 进行中节点名数组 |
| `risk_label()` | 节点延期状态标识数组，**不支持 `any_match`** |
| `get_node_attribute(node, attribute)` | 指定节点属性；`node` 可为节点名/`__ALL`/`__BELONGING`；**「所属节点」必须用 `__BELONGING`** |

### 8.1 `get_node_attribute` 属性访问

**可用属性**：排期、估分、节点时间、节点完成结论、节点完成意见、负责人、当前负责人、状态。**指定节点负责人**：`owner` 与 `负责人` 语义等价。

**开始/结束时间访问形式**（作为第二参数）：

- 节点排期：`'__排期_开始时间'` / `'__排期_结束时间'`
- 节点时间：`'__节点时间_开始时间'` / `'__节点时间_结束时间'`

### 8.2 `__ALL` 简写规则

- 对整体属性直接用区间/比较；**禁止**拆 `__排期_开始时间` / `__排期_结束时间`，**禁止**外套 `any_match`
- `排期` 支持 `BETWEEN`；`估分` 不支持 `BETWEEN`，需拆 `>= a AND <= b`

```sql
-- 指定节点负责人（value 必须 <id:userkey>）
WHERE array_contains(get_node_attribute('需求详评','owner'), '<id:userkey>')
-- 开始节点排期在过去 30 天内
WHERE RELATIVE_DATETIME_BETWEEN(get_node_attribute('开始','__排期_开始时间'), 'past','30d')
-- 所属节点当前负责人
WHERE array_contains(get_node_attribute('__BELONGING','当前负责人'), '<id:userkey>')
-- 全部节点排期在区间内
WHERE get_node_attribute('__ALL','排期') between '2026-01-01' and '2026-03-31'
-- 全部节点估分 ≥ 30
WHERE get_node_attribute('__ALL','估分') >= 30
```

节点名以 `workflow get-node` 返回为准：`--node-id-list` 传 `["_all"]`，并同时传 `--project-key` 与 `--work-item-id`。

### 8.3 控件函数多值语义

**`all_nodes_name()` / `in_progress_nodes_name()`**（不能用 `IN`，只能走 `any_match`）：

| 语义 | 写法 |
|------|------|
| 存在选项属于（OR） | `any_match(<控件>, x -> x IN ('a','b'))` |
| 包含（AND） | `array_contains(<控件>, 'a','b')` |
| 集合完全相等 | `<控件> = '["a","b"]'`（JSON 数组，例外） |
| 全部不属于 | `none_match(<控件>, x -> x IN ('a','b'))` |
| 不同时包含（至少缺一个） | `NOT array_contains(<控件>, 'a','b')` |

**`risk_label()`**（**不支持 `any_match`**）：

| 语义 | 写法 |
|------|------|
| 有交集（默认「含延期节点」） | `array_intersect(risk_label(), '["延期/前端","延期/后端"]')` |
| 全部包含（AND） | 多个 `array_contains(risk_label(), 'x')` AND |
| 父级「延期」/「排期信息不全」 | `array_contains(risk_label(),'延期')` |

---

## 9. 关系与关联函数

### 9.1 关系判断函数

对关系对端做条件判断：

- `any_relation_match(rel, x -> expr)` — 存在一个对端满足
- `all_relation_match(rel, x -> expr)` — 每一个对端都满足
- `none_relation_match(rel, x -> expr)` — 每一个对端都不满足
- `not all_relation_match(rel, x -> expr)` — 至少一个对端不满足

`relation_field_chain` 等取关系后**必须**外层套关系判断函数。

### 9.2 关系参数三种形式

1. 关联字段：`` `字段key` ``
2. `relation('关系名')`
3. `relation_field_chain('rel1', 'rel2' [, 'rel3'])` — ≤ 3 跳

**子任务父工作项**：关系名固定 `'__父工作项'`（双下划线前缀）。报错 `relationNode not found, label:父工作项` 时改为 `'__父工作项'`。

### 9.3 跨端字段引用

对端字段引用形式：`` x.`字段名<target:project_key::type_key>` `` 或 `` x.`字段名<target:all>` ``。

**通用字段必须用 `<target:all>`**：标题、创建人、创建时间、业务线、优先级、当前负责人、所属工作项、所属空间、工作项 ID、工作项类型、状态。

```sql
-- 对端优先级为 P0
WHERE any_relation_match(`多选关联字段`, x -> x.`priority<target:all>` = 'P0')
-- 子任务父工作项名称
WHERE any_relation_match(relation_field_chain('__父工作项'), x -> x.`name<target:all>` like '%登录%')
-- 多级关系 + 节点属性
WHERE all_relation_match(relation_field_chain('__父工作项','需求关联软件'), x -> array_contains(get_node_attribute('开始','负责人'), '<id:userkey>'))
```

### 9.4 其它关系函数

- `parent_work_item(relation('关系名'))` — 父工作项 ID。**仅支持 SELECT 阶段**，WHERE 中使用报 `parent_work_item() not supported in stage Where`。
  - SELECT：`SELECT parent_work_item(relation('关系名')), work_item_id FROM ...`
  - 按父工作项过滤改用：`WHERE any_relation_match(relation_field_chain('__父工作项'), x -> x.\`work_item_id<target:all>\` = '12345')`
- `association()` — 跨空间关联实例 ID：`WHERE association() = '实例ID'`
- `linked_work_item()` — 子任务来源控件（父工作项 ID）。判空推荐 `IS NOT NULL`；等值右值必须是真实父工作项 ID，否则 `attribute_value not found`

---

## 10. 状态函数 `status_time`

对状态流工作项（如 `issue`、缺陷）和节点流工作项（如 `story`）均有效。传入的状态名不存在时报 `metadata error`，需以 `workflow list-state-transitions` 或 `workitem meta-fields` 返回的真实状态名为准；后者的 `--field-keys` 传 `["work_item_status"]`。

| 用法 | 允许位置 | 参数形式 |
|------|---------|---------|
| `status_time('状态名')` | WHERE / SELECT | **纯状态名**，不带 `__` 前缀 或 `_开始时间`/`_结束时间` 后缀 |
| `status_time('__状态名_开始时间')` / `_结束时间` | **仅 SELECT 或时间差表达式** | WHERE 中使用会报错 |

状态名以 `workflow list-state-transitions` 或 `workitem meta-fields` 返回的真实 option 为准；后者的 `--field-keys` 传 `["work_item_status"]`。

```sql
-- ✅ WHERE 用纯状态名过滤
WHERE status_time('<状态名>') between '2025-01-01' and '2025-12-31'
-- ✅ SELECT 计算状态累计时长
SELECT `work_item_id`, status_time('__<状态名>_结束时间') - status_time('__<状态名>_开始时间')
FROM `project_key`.`issue`
-- ❌ WHERE 里做算术过滤
WHERE status_time('__<状态名>_结束时间') - status_time('__<状态名>_开始时间') > 86400
```

---

## 11. 名称消歧 `<id:xxxx>`

### 11.1 人员字段值位规则

人员字段、user、multi-user、自定义人员控件、角色列的值位**必须**优先用 `<id:userkey>`：

- 裸 userkey（`= 'example_userkey'`）在部分场景被当姓名解析，报 `user label '...' does not exist`
- 裸中文姓名（`= '张三'`）常报 `user label '...' is not unique`（即便 `user search` 唯一，服务端仍全局校验）

流程：`user search` 拿 userkey → 值位写 `<id:userkey>`。

```sql
-- 单人字段
WHERE `owner` = '<id:userkey>'
-- 多人字段：= 或 array_contains 均可
WHERE array_contains(`current_status_operator`, '<id:userkey>')
-- 多人 OR：首选 IN；含函数返回集合时才用 any_match
WHERE `current_status_operator` IN ('<id:k1>', '<id:k2>')
WHERE `current_status_operator` = current_login_user()
```

### 11.2 团队/枚举消歧

同名重复时用 `<id:xxxx>`：

```sql
WHERE any_match(`current_status_operator`, x -> x IN (team(true, '开放平台团队<id:3455>')))
WHERE `priority` = '<id:option_2>'
```

---

## 12. 角色（Role）

**角色不是字段**。MQL 中角色作为**列引用**。

### 12.1 列名两种写法

| 写法 | 使用场景 | 示例 |
|------|---------|------|
| **主写法** `` `__<role_name>` `` | 默认。`role_name` 严格取自 `workitem meta-roles` 返回值，含空格原样保留 | `` `__后端开发` ``、`` `__经办人` `` |
| **fallback** `` `__role_<project_key>_<work_item_type>_<role_id>` `` | 仅当 `role_name` 与其它字段/角色中文名冲突时 | `` `__role_<project_key>_story_<role_id>` `` |

**硬性前置**：涉及角色的 MQL 必须先调 `workitem meta-roles` 获取真实 `role_name` / `role_id`，并同时传 `--project-key` 与 `--work-item-type`。**禁止**按用户自然语言原文（俗称、缩写、错别字）直接拼列名。

**系统默认角色**：`role_name` = 「经办人」/「报告人」；`role_id` = `operator` / `reporter`（后者仅用于 `role_operate` API 参数，禁出现在 MQL 列名或函数里）。

### 12.2 无效形式（Code 3010 `attr label not found`）

- `` `__<role_id>` ``（如 `__fe_rd`、`__operator`）
- `` `<role_id>` ``（无 `__` 前缀）
- 函数包装：`role(fe_rd)`、`get_role_owners(fe_rd)`

```sql
-- ✅ 主写法
WHERE array_contains(`__后端开发`, '<id:userkey>')
WHERE array_contains(`__经办人`, '<id:userkey>')

-- ✅ fallback（仅冲突时）
WHERE array_contains(`__role_<project_key>_story_<role_id>`, '<id:userkey>')

-- ❌ 缺 __ / 用 role_id / 中文原文
WHERE `经办人` = '<id:userkey>'
WHERE array_contains(`__operator`, '<id:userkey>')
```

---

## 13. 特殊字段查询

### 13.1 日期区间字段（date_range）

工作项级日期区间字段（自定义"计划周期"等）**不能**直接以字段 key 查询，必须拆成访问形式（**MQL 派生语法**，非复合字段子字段概念）：

- `` `__<字段key>_开始时间` ``
- `` `__<字段key>_结束时间` ``

> 与节点排期不同：节点排期用 `get_node_attribute(...,'__排期_开始时间')` 访问。

```sql
-- ✅ 正确
WHERE `__field_xxxxxx_开始时间` > '2025-01-01'
WHERE RELATIVE_DATETIME_BETWEEN(`__field_xxxxxx_结束时间`, 'past', '30d')
-- ❌ 错误
WHERE RELATIVE_DATETIME_BETWEEN(`field_xxxxxx`, 'past', '30d')
```

### 13.2 树状/级联字段（如业务线）

先用 `workitem meta-fields` 获取 options：`--field-keys` 传 `["business"]`，或 `--field-query` 传 `业务线`；确认**叶子 option_id 与 label**。

| 语义 | 写法 |
|------|------|
| 父级（含所有下级） | `tree-multi-select`（数组型）用 `` any_match(`业务线`, x -> x = '<父级 label>') `` 或 `` `业务线` IN ('<父级 label>') ``；`tree-select`（单选）不支持父级级联，仅按叶子精确匹配 |
| 叶子等值 | `` `业务线` = '<叶子 label>' `` |
| ❌ 完整路径 | `` `业务线` = '<父级>/<叶子>' `` |

创建/更新类接口优先使用叶子节点 `option_id`。

---

## 14. Lambda 表达式限制

Lambda `x -> ...` 内部服务端**只受理下列极简条件**，其余一律拒回 `lambda predicate operator not supported: <OP>` 或 `unsupported lambda predicate`。

### 14.1 支持 / 不支持

| ✅ 支持 | ❌ 不支持 |
|--------|----------|
| `x = 'value'` | `x != 'a'` |
| `x IN ('a','b',...)`（值可含 `team(...)` / `current_login_user()` / `<id:userkey>`） | `x NOT IN (...)` |
| 同变量 OR：`x = 'a' OR x = 'b'`（推荐改 `IN`） | `x > / >= / < / <=` |
| — | `LIKE` / `NOT LIKE` |
| — | `BETWEEN` |
| — | `IS NULL` / `IS NOT NULL` |
| — | `RELATIVE_DATETIME_*(x,...)` |
| — | `AND` 复合条件（同/跨变量） |
| — | 嵌套 `any_match` / `none_match` |

### 14.2 决策规则

- 多选/数组字段优先直接用顶层 `IN` / `NOT IN` / `array_contains` / `NOT array_contains` / `none_match`
- **树状/级联字段父级匹配**（含所有下级）：`tree-multi-select`（数组型）用 `any_match(field, x -> x = '<父级 label>')` 或 `` `field` IN ('<父级 label>') ``；`array_contains` 仅精确匹配、不含子级级联。`tree-select`（单选）不支持父级级联，仅按叶子 label / option_id 精确匹配
- 仅当 lambda 值列表需引用 `team(...)` / `current_login_user()` 等**返回集合的函数**，或需在集合内做多个 `<id:userkey>` 判断，才使用 `any_match(x -> x IN (...))`
- `any_match` 第二参数**必须**是 `x -> ...` 形式的 lambda；不接受数组字面量

---

## 15. 完整示例

> 示例中字段 key / 角色 / options 均以 `workitem meta-fields` / `workitem meta-roles` 返回为准；两者都必须同时传 `project_key` 与 `work_item_type`。

### 15.1 数组包含 + 当前用户 + 未归档

```sql
SELECT `work_item_id`, `name`, `work_item_status`, `priority`
FROM `project_key`.`story`
WHERE array_contains(`current_status_operator`, current_login_user())
  AND `archiving_date` IS NULL
```

### 15.2 相对时间

```sql
SELECT `work_item_id`, `name`, `start_time`
FROM `project_key`.`story`
WHERE RELATIVE_DATETIME_BETWEEN(`start_time`, 'past', '30d')
```

### 15.3 逾期未完成（日期区间字段 + 状态过滤）

```sql
SELECT `work_item_id`, `name`, `work_item_status`
FROM `project_key`.`story`
WHERE RELATIVE_DATETIME_LT(`__field_xxxxxx_结束时间`, 'today')
  AND `work_item_status` != '<完成态label>'
```

### 15.4 团队角色（`team list` 前置）

> 团队名 `'真实团队名'` 是占位。**必须先用 `team list` 拉取真实团队名，并传 `--project-key`**，否则报 `attribute_value not found`。

```sql
-- 主写法
SELECT `work_item_id`, `name`, `priority`
FROM `project_key`.`story`
WHERE any_match(`__后端开发`, x -> x IN (team(true, '真实团队名')))

-- fallback（仅角色名冲突时）
WHERE any_match(`__role_<project_key>_story_<role_id>`, x -> x IN (team(true, '真实团队名')))
```

### 15.5 综合（模糊 + 数组 + 排序分页）

```sql
SELECT `work_item_id`, `name`, `work_item_status`, `priority`
FROM `project_key`.`issue`
WHERE `name` LIKE '%性能优化%'
  AND array_contains(`current_status_operator`, current_login_user())
  AND `priority` = 'P0'
ORDER BY `updated_at` DESC
LIMIT 50
```

### 15.6 节点属性 + 延期标识

```sql
-- 所属节点当前负责人（单条件；`__BELONGING` 属性禁与其它 `__BELONGING` 属性 AND 组合，禁被 `array_contains` 包裹后与其它条件组合，见下方警示）
SELECT `work_item_id`, `name`, `work_item_status`
FROM `project_key`.`story`
WHERE array_contains(get_node_attribute('__BELONGING','当前负责人'), '<id:userkey>')

-- 所属节点状态：必须用 `=` 顶层比较（返回空 list 无 error），禁止 array_contains 包裹
SELECT `work_item_id`, `name`
FROM `project_key`.`story`
WHERE get_node_attribute('__BELONGING','状态') = '<进行中状态label>'

-- 开始节点已延期（risk_label() 等值是「集合完全相等」，用 JSON 数组字符串，属例外）
SELECT `work_item_id`, `name`
FROM `project_key`.`story`
WHERE risk_label() = '["延期/开始"]'
```

> ⚠️ **服务端已知限制（权威定义，其它章节引用本节）**：`get_node_attribute('__BELONGING','状态')` 一旦被 `array_contains` 包裹（单条件即触发），或任意 `__BELONGING` 属性之间做 AND 组合（如"当前负责人 + 状态"），会触发 nil pointer / panic。修复策略：状态类**必须**用 `=` 顶层比较；负责人等多值属性**必须**保持单条件；如需组合，改用 `__ALL` 属性或状态字段（`work_item_status`）+ `current_status_operator` 拼接。

### 15.7 关系查询（链式 + 跨空间字段）

```sql
-- 子任务的父工作项名称包含"登录"
SELECT `work_item_id`, `name`
FROM `project_key`.`sub_task`
WHERE any_relation_match(relation_field_chain('__父工作项'), x -> x.`name<target:all>` like '%登录%')

-- 多级关系：子任务→父工作项→关联软件
SELECT `work_item_id`, `name`
FROM `project_key`.`sub_task`
WHERE any_relation_match(relation_field_chain('__父工作项','需求关联软件'), x -> x.`name<target:all>` = '某软件')
```

### 15.8 状态时间 + 节点负责人

```sql
-- issue（状态流）：状态窗口
SELECT `work_item_id`, `name`, `work_item_status`
FROM `project_key`.`issue`
WHERE status_time('<状态名>') between '2025-01-01' and '2025-12-31'

-- story（节点流）：开始节点负责人
SELECT `work_item_id`, `name`
FROM `project_key`.`story`
WHERE array_contains(get_node_attribute('开始','负责人'), '<id:userkey>')
```

---

## 附：关键词 → 语法 映射

### 控件关键词

| 用户关键词 | 语法 |
|-----------|------|
| 参与人员、全部参与人员 | `all_participate_persons()` |
| 当前参与人 | `participate_persons()` |
| 流程节点、所有节点 | `all_nodes_name()` |
| 进行中节点 | `in_progress_nodes_name()` |
| 节点排期、节点估分、所属节点 | `get_node_attribute(node, attr)`（所属节点用 `__BELONGING`） |
| 节点延期标识 | `risk_label()` |
| 关联工作项字段 | `relation_field_chain('rel1',...)`（≤3 跳） |
| 子任务父工作项 | `relation_field_chain('__父工作项')` |
| 子任务来源 | `linked_work_item()` |
| 状态时间窗口（状态流） | `status_time('状态名') between ...` |
| 状态累计时长（状态流） | `status_time('__<状态名>_结束时间') - status_time('__<状态名>_开始时间')`（仅 SELECT） |

### 关系语境

| 关键词 | 函数 |
|-------|------|
| 每一个 | `all_relation_match` |
| 存在一个/一组 | `any_relation_match` |
| 每一个不满足 | `none_relation_match` |
| 存在一个不满足 | `not all_relation_match` |

### 数组语义

| 语义 | 语法 |
|------|------|
| 存在选项属于（OR） | `IN + 元组`（首选）；控件函数用 `any_match` |
| 全部选项均不属于 | `NOT IN`（首选）；`none_match` 备选 |
| 同时包含（AND） | `array_contains(field, 'a', 'b')` |
| 集合完全相等 | `` `field` = ('v1','v2') ``（禁 JSON 数组字符串，控件函数除外） |
