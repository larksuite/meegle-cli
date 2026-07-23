# 流转工作项状态（状态流）

> **CRITICAL** — 开始前 MUST 先用 Read 工具读取 `../SKILL.md`，其中包含前置检查、授权流程、命令参数参考、字段值格式、通用规范和错误处理。

本 Skill 用于**状态流工作项**（如缺陷 / issue）的状态流转，全程自动化编排。

> ⚠️ **仅限状态流**。需求 / story 等节点流工作项请改用 `workflow transition`（action=confirm/rollback），不要混用本 Skill。

---

## 执行流程

### STEP 1 — 定位工作项 + 获取当前用户（并行）

**并行执行**：

1. **定位工作项**：从用户输入中提取 `work_item_id`、`project_key`、`work_item_type`。
   - **URL 解析**：用户给了链接则先调 `url decode`。只有 `url_kind == workitem_detail` 才能进入本 SOP；其他 kind 按 [url-kinds.md](url-kinds.md) 拒绝或追问。decode 返回的 `simple_name` 必须再调 `project search` 转为权威 `project_key`（同名空间可能有多个无权限）。**禁止**自己从 URL 截取路径段作参数。
   - **ID 类型**：传给任何工具的 `work_item_id` 必须是 **字符串（String）**。
   - 信息不足才追问。
2. **获取当前用户**：调用 `user search`，入参 `["current_login_user()"]` 拿到当前用户的 `user_key`（下一步必填）。

### STEP 2 — 查询可流转状态并匹配目标

```bash
meegle workflow list-state-transitions --work-item-id 工作项ID --work-item-type 类型key --user-key 当前用户userkey --project-key 空间key --format json
```

- **匹配目标状态**：精确 / 模糊 / 语义匹配用户意图（如"关闭" → "已关闭"，"解决" → "已解决"）拿到对应的 `transition_id`。
- 🚨 **未明确目标状态且有多个候选时**：**必须展示所有可选项让用户选择**，不得替用户默认选择。

### STEP 3 — Fail-fast 直接尝试流转

**不要前置查询必填项**，直接调用 `workflow transition-state`：

```bash
meegle workflow transition-state --work-item-id 工作项ID --project-key 空间key --transition-id 上一步拿到的ID --format json
```

- **流转成功** → 跳到 STEP 6 返回结果。
- **失败且提示必填字段未填** → 进入 STEP 4 按需补充。
- **失败其他报错** → 参考下方「智能修复」章节。

### STEP 4 — 按需收集并补充必填字段（仅流转失败时触发）

调用 `workflow list-state-required` 获取目标状态所需必填项：

```bash
meegle workflow list-state-required --work-item-id 工作项ID --state-key 目标状态key --project-key 空间key --mode {{mode}} --format json
```

> 若工具支持 `mode` 参数（如 `mode="unfinished"`），优先只查尚未填写的字段，减少噪音。

**4.1 硬拦截：不支持 API 更新的字段类型**

遇到以下字段被设为必填，**立即中断流转**并提示用户在页面手动填写：

- `vote-boolean` / `vote-option` / `vote-option-multi`（投票类）
- 计算字段（系统自动计算，只读）

> **复合明细表可先通过 `workitem update` 补充**，但 `compound_field` 与 `multi_user_compound_field` 的写协议不同；多人复合字段还是整体覆盖，必须先读取并保留全部人员和非目标数据。格式详见主文档 [SKILL.md](../SKILL.md)「字段值格式 → 复合明细表」章节；无法自动判断子字段值或人员范围时仍需询问用户。

> 中断话术示例：「流转失败。当前状态需要填写【字段名】，该字段不支持自动化补充，请在页面手动填写后通知我继续。」

**4.2 枚举选项的前置查询（批量 + 精准）**

缺失项包含枚举类（select/radio/multi-select/tree-select 等）时：

- 将所有目标 `field_key` 数组**一次性**传入 `workitem meta-fields` 的 `field_keys` 精准查询，拿到 `option_name` 与 `option_id`。**绝不逐页遍历全量配置。**
- 将所有必填项及可选值**汇总为一条消息**向用户展示并询问。

```bash
meegle workitem meta-fields --page-num 1 --project-key 空间key --work-item-type 类型key --field-types '{{field_types}}' --field-keys '["key1","key2"]' --field-query '{{field_query}}' --format json
```

**4.3 字段 Mock 与询问边界（安全底线）**

- **业务决策类、人员、日期、枚举类字段** → **禁止 AI 编造或 mock 数据**。必须列出并询问用户。
- **人员字段（user/multi-user）** → 搜出多个同名或用户未指定时必须确认，**不可默认填当前操作者**（除非用户明确说"分配给我"/"我来处理"）。
- **打回/关闭原因等纯说明性文本**（如 "Reopen 原因"、"流转说明"）在用户未提供且字段语义不关键时，可默认填 `"重新打开处理"` 或 `"发起流转"`。

**4.4 字段值格式**

拿到用户确认值后，按主文档 [SKILL.md](../SKILL.md)「字段值格式」章节转换为 `field_value`。

> 🚨 **关键约定**：`field_value` 协议层是 **STRING**。标量直接传字符串；数组/对象**必须 JSON.stringify**，否则报 `need STRING type, but got: LIST`。

| 字段类型 | field_value 传参 |
|---------|-----------------|
| `text` / `multi-pure-text` / `link` | 字符串直接传入 |
| `number` | 字符串化数字，如 `"100"` |
| `bool` | `"true"` / `"false"` |
| `user` | 单个 userkey，如 `"7509072868295085608"` |
| `multi-user` | **stringified**，如 `"[\"key1\",\"key2\"]"` |
| `select` / `radio` / `tree-select` | **纯字符串 `option_id`**（🚨 不要传 value/label 的 JSON） |
| `multi-select` | **stringified**，如 `"[{\"option_id\":\"xxx\"}]"`（若字段配置允许新增选项且用户明确提出新值，可生成 8 位随机小写加下划线格式的 option_id 填入） |
| `tree-multi-select` | **stringified 字符串数组**，如 `"[\"id1\",\"id2\"]"`（🚨 不可对象数组） |
| `multi-text`（富文本） | Markdown 字符串 |
| `date` | 毫秒时间戳，如 `"1722182400000"` |
| `schedule` | **stringified**，如 `"[1722182400000,1722355199999]"` |
| `precise_date` | **stringified**，如 `"{\"start_time\":...,\"end_time\":...}"` |
| `workitem_related_select` | 关联工作项 ID 字符串 |
| `file` / `multi-file` | 先 `meegle attachment +upload --resource-type 15 --project-key <K> --work-item-id <id> --field-key <field_key> <local-path>` 拿 `file_token`，再 **stringify** 数组 `"[{\"name\":\"a.pdf\",\"type\":\"application/pdf\",\"size\":\"12345\",\"fileToken\":\"<token>\"}]"`（`fileToken` 驼峰、`size` 字符串） |

> **用户提供的是工作项名称而非 ID** 时，按主文档 [SKILL.md](../SKILL.md)「关联工作项名称 → ID 转换」完整流程（获取目标约束 → `workitem query` 搜索 → 消歧 → 按类型写入）处理。

### STEP 5 — 补完字段后再次流转

所有必填字段通过 `workitem update` 写入后，再次调用 `workflow transition-state` 触发流转：

```bash
meegle workitem update --work-item-id 工作项ID --project-key 空间key --role-operate '{{role_operate}}' --fields '[{"field_key":"xxx","field_value":"yyy"}]' --format json
```

```bash
meegle workflow transition-state --work-item-id 工作项ID --project-key 空间key --transition-id transition_id --format json
```

### STEP 6 — 返回结果

向用户展示：

- 状态变更方向（**从 XX → YY**）
- 自动 / 协助填入的必填项摘要
- 工作项 ID 与（如返回包含）链接

---

## 智能修复（自愈机制）

> 通用自愈规则（格式错误、级联层级、枚举不合法）见主文档 [SKILL.md](../SKILL.md)「通用自愈规则」。本 Skill 无额外补充规则。

---

## 熔断与终止

> 通用熔断规则（空间未找到、权限不足）见主文档 [SKILL.md](../SKILL.md)「通用熔断规则」。以下为本 Skill 补充规则：

1. **必填字段全部为硬拦截类型**（投票/计算字段），无法通过接口写入。
2. **同一目标状态**：补字段 → 再次流转连续失败 **> 2 次**。

---

## 常见问题

| 问题 | 处理 |
|------|------|
| 用户只说"关闭这个 bug"没给空间 | 如 URL 可解析则用 URL；否则向用户确认 |
| 可流转状态为空 | 说明当前状态无合法下一步，告知用户在页面核对状态流配置 |
| 用户说"改为 XX"但 XX 不在可流转列表 | 展示当前可流转状态列表，让用户重新选择 |
| 工作项实际是节点流 | 告知用户走 `workflow transition`（action=confirm/rollback），本 Skill 仅处理状态流 |
| 同名字段多个 option | 展示全部 option_name 让用户选，**禁止默认取第一项** |
| 同名人员多个 | 展示列表让用户指定，**禁止默认填当前操作者** |
