# 附件域

附件上传/下载分两步：先调 `attachment prepare-upload` / `attachment prepare-download` 申请带签名的对象存储 URL，再与对象存储做一次或多次 HTTP 直连。Meegle CLI 内置 `attachment +upload` / `attachment +download` 一键封装，把两步合成一条命令；脚本里需要逐步控制时也可单独调上面的 prepare 命令。
## attachment prepare-upload
申请上传签名。`work_item_id` 与 `work_item_type` **二选一必填**：已有工作项传 `work_item_id`；"创建工作项时同步上传附件" 场景传 `work_item_type`，两者同传时 `work_item_id` 优先。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --resource-type | number | 是 | 附件场景：13=评论附件 / 14=评论图片 / 15=工作项附件字段 / 16=富文本字段图片 |
| --file-name | string | 是 | 附件名称 |
| --mime-type | string | 是 | MIME 类型 |
| --size | number | 是 | 文件总大小（字节）；后端据此判断走单次上传还是分片 |
| --work-item-id | string | 二选一 | 已有工作项 ID |
| --work-item-type | string | 二选一 | 工作项类型（仅 "创建工作项同步上传附件" 场景） |
| --field-key | string | 条件 | `resource_type=15/16` 必填，13/14 不填 |

## attachment prepare-download
申请下载签名。`file_url` 是其它命令（如 `workitem get` 的附件字段值、`comment list` 评论里的附件链接、富文本中的附件引用）回传的不透明引用，**不要**手工拼接。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| --project-key | string | 是 | 空间 key |
| --work-item-id | string | 是 | 工作项 ID |
| --file-url | string | 是 | 附件 URL（来自附件字段、评论或富文本） |

## attachment +upload
端到端上传：CLI 在本地把 `attachment prepare-upload` 与对象存储的签名 HTTP POST 串起来，返回 `file_token` 与文件元数据，可直接喂给 `workitem create` / `workitem update` / `comment add` 的附件字段。**Meegle CLI 专用**。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `<source-path>`（位置参数） | string | 是 | 本地文件路径 |
| --resource-type | string | 是 | 13/14/15/16，含义同 `attachment prepare-upload` |
| --project-key | string | 是 | 空间 key |
| --work-item-id | string | 二选一 | 已有工作项 ID |
| --work-item-type | string | 二选一 | 创建场景的工作项类型 |
| --field-key | string | 条件 | resource_type=15/16 时必填 |
| --filename | string | 否 | 覆盖发送给后端的文件名（默认取本地 basename） |
| --content-type | string | 否 | 覆盖 MIME 类型（默认按扩展名探测，未识别走 `application/octet-stream`） |

## attachment +download
端到端下载：CLI 在本地把 `attachment prepare-download` 与对象存储的签名 HTTP GET 串起来，并用 `.partial` 临时文件 + 原子改名落盘，失败时不会留下半残文件。**Meegle CLI 专用**。

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `<file-url>`（位置参数） | string | 是 | 附件 URL（来自附件字段、评论或富文本） |
| --project-key | string | 是 | 空间 key |
| --work-item-id | string | 是 | 工作项 ID |
| --output | string | 是 | 本地落地路径 |
| overwrite | bool | 否 | 目标已存在时是否覆盖（默认 false） |
