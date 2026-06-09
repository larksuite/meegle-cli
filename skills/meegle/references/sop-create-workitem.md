# 创建工作项

> **CRITICAL** — 开始前 MUST 先用 Read 工具读取 `../SKILL.md`，其中包含前置检查、授权流程、命令参数参考、字段值格式、通用规范和错误处理。

本技能用于在飞书项目中创建工作项（需求、任务、缺陷等），全程自动化执行，无需用户二次确认。

---

## 执行流程

### STEP 1 — 提取意图

从用户输入中提取：
- **空间名**：哪个项目空间
- **工作项类型**：需求 / 任务 / 缺陷 / 其他
- **字段值**：标题、优先级、负责人、描述等
- **URL**（如有）：先调 `url decode` 解析。`url_kind` 非 `workitem_create` / `workitem_detail` 时按 [url-kinds.md](url-kinds.md) 拒绝或追问；命中则用返回的 `simple_name` 取代空间名探测，`work_item_type` 取代类型探测。**禁止**自己从 URL 截取路径段作参数。


### STEP 2 — 确认空间和类型

1. 用 `project search` 验证空间 → 获取 `project_key`
2. 用 `workitem meta-types` 获取类型列表 → 确认 `work_item_type`

> 唯一匹配则直接用，多个匹配则展示列表让用户选，无匹配则问用户。**禁止猜测。**

> 资源工作项模板创建：如果用户明确表示“通过资源库模板/资源工作项创建”，或 URL 解析得到资源工作项模板实例 ID，则将该 ID 作为 `--work-item-id` 传给 `workitem create`；此时按工具约束，`--work-item-id` 是必填的资源模板实例 ID。

### STEP 3 — 收集元数据（并行）

同时发起以下调用：

| 调用 | 目的 |
|------|------|
| `workitem meta-create-fields` | 获取该类型的**必填字段**元信息（前置校验，防止因空间自定义必填项缺失导致创建失败） |
| `workitem meta-fields(field_keys=["template"])` | **获取模板 ID**（创建必填） |
| `workitem meta-fields(field_query="用户提到的字段名")` | 确认字段 key、类型、枚举值（通过 `field_keys` 精确匹配或 `field_query` 模糊搜索，避免全量拉取时因分页导致自定义字段遗漏） |
| `workitem meta-roles(page_num=1)` | 获取角色定义（如用户指定了负责人等角色） |

如用户提到了人名，并行调用 `user search` 转换为 userkey。

> 默认只查询在职用户。只有当用户明确要求包含离职、停用等非在职人员，或在职结果为空且用户要求继续查找时，才给 `user search` 传 `--need-all-status=true`。

### STEP 4 — 自动匹配模板

根据 STEP 3 获取的模板枚举值：
- **只有一个模板** → 自动选择
- **多个模板** → 根据用户描述中的关键词匹配最接近的模板名，选不出来时展示列表让用户选
- **用户明确指定了模板名** → 精确匹配

### STEP 5 — 自动补全必填项与转换字段值

**1. 自动补全必填项**

严格对比 `workitem meta-create-fields` 返回的必填字段与用户已给定的字段。对于缺失的必填项（尤其是空间自定义的特殊必填项），自动生成合理的默认值：
- 枚举：默认取第一项
- 文本：填"自动生成"
- 布尔：填 true

**2. 转换字段值**

将用户给的自然语言值及自动生成的默认值转换成 API 需要的格式：

| 来源 | 转换 |
|------|------|
| 人名 | 调用 `user search` 批量转换为 userkey |
| 枚举值 | 从字段配置的 options 中按 option_name 匹配得到 option_id |
| 日期 | 转为毫秒时间戳 |
| 其他类型 | 按主文档 [SKILL.md](../SKILL.md)「字段值格式」规范转换 |

**格式速查**（完整格式见主文档 [SKILL.md](../SKILL.md)「字段值格式」）：

> 🚨 **关键约定**：`field_value` 协议层是 **STRING**。标量（text/user/option_id/timestamp）直接传字符串；数组、对象**必须先 JSON.stringify 成字符串**，否则会报 `need STRING type, but got: LIST`。

| 字段类型 | field_value 传参 |
|---------|-----------------|
| text | `"文本内容"` |
| select / radio | `"option_id"`（从字段配置获取） |
| user | `"userkey"` |
| multi-user | `"[\"userkey1\",\"userkey2\"]"`（stringified） |
| schedule | `"[开始时间戳,结束时间戳]"`（stringified） |
| file / multi-file | 先 `meegle attachment +upload --resource-type 15 --project-key <K> --work-item-type <type> --field-key <field_key> <local-path>` 拿 `file_token`（工作项尚未创建，传 `--work-item-type` 而非 `--work-item-id`），再 **stringify** 数组 `"[{\"name\":\"a.pdf\",\"type\":\"application/pdf\",\"size\":\"12345\",\"fileToken\":\"<token>\"}]"`（`fileToken` 驼峰、`size` 字符串） |

**角色设置**（创建时）：通过 fields 中的 `role_owners` 字段，值为 stringified 对象数组：

```json
{"field_key":"role_owners","field_value":"[{\"role\":\"RD\",\"owners\":[\"userkey1\"]}]"}
```

> **角色补充**：创建时除了能在 `fields` 数组内处理极少部分内置的 role_owners 字段外，针对其他自定义角色（如 PO、PM、Tech Lead 等），请在创建后用 `workitem update` 的 `role_operate` 参数追加写入。

### STEP 6 — 创建

```bash
meegle workitem create --work-item-type 类型key --fields '[{"field_key":"template","field_value":"模板ID"},{"field_key":"name","field_value":"标题"}]' --project-key 空间key --work-item-id {{work_item_id}} --ignore-required {{ignore_required}} --ignore-role-calculate {{ignore_role_calculate}} --format json
```

仅在用户明确要求跳过创建校验，或业务流程已确认由后端/后续步骤补齐时，才使用 `--ignore-required` / `--ignore-role-calculate`；默认不要主动开启。

🚨 **批量创建**：当用户要求批量创建多个工作项时，必须**串行调用**（逐个请求），禁止高并发，以免触发平台限流。每个 field_value 均须符合「字段值格式」的 STRING 约定（标量直接字符串化；数组/对象 JSON.stringify）。

### STEP 7 — 确认结果

创建成功后，向用户展示：
- 工作项 ID 和名称
- 链接（如返回中包含）
- 已设置的关键字段摘要

---

## 特殊字段写入规则

### 级联选项 (Tree-select)

1. **格式极简**：对于业务线（`_business`）等级联单选字段，`field_value` **只传 `option_id` 纯字符串**，不要传 `value/label/children` 的复杂 JSON。
2. **层级强校验**：如果报错 `级联选项字段值不满足层级配置`，说明该字段要求选到末级叶子节点。此时查询该选项的 `children` 树，**将末级叶子节点列表展示给用户选择**，禁止自行向下盲猜。

### 不可写入的字段类型

以下字段类型不支持通过 API 写入，遇到时**直接跳过并告知用户原因**：

| 类型 | 原因 |
|------|------|
| `vote-boolean`（轻量表态） | 计数器，只能由用户在界面操作 |
| `vote-option` / `vote-option-multi`（投票） | 不支持通过接口伪造投票结果 |
| `compound_field` / `multi_user_compound_field`（复合明细表） | 内部结构校验复杂，API 暂不支持 |

### 富文本与关联字段

- **富文本/多行文本**：直接传 Markdown 字符串（`# 标题`、`|列1|列2|`、代码块等），完美渲染。
- **关联云文档（PRD）**：传 URL 数组，如 `["https://xxx.feishu.cn/docx/xxx"]`。
- **前置依赖/关联工作项**：传目标工作项 ID。**注意**：不同空间可能要求字符串 `"7093424682"` 或数字格式，遇到类型校验失败立刻切换格式重试。用户提供的是工作项**名称而非 ID** 时，按主文档 [SKILL.md](../SKILL.md)「关联工作项名称 → ID 转换」完整流程处理。
- **系统外信号类型 (`signal`)**：不接收 option_id，传纯字符串 `"true"`、`"false"` 或 `"null"`。

---

## 错误自动恢复（自愈机制）

> 通用自愈规则（格式错误、级联层级、枚举不合法）见主文档 [SKILL.md](../SKILL.md)「通用自愈规则」。以下为本 Skill 补充规则：

| 报错特征 | 自愈动作 |
|---------|---------|
| `json: unsupported type` / 网络超时 | 原参数直接重试 |
| 字段 key 不匹配 | 用 `field_query` 模糊搜索取最佳匹配 |
| 人名解析失败 | 尝试用邮箱前缀再搜一次 |
| 明确缺少必填字段 | 核对字段类型限制，关联工作项尝试数字↔字符串切换 |

---

## 熔断机制 (Circuit Breaker)

> 通用熔断规则（空间未找到、权限不足）见主文档 [SKILL.md](../SKILL.md)「通用熔断规则」。以下为本 Skill 补充规则：

1. **工作项类型未找到**：`workitem meta-types` 失败超过 3 次
2. **字段转换大面积失败**：字段值转换失败比例 > 60%，终止流程并列出失败字段明细，**不要强行创建残缺数据**

---

## 常见问题

| 问题 | 处理 |
|------|------|
| 用户未指定空间 | 问用户 |
| 用户未指定类型 | 如空间只有一种类型则直接用，否则问用户 |
| 用户提到的字段不存在 | `workitem meta-fields(field_query="关键词")` 模糊查询，找不到则告知用户 |
| 模板有多个 | 根据关键词匹配，匹配不到则展示列表让用户选 |
| 枚举值匹配不到 | 展示该字段所有枚举值让用户选 |
| 人名匹配到多人 | 展示完整列表让用户指定，**禁止自行选择** |
