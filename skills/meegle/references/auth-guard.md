# Auth Guard（所有业务命令前必须执行）

## 触发条件

- **主动登录**：用户说"登录 Meegle"、"连接飞书项目"、"login meegle"等。
- **被动拦截**：用户请求任何 Meegle 业务操作（查询待办、查工作项、创建任务等），优先执行 Auth Guard。
- **URL 触发**：用户发送了飞书项目/Meegle URL。处理流程：
  1. 先调 `url decode` 拿到结构化字段（`url_kind`、`host`、`simple_name`、`work_item_id` 等）。**禁止**自己从 URL 截取路径段作参数。字段含义与 kind 分支见 [url-kinds.md](url-kinds.md)。
  2. 保存 `$host` = response.host、`$url_kind`、`$simple_name`、`$work_item_id`。
  3. 执行 Auth Guard（下面的 STEP 1 起）。
  4. 登录成功后按 `$url_kind` 分支：
     - `workitem_detail` → `project search` 得权威 `$project_key`，再 `workitem get` 查询详情
     - `workitem_homepage` / `view_*` / `unknown` 等非详情页 → 按 url-kinds.md 的指引拒绝或追问
     - 其他 kind → 参考 url-kinds.md 对应处理方式

按以下 STEP 顺序执行。每个 STEP 结尾的 GOTO 指明下一步，严格遵循跳转。

---

### STEP 1 — 检查登录状态

```bash
meegle auth status --format json
```

返回值示例：
- 已登录：`{ "authenticated": true, "host": "meegle.com", "source": "token_store", "expires_in_minutes": 42 }`
- 未登录且有 host：`{ "authenticated": false, "host": "meegle.com", "source": null, "expires_in_minutes": null }`
- 未登录且无 host：`{ "authenticated": false, "host": null, "source": null, "expires_in_minutes": null }`

解析返回值，保存变量：
- `$authenticated` = response.authenticated
- `$host` = response.host

**URL 触发时的 host 覆盖**：如果用户发送了飞书项目/Meegle URL 触发本流程，且 `$host` 为 null，则使用上一步 `url decode` 返回的 `host` 字段作为 `$host`。

**跳转：**
- IF `$authenticated == true` → GOTO STEP DONE
- IF `$host != null` → GOTO STEP 2
- IF `$host == null` → GOTO STEP HOST

---

### STEP HOST — 选择站点

ASK user（等待用户回复）：

> 你要连接哪个站点？
> 1) 飞书项目 (project.feishu.cn)
> 2) Meegle (meegle.com)
> 3) 自定义域名（请直接输入域名）

SAVE `$host` from user reply → GOTO STEP 2

---

### STEP 2 — OAuth 登录

```bash
meegle auth login --host $host
```

命令会自动打开浏览器完成 OAuth 授权。等待命令执行完毕。

**跳转：**
- IF 命令成功（exit code 0） → GOTO STEP OK
- IF 命令失败 → SEND "OAuth 登录失败，请检查错误信息或在终端中手动执行 `meegle auth login`"，STOP

---

### STEP OK — 通知登录成功

SEND to user: "登录成功！"

> ⚠️ 此消息**必须单独发送**，不要与后续业务查询结果合并到同一条回复中。用户需要第一时间看到授权状态变化。

→ GOTO STEP DONE

---

### STEP DONE — 执行业务命令

Auth 已通过，执行用户请求的操作。

## 错误处理

- 如果 bash 返回 `command not found` 或 npx 不可用，提示用户安装 Node.js 18+。
- 如果 OAuth 登录失败，提示用户在终端中手动执行 `meegle auth login`。
