# Meegle CLI 扩展 V1 使用示例

V1 是“源码依赖 + 编译期装配”：企业程序引用本仓库，在自己的 `main` 包里用空白导入注册扩展，然后编译出企业版 `meegle`。不需要修改 Meegle 源码，也不需要新增服务端接口；V1 不支持运行时下载插件。

## 1. 完全不扩展

最小程序见 [`no-extension/main.go`](./no-extension/main.go)：

```go
package main

import (
	"os"

	meeglecmd "github.com/larksuite/meegle-cli/cmd"
)

func main() {
	os.Exit(meeglecmd.Execute())
}
```

它与官方 CLI 使用方式一致：

```bash
go build -o meegle ./examples/no-extension
./meegle --help
./meegle auth login --device-code
./meegle workitem get --project-key demo --work-item-id 123
```

没有注册扩展时，凭证、网络和命令执行都走现有内置逻辑。

## 2. 企业版 CLI 的装配方式

完整程序见 [`enterprise-cli/main.go`](./enterprise-cli/main.go)：

```go
package main

import (
	"os"

	meeglecmd "github.com/larksuite/meegle-cli/cmd"
	_ "github.com/larksuite/meegle-cli/examples/enterprise-cli/credential"
	_ "github.com/larksuite/meegle-cli/examples/enterprise-cli/governance"
	_ "github.com/larksuite/meegle-cli/examples/enterprise-cli/transport"
)

var version = "1.2.3"

func main() {
	os.Exit(meeglecmd.ExecuteWithVersion(version))
}
```

`version` 是企业发行版自己的语义化版本。如果企业插件使用 `RequireCLI`，必须调用 `ExecuteWithVersion`，让兼容性判断使用企业二进制版本；不声明版本约束时仍可使用 `Execute()`。本地 `go run` 或未注入版本的 `dev` 构建不会绕过约束，而会在错误链中提示使用 `ExecuteWithVersion`。

企业只需要维护装配层和自己的扩展包：

```bash
go build -o corp-meegle ./examples/enterprise-cli

CORP_MEEGLE_HOST=project.example.com \
CORP_MEEGLE_TOKEN=example-token \
CORP_DEVICE_TRUST=trusted \
./corp-meegle workitem get --project-key demo --work-item-id 123
```

同一个二进制中的扩展会同时作用于静态命令和 MCP 动态命令。MCP 更新 `tools/list` 后，携带 `metadata.resource` 和 `metadata.method` 的新工具会注册成对应命令，并自动经过同一组治理钩子；已有工具也可以继续使用 CLI 内置的 fallback 映射。

## 3. Credential：企业提供账号和 token

实现 `credential.Provider`，通常在扩展包的 `init` 中注册：

```go
type provider struct{}

func (provider) Name() string  { return "corp-sso" }
func (provider) Priority() int { return 1 } // 可选；数字越小越先执行，默认 10

func (provider) ResolveAccount(ctx context.Context) (*credential.Account, error) {
	return &credential.Account{
		Host:        "project.example.com",
		ProfileName: "corp",
	}, nil
}

func (provider) ResolveToken(ctx context.Context, spec credential.TokenSpec) (*credential.Token, error) {
	return &credential.Token{
		Value:  obtainTokenFromEnterpriseSSO(ctx, spec),
		Header: "Authorization",
		Source: "corp-sso",
	}, nil
}

func init() { credential.Register(provider{}) }
```

Provider 有四种结果：

- `value, nil`：命中，使用企业返回的账号或 token。
- `nil, nil`：跳过，继续下一个 Provider；全部跳过后回退到内置凭证。
- `nil, error`：运行失败，立即停止解析且不回退，例如企业 SSO 暂时不可用。
- `nil, &credential.BlockError{...}`：Provider 被调用时立即停止身份解析且不回退，明确表示企业策略主动拒绝，例如设备不可信、租户被禁用。

Credential 是按需身份解析器，不是全局启动 Hook。`--help`、`version`、`config`、`completion`、`url`、`extension` 等本地入口在构造阶段不会调用 Provider；动态业务命令仍会调用并 fail-closed。若企业必须让设备信任策略阻断包括帮助在内的所有命令，应在 Platform Lifecycle/Policy 中显式实现。

`Name()` 只在注册时读取一次，必须返回由小写字母、数字和连字符组成的非敏感标识；非法名称以及 Name/Priority 回调的 panic 或超时会让 CLI 启动 fail-closed。Account/Token 回调最多等待 30 秒，并且应主动监听传入的 `ctx`。`Token.Source` 只接受短标签，非法值在诊断中固定显示为 `<invalid>`；自定义 `Token.Header` 必须是合法 HTTP Header 名，否则在发送请求前拒绝。

自定义鉴权头也受支持：

```go
return &credential.Token{
	Value:  token,
	Header: "X-Enterprise-Token", // 不再写 Authorization: Bearer ...
	Source: "corp-sso",
}, nil
```

一次 CLI 执行只解析一次身份；MCP 的 `tools/list` 和后续 `tools/call` 使用同一份会话 token，避免两次临时 token 不一致。

启用自定义认证 Header 后，CLI 会删除 profile headers 中残留的 `Authorization` 和同名旧认证值，只发送 Provider 本次返回的 Token。

## 4. Transport：统一接管 CLI 发出的 HTTP 请求

Transport 可加请求头、选择代理或专线、记录耗时，也可以在发出请求前阻断：

```go
type provider struct{}
type interceptor struct{}

func (provider) Name() string { return "corp-network" }
func (provider) ResolveInterceptor(context.Context) transport.Interceptor {
	return interceptor{}
}

// 普通前置/后置钩子。
func (interceptor) PreRoundTrip(req *http.Request) func(*http.Response, error) {
	started := time.Now()
	req.Header.Set("X-Corp-Caller", "meegle-cli")
	host := req.URL.Host // 复制需要的值，不在后置 Hook 中继续持有 req。
	return func(resp *http.Response, err error) {
		// resp 是元数据快照：可读取状态码/Header，但 Body 固定为
		// http.NoBody，不能在这里读取业务响应内容。
		recordLatency(host, time.Since(started), resp, err)
	}
}

func init() { transport.Register(provider{}) }
```

需要阻断时，再实现 `AbortableInterceptor`：

```go
func (interceptor) PreRoundTripE(req *http.Request) (func(*http.Response, error), error) {
	if !strings.HasSuffix(req.URL.Hostname(), ".company.example") {
		return nil, errors.New("destination is outside the corporate network")
	}
	return interceptor{}.PreRoundTrip(req), nil
}
```

V1 的 Transport 覆盖：

- MCP 动态发现 `tools/list`。
- MCP 命令调用 `tools/call`，包括 401 后的重试。
- OAuth discovery、client 注册、device-code、token exchange 和 token refresh。
- 附件上传、下载的对象存储 HTTP 请求。

只允许一个有效 Transport Provider；后注册者覆盖先注册者。实现使用 CLI 专属的 `http.Client`，不会替换进程级 `http.DefaultClient`。Provider、前置 Hook 和后置 Hook 各自最多运行 30 秒；真实 MCP、OAuth 和附件请求继续使用调用方 Context 与原 HTTP Client timeout，所以启用扩展不会截断大文件上传下载或慢服务端操作。内置安全基线仍保留 10 次重定向上限，并在调用已有 `CheckRedirect` 前冻结原始 scheme；回调即使修改目标或重定向历史，也不能把 HTTPS 降级为 HTTP。MCP 的 Bearer Token 和自定义 Token Header 还会独立执行精确 origin 重定向检查，即使没有注册 Transport Provider 也不会被带到降级、换端口或其他域名的目标。前置回调拿到注入凭证后的真实 Request，因此进程内代码在技术上可以读取、修改或删除认证 Header；CLI 不提供进程内安全隔离，也不会冻结 Header 值，只应注册经过审查的可信 Transport。认证值仍应由 Credential 提供，Transport 通常只增加网络、路由和审计信息。读取 `req.Body` 会消费请求内容，正常返回前必须恢复；自定义 Body 必须遵守 `net/http` 的并发 `Read/Close` 契约，Context 结束或回调返回后不得继续访问 Request。后置回调只拿到状态码、Header、Trailer、Request 等元数据副本，`Body` 固定为 `http.NoBody`，`resp.Request.Context()` 携带独立的后置 Hook 截止时间，因此不会消费或占住业务响应流。后置回调超时后，CLI 会返回稳定错误并立即释放真实响应体。Go 不能强制终止忽略截止信号的第三方函数，因此企业回调仍应保证在合理时间内返回。

## 5. Platform：观察、包装和限制命令

### 5.1 前置和后置观察

Observer 只观察，不改变命令结果：

```go
builder.Observer(platform.Before, "audit-start", platform.All(),
	func(ctx context.Context, in platform.Invocation) {
		log.Printf("start %s", in.Cmd().Path())
	})

builder.Observer(platform.After, "audit-result", platform.All(),
	func(ctx context.Context, in platform.Invocation) {
		log.Printf("finish %s err=%v denied=%t",
			in.Cmd().Path(), in.Err(), in.DeniedByPolicy())
	})
```

### 5.2 包装命令

Wrapper 可以在命令前后执行逻辑，也可以返回错误中止执行：

```go
builder.Wrap("change-ticket", platform.ByWrite(), func(next platform.Handler) platform.Handler {
	return func(ctx context.Context, in platform.Invocation) error {
		if os.Getenv("CHANGE_TICKET") == "" {
			return &platform.AbortError{
				HookName: "change-ticket",
				Reason:   "write command requires CHANGE_TICKET",
			}
		}
		return next(ctx, in)
	}
})
```

每次 Wrapper 调用最多执行一次 `next`，并且必须在 Wrapper 返回前发起；运行时会等待已经进入的 `next` 完成，防止业务 Handler 在命令结束后继续访问 Cobra、IO 或连接资源。`next` 返回非 nil 错误时，即使 Wrapper 忽略该错误并返回 nil，最终命令仍然失败，避免退出码和审计结果被误报为成功。因此 Wrapper 和下游 Handler 都应监听传入的 `ctx`，并直接返回 `next` 的错误。

### 5.3 选择命令

选择器适用于静态命令和动态命令：

```go
platform.All()
platform.None()
platform.ByDomain("workitem", "view")
platform.ByCommandPath("workitem/get", "view/**", "workitem/**/get")
platform.ByReadOnly()
platform.ByWrite()
platform.ByExactRisk(platform.RiskHighRiskWrite)
platform.ByIdentity(platform.IdentityUser)

// 可组合
platform.ByDomain("workitem").And(platform.ByWrite())
platform.ByReadOnly().Or(platform.ByCommandPath("version"))
platform.ByCommandPath("auth/**").Not()
```

`*` 匹配一个路径段中的字符，`**` 匹配零个或多个完整路径段。

### 5.4 生命周期

```go
builder.
	On(platform.Startup, "startup", func(ctx context.Context, event *platform.LifecycleContext) error {
		return openAuditSink(ctx)
	}).
	On(platform.Shutdown, "shutdown", func(ctx context.Context, event *platform.LifecycleContext) error {
		return closeAuditSink(ctx, event.Err)
	})
```

每次进程执行各触发一次 startup 和 shutdown。插件元数据/Install 和每个 startup Hook 各有 2 秒安全边界：fail-open 超时会跳过且不影响后续插件，fail-closed 超时会终止 CLI，Install 超时后的迟到注册不会生效。shutdown 最长等待 2 秒；某个 fail-closed shutdown Hook 失败时，其余 Hook 仍会在这一个共享预算内继续清理，最终返回第一个失败。

### 5.5 Restrict 权限规则

只读 Agent 示例：

```go
builder.Restrict(&platform.Rule{
	Name:       "agent-readonly",
	Allow:      []string{"workitem/**", "view/**", "extension/**"},
	Deny:       []string{"workitem/delete"},
	MaxRisk:    platform.RiskRead,
	Identities: []platform.Identity{platform.IdentityUser},
})
```

`extension/**` 用于在业务 Allow 列表生效后保留六个脱敏诊断命令。Restrict 同样治理诊断命令；只有明确希望隐藏诊断信息时才应移除该路径。

执行和帮助展示使用同一条策略：被拒绝的命令不会出现在帮助中；即使用户直接输入隐藏命令，执行前仍会再次拒绝。

风险等级从低到高为 `read`、`write`、`high-risk-write`。会发布线上 WBS 或丢弃未发布 WBS 草稿的命令属于 `high-risk-write`；未知 MCP 工具没有风险标记，启用 `MaxRisk` 时默认拒绝，只有显式设置 `AllowUnannotated: true` 才放行。

同一进程只允许一个插件拥有 Restrict，避免多个插件之间出现不清晰的策略归属。该插件可以注册多条 Rule，但命令必须同时满足每一条 Rule；任意 Deny 命中都会全局拒绝，宽泛 Rule 不能绕过更窄 Rule 的路径、风险或身份限制。该插件必须 fail-closed。

显式使用 `--format json` 或 `--format ndjson` 时，拒绝结果使用统一错误 envelope，错误码为 `CLIENT_COMMAND_DENIED`，`detail` 中包含 rule、reason code 和非敏感 policy source，便于 Agent 稳定处理。CLI App 构造前发生的 Credential 与 Platform 启动失败也使用同一错误 envelope，分别暴露 `CLIENT_CREDENTIAL_RESOLUTION_FAILED` 和 `CLIENT_EXTENSION_INSTALL_FAILED`。扩展运行失败和主动中止分别使用 `CLIENT_EXTENSION_RUNTIME_FAILED`、`CLIENT_EXTENSION_ABORTED`，原始回调错误不会被写入公开输出。

### 5.6 完整注册

```go
func init() {
	plugin := platform.NewPlugin("corp-governance", "1.0.0").
		RequireCLI(">=1.2.0 <2.0.0").
		FailClosed().
		Observer(platform.After, "audit", platform.All(), observe).
		Wrap("change-ticket", platform.ByWrite(), wrap).
		On(platform.Startup, "startup", onStartup).
		Restrict(readOnlyRule()).
		MustBuild()

	platform.Register(plugin)
}
```

`RequireCLI` 支持精确版本和空格/逗号分隔的比较条件，例如 `1.2.3`、`>=1.2.0 <2.0.0`、`>= 1.2.0, < 2.0.0`。普通审计插件可以 `FailOpen`：安装或版本检查失败时跳过；Restrict 插件强制 `FailClosed`：失败时 CLI 不启动。使用 Builder 时，调用 `Restrict(...)` 会自动切换为 fail-closed；手写 `Plugin` 如果同时声明 `Restricts: true` 和 `FailurePolicy: FailOpen`，CLI 会在调用 `Install` 前直接启动失败，不会静默跳过策略。

## 6. 静态命令、动态命令和刷新

- `auth`、`config`、`inspect`、`completion`、`url`、`version`、`extension` 是静态命令。
- 业务命令来自 MCP `tools/list`，例如 `workitem get`、`view list`，属于动态命令。新工具应提供 `metadata.resource` 和 `metadata.method`；没有 metadata 的已有工具可以使用内置 fallback 映射。
- 静态根命令和已有 fallback 映射优先级更高，MCP metadata 不能把远端工具注册成 `auth status` 等本地路径，也不能改写已有工具的稳定命令路径。
- 单个动态工具的 wire 结构、命名、重复路径、Flag 定义或资源规模不合法时，只跳过该工具；其他动态工具、静态命令和 SDK 仍可使用。帮助文本会移除控制字符，缺少 description 会生成固定安全文本。
- `meegle --refresh ...` 或缓存重建后，Cobra 命令树会重建；企业静态命令和 Platform 钩子会自动重新挂载。
- SDK 也会动态发现 MCP tool 并建立内部工具注册表，但不会生成命令行命令，也不会读取上述 CLI 全局扩展注册表。

## 7. 诊断命令

诊断只输出非敏感元数据，不打印 token：

```bash
./corp-meegle extension doctor
./corp-meegle extension discovery
./corp-meegle extension credentials
./corp-meegle extension transport
./corp-meegle extension plugins
./corp-meegle extension policy
```

公开 SDK 通过 `meegle.NewClient(...)` 创建 `Client`，随后可调用 `client.DiscoveryIssues()` 获取被隔离工具的稳定 `Code`、`ToolName` 和 `Path`；诊断不包含 Token、Header 或服务端返回的自由文本。

典型输出：

```text
credential: corp-sso priority=1
credential-active: not-evaluated token-source=unknown
transport: corp-network status=active hook-timeout=30s tls-downgrade=blocked redirects=10
plugin: corp-governance version=1.0.0 status=active policy=fail-open restricts=false
```

## 8. V1 明确不做的事情

- 不在运行时下载、安装或热更新 Go 插件。
- 不新增 Meegle 服务端接口。
- 不包含插件 token/插件操作身份。
- 不包含 E2E 上报能力。
- 不让 SDK 自动加载企业 CLI 扩展。

以后增加插件操作身份时，可以在 Credential 的身份模型中扩展，不需要推翻本 V1 的 CLI 装配、Transport 和 Platform 结构。
