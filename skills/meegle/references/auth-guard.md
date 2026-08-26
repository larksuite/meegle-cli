# Auth Guard（所有业务命令前必须执行）

## 触发条件

- **主动登录**：用户说"登录 Meegle"、"连接飞书项目"、"login meegle"等。
- **被动拦截**：用户请求任何 Meegle 业务操作（查询待办、查工作项、创建任务等），优先执行 Auth Guard。
- **URL 触发**：用户发送了飞书项目/Meegle URL。处理流程：
  1. 先调 `url decode` 拿到结构化字段（`url_kind`、`host`、`simple_name`、`work_item_id` 等）。**禁止**自己从 URL 截取路径段作参数。字段含义与 kind 分支见 [url-kinds.md](url-kinds.md)。
  2. 保存 `$url_host` = response.host、`$target_host = $url_host`、`$url_kind`、`$simple_name`、`$work_item_id`。`$url_host` 只表示原链接所属站点，`$target_host` 表示本轮业务必须连接的站点，两者都不得被后续登录状态覆盖。
  3. 执行 Auth Guard（下面的 STEP 1 起）。
  4. 登录成功后按 `$url_kind` 分支：
     - `workitem_detail` → `project search` 得权威 `$project_key`，再 `workitem get` 查询详情
     - `workitem_homepage` / `view_*` / `unknown` 等非详情页 → 按 url-kinds.md 的指引拒绝或追问
     - 其他 kind → 参考 url-kinds.md 对应处理方式

按以下 STEP 顺序执行。每个 STEP 结尾的 GOTO 指明下一步，严格遵循跳转。

进入 STEP 1 前初始化变量：

- URL 触发 → 保留上面已解析的 `$url_host` 与 `$target_host`。
- 非 URL 触发 → SAVE `$url_host = null`；调用流程明确要求访问特定站点时，必须为本次请求传入 `$target_host`（例如链接生成流程中的显式域名），否则 SAVE `$target_host = null`。禁止继承上一请求的值。
- 将 `$profile_args` 保存为参数数组：用户显式选择 profile → `profile_args=(--profile "$profile")`；未显式选择 → `profile_args=()`。下面所有 Auth Guard 命令和 STEP DONE 的业务命令都必须复用同一个数组，禁止中途切换。

---

### STEP 1 — 检查登录状态

```bash
meegle "${profile_args[@]}" auth status --format json
```

返回值示例：
- 已登录：`{ "authenticated": true, "host": "meegle.com", "source": "token_store", "expires_in_minutes": 42 }`
- 未登录且有 host：`{ "authenticated": false, "host": "meegle.com", "source": null, "expires_in_minutes": null }`
- 未登录且无 host：`{ "authenticated": false, "host": null, "source": null, "expires_in_minutes": null }`

解析返回值，保存变量：
- `$authenticated` = response.authenticated
- `$auth_host` = response.host

**目标 host 一致性检查**：`$target_host` 非空时执行。比较前将 `$target_host` 与 `$auth_host` 的域名转为小写并去掉末尾的 `.`，端口必须保持一致。

- IF `$target_host != null` AND `$auth_host != null` AND 两者不一致 → SEND "当前登录站点为 `$auth_host`，但目标属于 `$target_host`。请切换或指定目标站点对应的 profile 后重试；本次未执行后续查询。"；STOP
- IF `$auth_host != null` → SAVE `$host = $auth_host`
- IF `$auth_host == null` AND `$target_host != null` → SAVE `$host = $target_host`
- 其他情况 → SAVE `$host = null`

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
meegle "${profile_args[@]}" auth login --host "$host"
```

命令会自动打开浏览器完成 OAuth 授权。等待命令执行完毕。

**跳转：**
- IF 命令成功（exit code 0） → GOTO STEP OK
- IF 命令失败 → SEND "OAuth 登录失败，请检查错误信息或在终端中手动重新执行上方登录命令"，STOP

---

### STEP OK — 通知登录成功

SEND to user: "登录成功！"

> ⚠️ 此消息**必须单独发送**，不要与后续业务查询结果合并到同一条回复中。用户需要第一时间看到授权状态变化。

→ GOTO STEP DONE

---

### STEP DONE — 执行业务命令

Auth 已通过，执行用户请求的操作。每条业务命令继续使用进入 STEP 1 前保存的同一个 `$profile_args`。

## 错误处理

- 如果 bash 返回 `command not found` 或 npx 不可用，提示用户安装 Node.js 18+。
- 如果 OAuth 登录失败，提示用户在终端中手动重新执行上方登录命令。
