# 错误处理详细规则

SKILL.md 主文件已经收录错误处理总则与熔断条件，本文件提供完整的自愈规则与错误速查表。

## 自愈规则（按报错特征匹配修复后重试）

| 报错特征 | 自愈动作 |
|---------|---------|
| `need STRING type, but got: LIST` / `MAP` | field_value 从原生 JSON 改为 JSON.stringify 后的字符串（见 SKILL.md「字段值格式」） |
| `cannot unmarshal object...` | 仅改变格式（数字↔字符串、单值↔数组、对象↔纯字符串），值不变 |
| `不满足层级配置`（级联层级错误） | 查 `children` 树，展示末级叶子节点让用户选择 |
| `invalid select option(s)`（枚举不合法） | 从 `possible values` 匹配；唯一匹配则修正重试，否则询问用户 |

## 错误速查

| 现象 | 排查/修复 |
|------|---------|
| 找不到空间 / 中文名匹配多个空间 | `project search` 验证，取 project_key 精确调用 |
| 找不到工作项类型 | `workitem meta-types` 确认合法 type_key |
| 字段名错误 / MQL 返回为空但数据存在 | `workitem meta-fields` 确认字段 key 与类型 |
| MQL 查询失败 | FROM 用 `` `空间名`.`工作项类型` ``；数组字段改用 `array_contains` / `any_match` |
| 日期区间字段查询失败 | 用子字段 `` `__字段名_开始时间` `` |
| 角色查询无结果 | MQL 角色名用 `` `__{角色名}` `` 格式 |
| 人名/团队名重复 | MQL 用 `<id:xxxx>` 消歧（见 MQL 语法参考） |
| 人名→userkey 失败 | `user search` 批量查询 |
| 人员字段写入失败 | user 传单个 userkey 字符串；multi-user 必须 stringified 如 `"[\"k1\",\"k2\"]"` |
| node not found | 先 `workitem get` 获取真实 node_id，禁止猜测 |
| 节点流转失败 | 节点流用 `workflow transition`；状态流用 `workflow transition-state`（先 `workflow list-state-transitions` 取 transition_id，再 `workflow list-state-required` 查必填项） |
| 创建工作项缺少模板 | `workitem meta-fields(field_keys=["template"])` 获取 |
| 角色更新失败 | 改用 `workitem update` 的 `role_operate` 参数（不走 fields） |
| `group_id is required when group_type=bind` | 更新 `group_type` 时 `type=bind` 必须带 `group_id`，并且不能是空串或纯空格；改成 `{"type":"bind","group_id":"oc_xxx"}` 后重试。要解绑群改用 `{"type":"disabled"}` |
| `group_type conflicts with group_id: type=<auto|disabled>` | 同时传了 `type=auto` 或 `type=disabled` 又带了 `group_id`；保留 `type=bind` + `group_id`，或去掉 `group_id` 后重试 |
| `need I64 type, but got: STRING`（page_size / page_token） | `page_size` 被当成字符串传出；meegle CLI 改用 `--params '{"page_size":N,"page_token":"<token>"}'` 让数字以 JSON number 传出 |
| mywork.todo 需选择工作区 | 按报错中的列表把 `asset_key`（Asset_xxx）传入重试 |
