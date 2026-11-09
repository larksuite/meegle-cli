# 性能与并发调用指南

本文件收录减少延迟的工程性规则。核心协议（字段格式、错误自愈）仍在 SKILL.md 主文件中。

## 并行调用

无依赖的命令调用应并行发起，有依赖则必须串行。

**必须串行**（前者输出是后者输入）：
- `project search` → `workitem meta-fields` → `workitem query`
- `workitem get` → `workflow transition` / `workitem update`
- `workitem meta-fields` → `workitem create`

**可并行**：
- `workitem meta-fields` 和 `workitem meta-roles`（同类型）
- 多种工作项类型的 `workitem meta-fields`（如 story + issue）
- 各条件的 count 查询、多人排期分批查询

## 大结果处理

- **分批查询**：`workhour list-schedule` 多人时拆成每批 ≤ 20 人并行
- **精简 SELECT**：只选必要字段，避免富文本等大体积字段
- **按需翻页**：先读首页取 `list[0].count`；`workitem query` 要全量时用 `--session-id` + `--group-pagination-list` 逐页取（SKILL.md「翻页取全量」），不要用 `LIMIT offset, n`
- **文件输入输出**：参数内容已经在 CLI 执行环境的文件中时，用 `--input-file @path`；需要保留大字段正文、但不需要 Agent 立即阅读全文时，用 `--output-file` 将字段保存到文件，让上下文只保留路径及剩余数据。先按 [CLI 使用指南](cli-guide.md#文件输入与指定字段落盘) 检查当前 CLI 是否支持，确认路径在执行环境中可访问；没有文件读写工具时不要假设可访问返回路径。
- **大小边界**：输入文件上限 32 MiB，普通 MCP 响应仍受 32 MiB 完整响应限制，分页合并仍占用内存。文件输出减少上下文占用，不提供流式传输或恒定内存保证；未选择的大字段仍会返回。超限时应缩小查询、按需分页，或使用业务提供的文件下载能力，不要只添加输出文件参数后重试同一超限请求。
