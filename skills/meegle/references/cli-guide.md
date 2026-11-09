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
| `--input-file @path` | 从一个本地 UTF-8 JSON 对象文件读取业务参数（上限 32 MiB，与 `--params` 互斥） |
| `--output-file <JSON>` | 按 `{ "path": "result.json", "fields": ["work_item_fields"] }` 导出结果字段，支持单对象、对象数组或重复对象 |
| `--select <props>` | 选取输出属性，逗号分隔（支持 dot path，如 `name,owner.name`） |
| `--profile <name>` | 临时切换 profile |
| `--verbose` | 显示详细日志 |
| `--refresh` | 从服务端刷新本地命令缓存（旁路 24h cache） |

## 参数传递

可使用以下方式传递业务参数（`--fields` 属于普通 flag）：

1. **Flag 模式**（推荐）：`--project-key PROJ --work-item-type story`
2. **--fields 模式**（写工作项字段，可重复）：`--fields '{"field_key":"name","field_value":"任务标题"}' --fields '{"field_key":"priority","field_value":"1"}'`
3. **--params 模式**（完整 JSON 兜底）：`--params '{"fields":[{"field_key":"name","field_value":"任务标题"}]}'`
4. **--set 模式**（支持点路径，不支持 fields[] 数组索引）：`--set page_num=1` 等价于 `--page-num 1`，支持 dot-path 嵌套；不要用它写工作项字段

合并优先级为 **普通 flag > `--set` > `--params`**；文件参数的规则见下文。

**`field_value` 填写规则**：固定填字符串。标量直接填字符串，如 `"100"`、`"true"`；数组或对象填序列化后的 JSON 字符串，如 `"[\"u1\",\"u2\"]"`、`"{\"type\":\"auto\"}"`。不要给 `field_value` 传原生 number、array 或 object。

此限制只针对 `field_value`；`fields`、`role_operate`、`node_schedule` 等参数仍按各自 schema 传原生数组或对象。

## 文件输入与指定字段落盘

### 先检查当前 CLI 能力

Skill 与 CLI 独立更新，不能仅凭本指南存在就假定已安装的 CLI 支持文件参数。首次使用前查看目标命令的 `--help`，确认包含本次要使用的 `--input-file` / `--output-file`：

```bash
meegle workitem get --help
```

缺少参数时停止使用该选项，提示升级到支持它的 CLI 版本；不要把新参数塞入 `--params` / `--set`，也不要反复重试。若当前可安装版本仍不支持，继续使用已有参数、字段精简和分页方式，不宣称文件能力可用。

### 输入文件

`--input-file @request.json` 只允许传一次，文件根节点必须是 JSON 对象，可包含多个业务参数及数组、嵌套对象。与 `--params` 互斥；同名顶层参数优先级为 **直接 flag > `--set` > 输入文件**。文件中不能设置认证、profile、输出格式或本地文件控制选项，不递归展开其中的 `@path` 字符串。业务参数自身的类型要求不变，例如 `field_value` 仍按上文传字符串。

`request.json` 示例（替换为已确认的空间与工作项 ID）：

```json
{"project_key":"PROJ","work_item_id":"12345","fields":["description","watchers"]}
```

### 输出文件

每个输出对象只接受 `path` 和非空字符串数组 `fields`。单对象可保存多个字段；多个输出文件可用一个对象数组，或重复 `--output-file`、每次传一个对象。**不能混用数组值和重复选项。** 以下示例使用双引号及转义，可用于 Shell 或命令字符串调用；外层若再包 JSON，需按外层格式再次转义。

```bash
# 多个字段保存到一个文件
meegle workitem get --input-file @request.json --output-file "{\"path\":\"./details.json\",\"fields\":[\"work_item_fields\",\"work_item_current_node\"]}"

# 对象数组：两个文件
meegle workitem get --input-file @request.json --output-file "[{\"path\":\"./fields.json\",\"fields\":[\"work_item_fields\"]},{\"path\":\"./name.json\",\"fields\":[\"work_item_attribute.work_item_name\"]}]"

# 重复对象：与上面的两个文件示例等价
meegle workitem get --input-file @request.json --output-file "{\"path\":\"./fields.json\",\"fields\":[\"work_item_fields\"]}" --output-file "{\"path\":\"./name.json\",\"fields\":[\"work_item_attribute.work_item_name\"]}"
```

每次选择一种形式，并使用尚不存在的目标路径。字段路径从 **CLI 包装前的业务结果** 开始；`fields` 是结果字段路径，不是业务请求中的 `--fields` 参数。支持对象点路径和整个数组，不支持数组下标、数组遍历、通配符或包含点的字面字段名。不存在的字段、跨文件重复字段及父子路径重叠会失败；同一文件内相同字段去重。

文件始终是 UTF-8 JSON，保留字段名、层级及类型，单字段或 `.txt` 后缀也不输出裸文本。例如保存 `work_item_attribute.work_item_name` 后，文件结构为：

```json
{"work_item_attribute":{"work_item_name":"示例需求"}}
```

Agent 收到 `{data, meta, error}`：`data` 是移除已保存字段后的剩余业务结果，空父对象保留；`meta.output_files` 包含每个文件的绝对 `path`、`fields` 和 `format: "json"`，原有元数据继续保留。`null`、空字符串、空数组和空对象正常保存。输出文件不保证符合另一条命令的入参结构，不能未经检查直接作为其输入文件。

### 路径与失败处理

- 路径属于 **CLI 执行进程的文件系统**，相对路径以该进程工作目录为基准。输入须是本地普通文件，不支持 URL、管道或 `-`；输出父目录须已存在。不自动上传、下载或同步到用户电脑。没有对应环境的文件读写工具时，不要假定能创建输入文件或读取返回路径。
- 文件输出固定为 JSON 包装，与 `--select`、非 JSON 格式、`--dry-run`、显式 `--envelope=false` 互斥。仅使用输入文件时允许 `--dry-run`，但预览会包含展开后的参数，可能很大。
- 不覆盖已有目标（包括符号链接）。每个文件独立原子发布，要求文件系统支持同目录硬链接；多文件不是整体事务，正常捕获的失败或取消会清理未发布临时文件，强制终止或断电不承诺清理。
- 业务执行后的落盘失败返回非零退出码和 `FILE_OUTPUT_FAILED`。检查 `meta.business_execution`、已完成的 `meta.output_files` 及 `meta.output_failure`（`stage`、`path`）；`completed` 表示业务已返回，不保证批量任务全部成功。**不要因文件输出失败自动重试业务命令**，尤其是创建、更新等写操作；已完成文件应保留，不回退把大字段全部打印到 Agent 上下文。

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
