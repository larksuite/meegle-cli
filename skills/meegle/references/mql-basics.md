# MQL 基础语法与硬规则

目录：1 语句形态 · 2 标识符与字符串 · 3 禁用语法 · 4 不可查询字段类型 · 5 字段名易错映射 · 6 枚举值 · 7 LIKE 转义 · 8 多值右值 · 9 数据类型 · 10 安全传参 · 11 已终止工作项

## 1. 语句形态

```sql
SELECT fieldList FROM `project_key`.`work_item_type_key` [WHERE cond] [ORDER BY field [ASC|DESC]] [LIMIT n]
-- 字段列表禁 *；FROM 必须双段完整；单次最多 50 条，LIMIT n 只截断（n ≤ 50）不翻页，翻页用 --session-id + --group-pagination-list
```

## 2. 标识符与字符串

- 字段 / 表名用反引号：`` `work_item_id` ``、`` `project_key`.`story` ``。
- `<target:xxx>` 修饰符必须整体放在同一对反引号内（`` `name<target:all>` ``），且仅允许在关系判断 lambda 内部（`` x.`name<target:all>` ``）；顶层 SELECT / WHERE 直接引用主表字段禁止使用。
- 字符串值一律单引号（中文值也是）：`= '进行中'`、`LIKE '%性能%'`、`= '<id:759…>'`。反引号不能包值，`` = `进行中` `` 会被当成字段名。
- 值里带单引号写 `''`（`'张''三'`）。禁止 `\'`：服务端不做转义，`'张\'三'` 会当作字面量 `张\` 导致空结果。
- 日期必须加引号：`'2026-06-01'`，裸日期会被解析为减法。枚举值优先用 label（须先经 `workitem meta-fields` 确认存在），找不到时用 `<id:option_id>`。

## 3. 禁用语法

| 禁止 | 替代 | 原因 |
|------|------|------|
| `SELECT *` | 显式列出字段 | 不支持通配符 |
| `count(*)` / `SUM()` / `GROUP BY` | 从返回的 `count` 字段读总数 | 不支持聚合 |
| `REGEXP` / `regexp_like()` | `LIKE '%...%'` | 不支持正则 |
| `currentUser()` / `current_user()` | `current_login_user()` | 函数名错误 |
| `CONTAINS(field, val)` | `array_contains()` 或 `LIKE` | 无此运算符 |
| `NOT BETWEEN a AND b` | `< a OR > b` | 服务端行为不稳定 |
| SELECT 里放仅支持 WHERE 的函数（`current_login_user()`、`array_contains()`） | 按函数允许位置使用 | `status_time()`、`parent_work_item()` 的 SELECT 用法见对应专题 |

## 4. 不可查询的字段类型

出现在 SELECT / WHERE 会报 `unsupported field type`，必须移除并改用 `workitem get`：`attachment` / `file`（附件）、`spec_doc` / `specDocs` / `spec_documents`（文档）。

例外：`multi-file`（如 `multi_attachment`）仅支持 `SELECT` / `IS NULL` / `IS NOT NULL`，返回 `key_label_value_list`；不支持 `LIKE` / `array_contains` 等深度筛选。

## 5. 字段名易错映射

MQL 支持字段 key 和中文名，必须优先 key，中文名多有同名歧义。

| ❌ 写法 | ✅ 替代 |
|--------|--------|
| `state_key` / `status` | `work_item_status` |
| `archiving_status` / `is_archived` | `archiving_date`（未归档用 `IS NULL`） |
| `is_aborted` / 用状态 label 判断已终止 | MQL 里写 `` `abort_reason` IS NOT NULL ``；调用时另传参数 `--features`，见 §11 |
| 中文名如「名称」「当前负责人」「创建时间」 | 先 `workitem meta-fields` 查 key（同时传 `--project-key` 与 `--work-item-type`） |

## 6. 枚举值 / 状态值禁止硬编码

状态、select、tree-select 等枚举 label 由「空间 + 工作项类型」自定义。禁止硬编码 `关闭`、`已完成`、`进行中`、`OPEN`、`CLOSED`，违反报 `attrValueLabel not found`。
流程：`workitem meta-fields` 取 options → 用真实 label 或 `<id:option_id>`。

## 7. LIKE 转义

- 通配符仅 `%`。`_` 不是通配符，未转义会被服务端拒回 `Internal % and _ characters must be escaped`（含 `%test_case%` 这类关键词中间的 `_`）。
- 字面量 `_` 写 `\_`，字面量 `%` 写 `\%`。模式必须是完整包含形态 `'%...%'`。

```sql
✅ LIKE '%性能问题%'   ✅ LIKE '%test\_case%'（字面量下划线转义）   ✅ LIKE '%100\%完成%'
❌ LIKE '100%完成'（缺前 %）   ❌ LIKE '%test_case%'（裸 _ 未转义）   ❌ LIKE '%100%完成%'（内部 % 未转义）
```

## 8. 多值右值

多值右值统一用元组 `(v1, v2)`，适用于 `IN` / `NOT IN` / `array_contains` / `=` / lambda 内的 `IN`。JSON 数组字符串 `'["a","b"]'` 在 `IN` 直接 syntax error，`=` 服务端兼容但不推荐。

唯一例外：控件函数场景要求 JSON 数组字符串——`array_intersect(<控件函数>, '["a","b"]')`（有交集）与 `risk_label() = '["a","b"]'`（集合完全相等）。

## 9. 数据类型

| MQL 类型 | 对应字段类型 |
|---------|-------------|
| bool | bool |
| bigint | number 中的 `work_item_id`、`auto_number` |
| double | 其它 number |
| varchar | text、multi-pure-text、multi-text、select、tree-select、radio、user、link、signal、workitem_related_select |
| date | date（`YYYY-MM-DD` 或 `YYYY-MM-DD+TZD`） |
| datetime | schedule、precise_date（`YYYY-MM-DDThh:mm:ss[TZD]`） |
| array(varchar) | multi-select、tree-multi-select、multi-user、link_cloud_doc、workitem_related_multi_select |
| array(struct) | compound_field |
| lambda | `x -> x IN ('a','b')` 等 |

## 10. 传参：heredoc 隔离 shell

MQL 中的反引号、单引号和 `$` 必须原样传给服务端。Shell 调用推荐使用带引号定界符的 heredoc（`<<'MQL'`），避免命令替换和引号丢失；不要把含反引号的 MQL 直接放进 shell 双引号。

```bash
meegle workitem query --project-key 空间key --mql "$(cat <<'MQL'
SELECT `work_item_id`, `name` FROM `空间key`.`story` WHERE `name` LIKE '%性能%'
MQL
)" --format json
```

## 11. 已终止工作项（`FindAborted`）

`workitem query` 默认只返回未终止的工作项，已终止的需要通过 `--features` 的 `FindAborted` 开关放出。判断是否已终止用 `` `abort_reason` ``，不要用 `work_item_status` 的状态 label 猜。

| 用户意图 | 调用参数 `--features` | MQL 的 WHERE |
|---------|------|-------|
| 只查未终止的（默认） | 不传 | 不加终止条件 |
| 未终止和已终止都要（「包含已终止的」「全部，含已终止」） | `{"FindAborted":"true"}` | 不加终止条件 |
| 只查已终止的 | `{"FindAborted":"true"}` | `` `abort_reason` IS NOT NULL `` |

- `--features` 里只放开关；`abort_reason` 条件写在 `--mql` 里。
- `--features` 的键值都是字符串：`"true"` / `"false"` 必须小写加引号，`{"FindAborted": true}`（布尔值）不合法。
- 只写 `` `abort_reason` IS NOT NULL `` 而不传开关，已终止的工作项不在结果里，查不出数据。

```sql
-- 只查已终止的（同时传 --features '{"FindAborted":"true"}'）
SELECT `work_item_id`, `name` FROM `project_key`.`story` WHERE `abort_reason` IS NOT NULL
```

完整调用示例见 [api-examples.md](api-examples.md) 的 `workitem query` 小节。
