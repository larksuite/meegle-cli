# 字段值进阶：关联工作项名称 → ID 转换

当用户为 `workitem_related_select` / `workitem_related_multi_select` 字段提供的是**工作项名称而非 ID** 时，按以下流程转换后再写入。

1. **获取关联字段的目标约束**：从 `workitem meta-fields` 返回的该字段配置中，提取其绑定的**目标空间**（`project_key`）和**目标工作项类型**（`work_item_type_key`）。若配置未限定（可关联任意类型），默认在当前空间内搜索。
2. **按名称搜索目标工作项**：调用 `workitem query`，在目标空间和类型范围内按名称匹配。示例 MQL：
   ```sql
   SELECT `工作项ID`, `名称` FROM `目标空间`.`目标类型` WHERE `名称` = '用户给的名称'
   ```
   精确匹配无结果时改用 `like '%关键词%'` 模糊搜索。
3. **消歧处理**：
   - 唯一结果 → 直接取工作项 ID
   - 多个结果 → 列出所有匹配项（ID + 名称 + 状态）让用户确认
   - 零结果 → 提示用户"未找到名为 XXX 的工作项，请确认名称或直接提供 ID"
4. **写入格式**：
   - `workitem_related_select` → 传入单个 ID 字符串
   - `workitem_related_multi_select` → 传入 stringified ID 数组
   - 不同空间可能要求字符串或数字格式，遇类型校验失败立刻切换格式重试
5. **循环引用保护**：写入前必须排查当前工作项自身 ID，**禁止将自身 ID 写入关联字段**，否则会触发 `exists loop`（循环引用）报错。
