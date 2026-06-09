# 更新工作项

> **CRITICAL** — 开始前 MUST 先用 Read 工具读取 `../SKILL.md`，其中包含前置检查、授权流程、命令参数参考、字段值格式、通用规范和错误处理。

本技能用于在飞书项目中更新工作项的字段值或角色成员，全程自动化执行。不包括状态流转和节点流转（由其他 Skill 负责）。

---

## 执行流程

### STEP 1 — 定位工作项并提取修改意图

从用户输入中提取：
- **目标工作项** — URL、工作项 ID 或名称
- **修改内容** — 哪些字段要改成什么值

> **URL 处理**：用户给了 URL 必须先调 `url decode`。只有 `url_kind == workitem_detail` 才能进入本 SOP；其他 kind 按 [url-kinds.md](url-kinds.md) 拒绝或追问。拿到 `simple_name` 和 `work_item_id` 后，必须再调 `project search` 把 `simple_name` 转为权威 `project_key`（同名空间可能有多个）。**禁止**自己从 URL 截取路径段作参数。`work_item_id` 参数必须是字符串类型。

🚨 **获取工作项类型（极重要）**：后续所有查询（字段配置、角色配置等）都强依赖 `work_item_type`。如果用户没有明确告知类型，**必须先调 `workitem get` 获取该实例的真实 `work_item_type`（返回体中的 `work_item_type.key`）**，绝不能猜测为 story 或 issue。

### STEP 2 — 并行查询配置

根据要修改的内容，**一轮内并行发起**：

| 调用 | 条件 | 说明 |
|------|------|------|
| `workitem meta-fields(field_query="字段名")` | 涉及字段修改 | 用 `field_query` 模糊搜索或 `field_keys` 精确匹配，**禁止逐页遍历** |
| `workitem meta-roles` | 涉及角色修改 | 获取角色 key |
| `user search` | 涉及人员字段 | 批量转换姓名为 userkey |

🚨 **效率要求**：必须一轮并发完成所有配置查询，第二轮直接执行更新。禁止逐个串行查询。

### STEP 3 — 转换字段值

将用户给的自然语言值转换成 API 格式（完整格式见主文档 [SKILL.md](../SKILL.md)「字段值格式」）：

> 🚨 **关键约定**：`field_value` 协议层是 **STRING**。标量直接字符串化；数组/对象**必须 JSON.stringify**，否则报 `need STRING type, but got: LIST`。

| field_type | 转换规则 & field_value 传参 |
|---|---|
| `text` / `number` / `bool` / `link` | 直接字符串，如 `"100"` / `"true"` |
| `user` | 姓名 → `user search` → 单个 userkey 字符串。多人同名时：用户说"分配给我自己"用 `current_login_user()`，否则**必须确认** |
| `multi-user` | 批量转换为 userkey，**stringified** 如 `"[\"key1\",\"key2\"]"` |
| `select` / `radio` / `tree-select` | option_name 匹配 → `"option_id"` 纯字符串 |
| `multi-select` | **stringified** 如 `"[{\"option_id\":\"xxx\"}]"` |
| `tree-multi-select` | **stringified 字符串一维数组** `"[\"id1\",\"id2\"]"`（禁对象数组） |
| `multi-text` | Markdown 字符串 |
| `date` | 毫秒时间戳字符串，如 `"1722182400000"` |
| `schedule` | **stringified** `"[开始ms,结束ms]"` |
| `precise_date` | **stringified** `"{\"start_time\":...,\"end_time\":...}"` |
| `signal` | `"true"` / `"false"` / `"null"` |
| `workitem_related_select` | 工作项 ID 字符串（数字或字符串按空间配置） |
| `workitem_related_multi_select` | **stringified** ID 数组，**禁止写入自身 ID**（防循环引用，触发 `exists loop` 报错） |
| `file` / `multi-file` | 先 `meegle attachment +upload --resource-type 15 --project-key <K> --work-item-id <id> --field-key <field_key> <local-path>` 拿 `file_token`，再 **stringify** 数组 `"[{\"name\":\"a.pdf\",\"type\":\"application/pdf\",\"size\":\"12345\",\"fileToken\":\"<token>\"}]"`（`fileToken` 驼峰、`size` 字符串） |

> **用户提供的是工作项名称而非 ID** 时，按主文档 [SKILL.md](../SKILL.md)「关联工作项名称 → ID 转换」完整流程（获取目标约束 → `workitem query` 搜索 → 消歧 → 按类型写入）处理。

### STEP 4 — 执行更新

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --role-operate '{{role_operate}}' --fields '[{"field_key":"priority","field_value":"option_id"}]' --format json
```

**角色操作**通过 `role_operate` 参数：
```json
[{"op": "add", "role_key": "角色key", "user_keys": ["userkey1"]}]
```

**拉群方式更新（`group_type` 逻辑字段）**：服务端把 `group_id` / `chat_group` 合并到 `group_type` 单字段，统一通过它读写——**禁止再单独写 `group_id` / `chat_group`**。写协议 `field_value`：

```json
{"type": "auto" | "bind" | "disabled", "group_id": "oc_xxx"}
```

⚠️ **读写不对称**：读返回的判别键是 `value`（如 `{"value":"auto","label":"自动拉群","group_id":"oc_xxx"}`），写要求的判别键是 `type`。不能直接把读到的结构丢回 update。

| `type` | 含义 | 是否带 `group_id` | 客户端预校验 |
|---|---|---|---|
| `auto` | 自动拉群 | 不允许 | 带了 → 阻断（服务端会回 `group_type conflicts with group_id: type=auto`） |
| `bind` | 绑定现有群 | **必填**，非空 `oc_xxx`（空串/纯空格也算缺失） | 缺失或全空白 → 阻断（服务端会回 `group_id is required when group_type=bind`） |
| `disabled` | 不拉群 | 不允许 | 带了 → 阻断（服务端会回 `group_type conflicts with group_id: type=disabled`） |

写法示例（详见 [api-examples.md](api-examples.md) 工作项域）：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --role-operate '{{role_operate}}' --fields '[{"field_key":"group_type","field_value":"{"type":"bind","group_id":"oc_xxx"}"}]' --format json
```

### STEP 5 — 返回结果

展示修改了哪些字段及修改后的值。

---

## 增量追加 (Append SOP)

当用户要求"追加"、"添加"内容时（而非覆盖），必须遵循：

**1. 获取旧值** → 调 `workitem get` 或 `workitem query` 查当前值

**2. 合并新旧值**：

| 字段类型 | 合并方式 |
|---------|---------|
| 文本（`text` / `multi-text`） | 新文本拼接到旧文本后面 |
| 多选枚举（`multi-select`） | 旧选项 + 新选项，`[{"option_id":"xxx"}, ...]` |
| 树状多选（`tree-multi-select`） | 纯字符串一维数组 `["id1", "id2"]`，去重 |
| 关联工作项（`workitem_related_multi_select`） | 旧 ID + 新 ID，去重后写入（只能绑定同空间或白名单空间实例） |
| 多选人员（`multi-user`） | 旧 userkey + 新 userkey，去重 |
| 附件（`multi-file`） | 取旧附件数组，把新 +upload 拿到的 `{name,type,size,fileToken}` 拼上去，整体 stringify 写回（`update` 是覆盖语义，不取旧值会丢历史附件） |

**3. 覆盖写入** → 通过 `workitem update` 写入合并后的值

### 角色追加与删除

| 操作 | `role_operate` 参数 |
|------|-------------------|
| **追加角色人员** | `{"op": "add", "role_key": "xxx", "user_keys": ["userkey"]}` — 不需要先获取旧值 |
| **清空角色人员**（保留角色） | `{"op": "remove", "role_key": "xxx", "user_keys": []}` |
| **彻底删除角色** | `{"op": "remove", "role_key": "xxx", "user_keys": ["_all"]}` |

### 动态 MQL 查询追加

当用户以自然语言条件要求追加关联项（如"将名称包含'依赖'的所有需求追加到前置依赖中"）时：
1. 用 `workitem query` 检索符合条件的工作项
2. 提取 ID 列表
3. 按【获取旧值 → 合并 → 写入】流程追加

---

## 边界说明

| 场景 | 处理 |
|------|------|
| **节点级字段**（排期、估分、节点负责人） | 不属于本 Skill，需用 `workflow update-node`。如检测到用户要改节点字段，自动调 `workflow update-node` |
| **角色更新** | 必须通过 `role_operate`，不能放在 `fields` 里 |
| **模板切换**（修改 template） | 高风险操作，这是**唯一需要提醒用户确认**的场景 |

---

## 不可写入的字段类型

以下字段不支持 API 写入，遇到时**直接跳过并告知用户**：

| 类型 | 原因 |
|------|------|
| `vote-boolean`（轻量表态） | 计数器，只能页面操作 |
| `vote-option` / `vote-option-multi`（投票） | 不支持接口伪造 |
| `compound_field` / `multi_user_compound_field`（复合明细表） | API 暂不支持 |
| 计算字段 | 系统自动计算，只读 |

**富文本与关联字段**：
- **富文本/多行文本** → 直接传 Markdown 字符串
- **关联云文档** → URL 数组 `["https://xxx.feishu.cn/docx/xxx"]`
- **前置依赖/关联工作项** → 工作项 ID，不同空间可能要求字符串或数字格式，遇类型校验失败立刻切换；用户给的是名称而非 ID 时，按主文档 [SKILL.md](../SKILL.md)「关联工作项名称 → ID 转换」完整流程处理
- **signal 类型** → 纯字符串 `"true"`/`"false"`/`"null"`
- **级联选项 (tree-select)** → 只传 `option_id` 纯字符串；报 `不满足层级配置` 时查 `children` 树找末级叶子节点，**展示给用户选择**
- **循环引用保护** → 关联字段写入前**必须排查当前工作项自身 ID**，禁止将自身 ID 写入关联项，否则触发 `exists loop`（循环引用）报错

---

## 错误自动恢复（自愈机制）

> 通用自愈规则（格式错误、级联层级、枚举不合法）见主文档 [SKILL.md](../SKILL.md)「通用自愈规则」。以下为本 Skill 补充规则：

| 报错特征 | 自愈动作 |
|---------|---------|
| 字段名匹配不到 | 用 `field_query` 模糊搜索取最佳匹配 |
| 枚举值匹配不到 | 模糊匹配 option_name（包含关系），失败则列出所有选项 |
| 角色 key 不确定 | 用 `role_query` 模糊搜索 |

---

## 熔断机制 (Circuit Breaker)

> 通用熔断规则（空间未找到、权限不足）见主文档 [SKILL.md](../SKILL.md)「通用熔断规则」。以下为本 Skill 补充规则：

1. **工作项类型未找到**：`workitem meta-types` 失败超过 3 次
2. **字段转换大面积失败**：转换失败比例 > 60%，终止并列出失败字段明细

---

## 常见问题

| 问题 | 处理 |
|------|------|
| 用户未指定工作项 | 追问 ID 或 URL |
| 字段名匹配不到 | `workitem meta-fields(field_query="关键词")` 模糊查询 |
| 枚举值匹配不到 | 展示所有枚举值让用户选 |
| 人名匹配到多人 | 展示列表让用户指定，**禁止自行选择** |
| 用户要"追加"而非覆盖 | 走增量追加 SOP |
| 用户要改节点字段 | 自动切换到 `workflow update-node` |
