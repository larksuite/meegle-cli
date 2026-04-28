# URL Kinds —— `url decode` 返回值到 SOP 的映射

## 为什么 skill 不自己拆 URL

Meego/飞书项目的路由非常多，且 snake_case 旧路径、`/meego/` 前缀、`_xxx_resource` 资源工作项、预置功能区（user-gantt / chart / multi-project-view 等）这些都会让"看起来像工作项详情页"的 URL 其实不是。**禁止**自己从 URL 截取路径段作参数。

统一走一条命令：

```bash
meegle url decode --url '<URL>' --format json
```

拿到 `url_kind` 后按本文表格选择 SOP 或回绝。纯本地解析，无网络调用。

---

## 返回字段

| 字段 | 说明 |
|---|---|
| `url_kind` | 必返；未识别时为 `unknown` |
| `simple_name` | 空间标识；需要 `project_key` 时先 `project search` 用 `simple_name` 作为输入 |
| `work_item_type` | 工作项类型 api_name（已脱去 `_xxx_resource` 包装） |
| `work_item_id` | 工作项 ID（字符串） |
| `view_id` / `chart_id` / `plugin_key` / `team_id` / `template_id` | 按路径分别返回 |
| `setting_type` | 设置页子参数（如 permission 类型） |
| `edit_str` | `homepage/edit`、`overview/edit` 的编辑态标记 |
| `is_resource` | true 表示路径里带 `_xxx_resource` 包装 |
| `query` | 原始 query 参数，保留 `scope/node` 等二级导航上下文 |
| `redirected_from` | 若经别名归一化或 `/meego/` 前缀剥离，记录原始路径 |
| `pathname` / `host` / `raw` | 诊断用 |

---

## `url_kind` → 允许的 SOP

### 工作项类

| url_kind | 可用字段 | 推荐 SOP / 命令 |
|---|---|---|
| `workitem_detail` | simple_name · work_item_type · work_item_id | `sop-update-workitem` / `sop-transition-node` / `sop-transition-state` 任一，先 `project search` → `workitem get` |
| `workitem_create` | simple_name · work_item_type | `sop-create-workitem`（`workitem create`） |
| `workitem_draft` | simple_name · work_item_type | 同上，提示用户这是草稿视图 |
| `workitem_homepage` / `workitem_homepage_edit` | simple_name · work_item_type | 无具体工作项 ID — **拒绝**直接操作，要求用户提供详情页 URL 或工作项 ID |

### 视图类（有 `view_id` 但无 `work_item_id`）

| url_kind | 语义 | 处理 |
|---|---|---|
| `view_story` / `view_issue` | 需求 / 缺陷视图 | 如果用户想操作"这个视图里的工作项"，要求具体工作项 URL；否则可用 `view get` 查视图 |
| `view_multi_project` / `view_project_overview` / `view_user_gantt` | 跨空间/全域/甘特视图 | 同上 |
| `view_chart` | 图表视图 | 交叉到 `chart_*` 流程 |
| `view_workitem` | 通用工作项视图 | 若 `is_resource=true`，`work_item_type` 已脱包装可直接用 |

### 图表类

| url_kind | 可用字段 | 处理 |
|---|---|---|
| `chart_detail` | simple_name · chart_id | 图表详情；可用 `chart get` |
| `chart_create` | simple_name | 图表创建入口（无 ID） |
| `chart_homepage` / `chart_datascope*` / `chart_penetrate*` | simple_name · chart_id? | 抽屉/子页，通常**不**作为操作目标 |

### 空间/设置类（写操作请走 OpenAPI，非本 skill 范围）

| url_kind | 说明 |
|---|---|
| `project_home` · `project_overview` · `project_empty` · `project_ai_assist` | 空间级落地页，`simple_name` 可用于 `project search` |
| `project_overview_edit` | 编辑态，不作为操作目标 |
| `project_404` · `project_401` · `project_500` | 错误页，**拒绝** |
| `setting_*` | 各类设置页；本 skill 不做设置写操作，**拒绝**并告知 |
| `setting_other` | 未枚举的 setting 子页（前端通过非 exact 路由内部渲染），等同于 `setting_*` — **拒绝** |
| `import_jira` · `import_excel` · `data_recycle` | 导入/回收操作在界面内完成，**拒绝** |
| `plugin_page` | 插件页 — 行为由插件定义，CLI 无法操作，**拒绝** |

### 全局/导航类

| url_kind | 处理 |
|---|---|
| `workbench` · `workspaces` · `favorites` · `inbox` | 顶级导航页，**没有具体目标**，请追问 |
| `teams` · `team_detail` | 团队页；`team_detail` 的 `team_id` 可用于 `team list-members` |
| `templates` · `template_detail` · `template_manage` | 模板中心，本 skill 不做模板操作，**拒绝** |
| `project_list` | 全部空间列表，追问具体空间 |

### 系统域 `/b/*`

| url_kind | 处理 |
|---|---|
| `preference` · `mcp_config` · `mcp_auth` · `ai_hub` · `handover` · `onboarding_*` · `trial_*` · `cross_*` · `slack_connect` · `resource_handover` · `no_project_auth` · `login_datacenter` · `unbundled_register_result` · `b_home` | 系统/管理页，本 skill **拒绝**业务操作 |

### 登录/外部入口

| url_kind | 处理 |
|---|---|
| `login_fetch_cookie` · `login_asset` · `switch_asset` · `home_ka` · `tenant_select` · `tenant_create` · `channel_error` | 登录相关 — 改走 `auth-guard` |
| `quick_create_form` · `issue_trans` · `issue_create_open_usecase` · `story_create_open` · `jump_to_outer` · `light_share` · `ai_application_share` | 飞书内嵌入口，本 skill 通常不作为操作起点 |

### 错误兜底

| url_kind | 处理 |
|---|---|
| `lark_page_404` · `project_empty_page` · `route_loading` · `system_upgrade` | 错误页，**拒绝** |
| `unknown` | **拒绝**并要求用户提供详情页 URL 或直接描述任务 |

### 特殊字段校验

- `redirected_from` 非空 → 在回复中提一句"检测到旧版路径，已自动归一化"，避免用户误以为 URL 错了
- `is_resource=true` → 告知用户这是资源工作项视图，`work_item_type` 已自动脱去 `_xxx_resource` 包装
- `query.scope` / `query.node` 非空 → 仅作为导航上下文，不要当作业务参数

---

## 典型分支模板（供 SOP 引用）

```
STEP 0 — URL 解析（仅当用户提供了 URL）

  url decode --url "<URL>"
  SAVE $url_kind, $simple_name, $work_item_type, $work_item_id, $view_id, $redirected_from

  SWITCH $url_kind:
    - workitem_detail    → GOTO 本 SOP 的 STEP 1（已具备 simple_name + work_item_id）
    - workitem_homepage  → ASK user："需要具体工作项 URL，这是类型主页。"；STOP
    - view_*             → ASK user："这是视图 URL，请粘贴具体工作项详情页 URL。"；STOP
    - unknown            → ASK user："无法识别该 URL，请确认后重发或直接描述任务。"；STOP
    - 其他非本 SOP 范围   → 告知 kind，建议对应操作；STOP
```
