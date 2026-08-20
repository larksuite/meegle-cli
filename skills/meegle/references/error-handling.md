# 错误处理规则

> 症状 → 修复动作映射。语法/协议细节引用 [mql-syntax.md](mql-syntax.md) 与 SKILL.md「字段值格式」。

**通用原则**：从报错提取关键字 → 匹配下表修复 → 重试。**同一错误最多自动重试 2 次（共 3 次尝试）**。仍失败则停止自愈，向用户结构化说明：① 原始请求与目标语义；② 每次修复的关键改动与服务端返回；③ 推测根因（字段/角色/枚举不存在、无权限等）并请求澄清。**禁止无限重试或反复微调同一参数**。

---

## 1. MQL 自愈规则（高频，优先匹配）

> `workitem meta-fields` / `workitem meta-roles` 必须同时带 `--project-key` 和 `--work-item-type`；`workitem meta-types` 只需 `--project-key`。

### 1.1 元数据类

| 报错症状 | 修复动作 |
|---------|---------|
| 字段 key 在该空间+工作项类型下不存在 | 立即调 `workitem meta-fields`，同时传空间、类型与报错中的字段名，替换重试 |
| 字段中文名歧义（多字段同名） | 改用字段 key。通过 `workitem meta-fields` 的 `field_query` 拿所有匹配 key，选语义正确的替换；无法判断询问用户 |
| 把角色当字段写（如 `经办人`） | 见 [mql-syntax.md §12 角色](mql-syntax.md)：`workitem meta-roles` 取 `role_name` / `role_id`，用 `` `__<role_name>` `` 或 fallback 复合列名 |
| 枚举值/状态值在该空间+工作项类型下不存在 | `workitem meta-fields` 精确获取对应字段的 options，用真实 label 替换。禁硬编码 |
| 树状字段值写成完整路径或非叶子 | `workitem meta-fields` 获取 options，确认叶子 `option_id` / label；父级查询改用 `any_match` |
| 查询不支持的字段类型（`attachment` / `file` / `spec_doc` / `specDocs`） | 从 SELECT/WHERE 移除，改用 `workitem get`。**`multi-file` 例外**：允许 `SELECT` / `IS NULL` / `IS NOT NULL`；深度筛选不支持 |

### 1.2 语法类

| 报错症状 | 修复动作 |
|---------|---------|
| 中文字符编码损坏 | 整个 MQL 用单引号包裹、中文名反引号、无多余反斜杠。重构重试 |
| 用了硬规则禁用语法（`SELECT *`、`count()`/`GROUP BY`、`REGEXP`、`CONTAINS()`） | 对照 [mql-syntax.md §1.1](mql-syntax.md)。`LIMIT ... OFFSET n` / `LIMIT n,count` 均支持，推荐配 `ORDER BY` |
| 字段名未加反引号导致 `syntax error near ':...'` | 字段名用反引号；含 `<target:xxx>` 时**整体**放同一对反引号（`` `name<target:all>` ``） |
| 顶层 SELECT/WHERE 用了 `` `name<target:all>` `` 报 `attribute[...] not found` | `<target:xxx>` **仅**允许在关系判断 lambda 内（`` x.`name<target:all>` `` 形式）；顶层引用去掉修饰符 |
| 用 `\'` 转义单引号 | 改用 `''`（两个单引号） |
| `Internal % and _ characters must be escaped` | LIKE 内部字面量 `%` / `_` 必须写作 `\%` / `\_`；即便 `_` 出现在关键词中间（如 `%test_case%`）也会被服务端强制拒回，必须转义为 `%test\_case%`。见 [mql-syntax.md §1.6](mql-syntax.md) |
| LIKE 缺通配符 | 完整包含形态 `LIKE '%关键词%'` |

### 1.3 语义类

| 报错症状 | 修复动作 |
|---------|---------|
| FROM 缺空间或类型 | 修正为 `` FROM `project_key`.`work_item_type_key` `` |
| SELECT 中放了函数（`current_login_user()`、`array_contains()`） | 从 SELECT 移除，函数只能在 WHERE 中 |
| `parent_work_item() not supported in stage Where` | `parent_work_item()` 仅支持 SELECT；WHERE 中按父工作项过滤改用 `` any_relation_match(relation_field_chain('__父工作项'), x -> x.`work_item_id<target:all>` = '<父ID>') `` |
| 操作符与字段类型不兼容 | 对照 [mql-syntax.md §4 兼容性表](mql-syntax.md) 修正。常见：`number` / `bigint` 不支持 `BETWEEN` / `LIKE` / `array_contains`，改为 `>=` + `<=` / 精确等值 |
| 多值右值用了 JSON 数组字符串（`IN '["a","b"]'`） | `IN` 直接 syntax error，改元组 `IN ('a','b')`。`=` 服务端当前兼容 JSON 数组字符串但非推荐写法，统一改元组 `= ('a','b')`。**例外**：`array_intersect` / `risk_label() = ...` 保留 JSON 数组字符串 |
| `tree-multi-select` / `workitem_related_multi_select` 右值拒回 `metadata error` 或 `attrValueLabel not found`（如裸 option_id / 裸 work_item_id） | 改为 label 或 `<id:option_id>` / `<id:work_item_id>` 包裹形式。见 [mql-syntax.md §5.1](mql-syntax.md) |
| signal 字段 `IS NULL` / `IS NOT NULL` 报 `operator not supported` | signal 不支持空判断，改用 `!= '真实 label'` 或去掉该条件 |
| signal 值位传了 `option_id` / `<id:>` / `'true'` / `'false'` / `'null'` | 改用 `option_name` label（如 `'已通过'`） |
| Lambda 报 `lambda predicate operator not supported: <OP>` / `unsupported lambda predicate` | Lambda 内仅支持 `x = 'v'`、`x IN (...)`、同变量 OR。其余（`!=` / `NOT IN` / 比较符 / `LIKE` / `BETWEEN` / `IS NULL` / `RELATIVE_DATETIME_*` / `AND` 复合 / 嵌套 Match）全部拒回。见 [mql-syntax.md §14](mql-syntax.md)。多选/数组字段优先用顶层 `IN` / `NOT IN` / `array_contains` / `none_match`；仅值列表含 `team()` / `current_login_user()` 时用 `any_match` |
| `get_node_attribute('__BELONGING','状态')` 触发 nil pointer / panic | 见 [mql-syntax.md §15.6](mql-syntax.md) 权威规则：状态类改用 `=` 顶层比较（禁被 `array_contains` 包裹）；`__BELONGING` 属性禁 AND 组合 |

### 1.4 参数类

| 报错症状 | 修复动作 |
|---------|---------|
| `RELATIVE_DATETIME_*` 报 `unexpected operator for future` / `invalid argument` | 见 [mql-syntax.md §6.1 兼容矩阵](mql-syntax.md)：`_EQ` 只接受 `today`/`tomorrow`/`yesterday` 且**无 offset**；`_GT/_GE/_LT/_LE` 只接受 `today`（可带 `±Nd`）；`future`/`past`+`Nd` 只允许配 `_BETWEEN` |
| ORDER BY 字段不支持排序 | 换可排序字段（如 `updated_at` / `start_time` / `work_item_id`）或移除 |
| MQL 与 session_id 都空 | 首查必须传 MQL；翻页必须传 session_id |
| 翻页参数缺失 | 传 `[{"group_id":"1","page_num":N}]`（无分组时 `group_id` 固定 `"1"`） |
| 未传 project_key | `workitem query` 的 `--project-key` 必填 |
| 空间不存在 | 用 `project search` 确认 |
| 空间名匹配多个 | 从报错提取候选，让用户选择或用精确 project_key 重试 |
| 当前用户对该空间无权限 | 告知用户需申请访问，**禁止重试** |

---

## 2. 通用字段值自愈

| 报错症状 | 修复动作 |
|---------|---------|
| `need STRING type, but got: LIST/MAP` | 原生 JSON 改为 JSON.stringify 字符串（见 SKILL.md「字段值格式」） |
| 字段值类型错配（数字↔字符串、单值↔数组） | 仅改格式，值不变 |
| 级联选项传非叶子节点 | 展示 `children` 树，让用户选叶子 |
| 枚举值不在可选项 | 从 options 匹配；唯一命中则修正重试，否则询问用户 |

---

## 3. 非 MQL 错误速查

| 报错症状 | 修复动作 |
|---------|---------|
| 找不到空间 / 中文名多命中 | `project search` 验证，取 project_key 精确调用 |
| 找不到工作项类型 | `workitem meta-types` 确认合法 `type_key` |
| 模板不存在/禁用 | `workitem meta-fields` 获取可用模板 |
| 角色查询无结果 / 经办人报告人查不到 | `workitem meta-roles` 确认 `role_name` / `role_id`；系统默认 `role_name`=`经办人`/`报告人`，`role_id`=`operator`/`reporter` |
| 人名→userkey 转换失败 | `user search` 批量查询 |
| 人员字段写入失败 | user 传单个 userkey；multi-user 必须 stringified 数组（如 `"[\"k1\",\"k2\"]"`） |
| 找不到节点 | `workflow get-node` 查全量节点详情列表匹配真实 `node_id`（`workitem get` 只返进行中节点） |
| 节点流转失败 | 节点流用 `workflow transition`；状态流用 `workflow transition-state`（先 `workflow list-state-transitions` 取 `transition_id`，`workflow list-state-required` 查必填） |
| 重复流转已完成节点 | 流转前 `workflow get-node` 检查状态 |
| 节点未激活 | 先确认工作流进度 |
| 创建缺模板 | `workitem meta-fields` 获取模板字段 |
| 创建必填字段未提供 | `workitem meta-create-fields` 取 `is_required=1` 的所有字段（注意 `workitem meta-fields` 不返回 required 标记） |
| 角色更新失败 | 改用 `workitem update` 的 `role_operate`（不走 fields） |
| `group_type=bind` 缺 `group_id` | 补非空 `group_id`；解绑用 `{"type":"disabled"}` |
| `group_type=auto/disabled` 却传了 `group_id` | 二选一：保留 `bind + group_id`，或去掉 `group_id` |
| `page_size` 被序列化为字符串 | Meegle CLI 改用 `--params '{"page_size":N,"page_token":"..."}'` |
| `mywork todo` 需选择工作区 | 从报错列表把 `asset_key`（Asset_xxx）传入重试 |
| 操作记录时间区间非法 | `start_time < end_time`，格式毫秒时间戳或 `YYYY-MM-DD` |
| 关联查询缺 `relation_id` | 先用 `relation meta-definitions` 获取 |
| 接口限流 | 等待 1-2 秒重试，或减少并发 |
| 视图配置失效 / session 缓存过期 | 前者 `view search` 取有效 ID；后者不传 `session_id` 重查 |
| 工作项已冻结/终止/归档 | 告知用户不可编辑，**禁止重试** |
| 状态流转目标不可达 | 先 `workflow list-state-transitions` 获取合法路径供用户选择 |
| 选项可见性不满足 | 重新用 `workitem meta-fields` 取上下文可见选项 |
| `work_item_type_key` 不存在 | `workitem meta-types` 取合法列表 |
| WBS 草稿不存在 | 先用 `wbs create-draft`，或确认该空间已启用 WBS |
| 非 IPD 项目调用 WBS/资源库/交付物 | 告知用户该空间不支持 |
| `wbs edit-draft` 父行不支持新增子行 | `wbs list-draft-rows` 确认父行类型和拆解模式 |
| 工时未启用 | 需在项目设置开启（`actual_work_time_switch=true` 或安装"工时登记"插件），**禁止重试** |
| 工时记录返回空但确认有数据 | 确认 `work_item_type` 是否正确 |

---

## 4. 熔断条件（立即终止）

- 空间未找到（`project search` 连续 3 次失败）
- Permission Denied（无空间访问权限）
- 服务端返回明确的"已归档/已冻结/无权限"业务错误
