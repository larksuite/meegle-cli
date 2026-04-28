# CLI 使用指南

## 前置条件

运行环境需要 Node.js 18+。所有命令通过 `meegle` 执行。

## 命令结构

```bash
meegle <resource> <method> [flags] --format json
```

命令采用 `resource method` 两级结构。所有输出推荐使用 `--format json` 获取结构化数据。

## 全局 Flag

| Flag | 说明 |
|------|------|
| `--format json\|table\|ndjson` | 输出格式，默认 json |
| `--select <props>` | 选取输出属性，逗号分隔（支持 dot path，如 `name,owner.name`） |
| `--profile <name>` | 临时切换 profile |
| `--verbose` | 显示详细日志 |
| `--refresh` | 从服务端刷新本地命令缓存（旁路 24h cache） |

## 参数传递

几种方式，优先级从高到低：

1. **Flag 模式**（推荐）：`--project-key PROJ --work-item-type story`
2. **--fields 模式**（写工作项字段，可重复）：`--fields '{"field_key":"name","field_value":"任务标题"}' --fields '{"field_key":"priority","field_value":"1"}'`；`field_value` 支持任意 JSON 值（数组/对象原样传）
3. **--params 模式**（完整 JSON 兜底）：`--params '{"fields":[{"field_key":"name","field_value":"任务标题"}]}'`
4. **--set 模式**（仅顶层参数快捷写法，不支持 fields[]）：`--set page_num=1` 等价于 `--page-num 1`，支持 dot-path 嵌套；不要用它写工作项字段

Flag 覆盖 `--params`；`--set` 只影响顶层参数，**不会**写到 `fields[]`。

## 命令发现

CLI 的命令和参数会随版本更新。遇到不确定的命令或参数时，使用 `inspect` 获取最新信息：

```bash
meegle inspect                    # 列出所有可用命令
meegle inspect workitem.create    # 查看具体命令的参数 schema
```

> 命令清单本地缓存 24 小时。如果 `inspect` 输出的参数与服务端实际不符，或服务端有新命令但 CLI 报 `unknown command`，加上 `--refresh` 强制从服务端重新拉取最新清单：
> ```bash
> meegle --refresh inspect workitem.create
> ```

## 输出处理

- 始终使用 `--format json` 获取结构化输出，方便解析
- 使用 `--select` 精简返回字段，如 `--select id,name,current_nodes.name`
- 命令返回错误时，JSON 中包含 `error` 和 `message` 字段
