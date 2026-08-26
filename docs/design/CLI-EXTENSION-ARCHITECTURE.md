# Meegle CLI 企业扩展架构

## 目标与边界

企业发行版直接依赖官方 Go Module，通过编译期导入注册 Credential、Transport 和 Platform 扩展，再调用公开 `cmd` 入口构建自己的二进制。企业不需要 Fork 官方仓库，也不需要新增 Meegle 后端接口。

V1 不支持运行时下载、安装或热更新插件；扩展与 CLI 在同一进程内运行，因此只能链接可信企业代码。插件 Token、插件操作身份和 SDK 扩展不属于 V1。

## 模块关系

```text
企业 main
  ├─ blank import 企业 adapter
  └─ cmd.ExecuteWithVersion(version)
       │
       ├─ extension/credential  账号与用户 Token
       ├─ extension/transport   HTTP 请求前后 Hook
       └─ extension/platform    Restrict / Observer / Wrap / Lifecycle
               │
               ▼
       internal/extension runtime
               │
               ▼
       internal/products/meegle
         ├─ MCP tools/list → 动态命令树
         ├─ 静态命令
         ├─ OAuth / refresh
         └─ Attachment HTTP
```

公开包只描述企业需要实现的接口；优先级、快照、失败转换、原子安装、Cobra 树重建和安全基线保留在内部运行时。

## 单次执行顺序

1. 企业包通过 `init()` 完成编译期注册。
2. Transport Factory 读取当前 Provider，构建 CLI 专属 HTTP Client。
3. Platform 插件在暂存区安装；校验失败时不提交部分 Hook 或 Rule。
4. CLI 冻结 Credential 注册快照，并从同一快照解析 Account 和 Token。
5. MCP `tools/list` 使用已解析身份和统一 HTTP Client 发现动态工具，并逐个校验、隔离非法或冲突条目。
6. Router 构建最终 Cobra 树，加入静态命令，再应用 Restrict、Observer 和 Wrap；顶层 `--version` 会归一到同一棵树中的 `version` 命令，不走 Cobra 的治理旁路。
7. Platform 每个插件的元数据/Install 和每个 Startup Hook 都有独立的两秒边界；随后目标命令和 Shutdown 依次执行。Shutdown 逆序且总等待不超过两秒。即使目标命令的 Context 已取消，Shutdown 仍保留 Context value，并获得独立的两秒清理窗口。

Registry Rebuild 后会重新对新命令树应用静态命令和 Platform Runtime。挂载具有幂等保护，同一命令不会重复执行 Hook。

## 三类扩展契约

### Credential

多个 Provider 按注册时冻结的优先级排序；名称也只在注册时读取一次，必须是小写字母、数字和连字符组成的非敏感标识。非法名称以及 Name/Priority 回调的 panic 或超时会形成稳定、不可注入的诊断。`value, nil` 表示命中，`nil, nil` 表示跳过；Provider 被调用时，任何非 nil 错误都会停止解析且不回退，`BlockError` 只是把“企业策略主动拒绝”与普通运行失败区分开。Account/Token 回调受调用 Context 和 30 秒上限约束；Provider 错误会附带冻结后的安全名称和 Account/Token 阶段，同时保留原始错误链。Token Source 作为诊断标签会做字符和长度校验，非法值固定显示为 `<invalid>`；自定义 Token Header 必须符合 HTTP 字段名语法，否则在发请求前 fail-closed。一次身份解析只读取一次 Provider 列表，因此解析期间发生的新注册要到下一次执行才生效。需要身份的 CLI 调用在 `tools/list`、`tools/call` 和静态 Handler 之间共享同一身份快照；SDK 继续使用原有身份链。

Credential 只在命令需要身份时解析，不再充当全局启动 Hook。CLI 会保守识别已知本地根命令：`--help`、`version`、`config`、`completion`、`url`、`extension` 等在构造阶段不调用 Provider；`auth` 和 `inspect` 也延迟到具体 Handler 决定是否解析。动态或无法确定的业务根命令仍在构造阶段调用 Provider，以便完成 MCP 命令发现，普通错误、超时和 `BlockError` 全部 fail-closed。需要全局阻断所有命令的企业准入应使用 Platform Lifecycle/Policy 显式实现。内置 profile 的 `${VAR}` 展开失败同样延迟到真正需要身份的业务命令：自救入口保持可用，业务命令返回 `CONFIG_ENV_UNRESOLVED`，不会使用空身份继续请求。

### Transport

只启用一个 Provider，后注册覆盖先注册。Interceptor 覆盖 MCP、OAuth、Token 刷新和附件请求，但不能替换 CLI Client 本身。Provider、前置 Hook 和后置 Hook 各自有 30 秒安全上限；该上限只保护扩展回调，不会写入底层业务 Request，也不会改写原 HTTP Client timeout。真实 MCP、OAuth 和附件传输继续使用调用方 Context，因此启用扩展不会新增 30 秒的大文件或慢操作上限。扩展仍固定保留 10 次重定向上限和 HTTPS 防降级；CLI 在调用已有 `CheckRedirect` 前冻结原始 scheme，回调执行后使用不可变快照检查最终 URL，回调即使同时修改目标和重定向历史也不能重新引入 HTTP。前置回调操作注入凭证后的真实 Request，因此可信的进程内扩展在技术上可以读取、修改或删除认证 Header；CLI 的内置安全基线不提供进程内隔离，也不冻结 Header 值。前置回调可以修改 Header、Path 和 Query，但不能修改 `URL.Host` 或 `Request.Host`；CLI 会冻结两者，端口变化也算目标变化，并在调用底层 Transport 前中止请求、关闭请求体且把错误传给后置回调。测试或企业网络需要把固定域名连接到其他地址时，应在 Dialer 层改变实际连接地址，不能改写公开 Request 的 authority。Credential 仍是认证值来源，Transport 只应由经过审查的可信代码实现。读取 `Body` 会消费请求内容，正常返回前必须恢复，自定义 Body 必须遵守 `net/http` 要求的并发 `Read/Close` 契约，回调返回或 Context 结束后不得继续访问 Request；若 Hook 替换 Body，CLI 会在下发前关闭原 Body，底层 Transport 负责关闭替换后的 Body。前置拒绝、超时、非法 URL 或 Hook panic 不会调用底层 Transport，并会关闭请求体；对合规 Body，Close 会安全解除正在等待的 Read。后置回调只接收状态码、Header、Trailer、TLS 状态和 Request 等隔离元数据副本，证书对象也会重新解析为独立副本，`Body` 固定为 `http.NoBody`，其 `Request.Context()` 携带独立的后置 Hook 截止时间；超过截止时间时 CLI 返回稳定错误并立即关闭真实响应体。回调返回的原因和 panic 值只保留在 Go error chain，公开输出只使用稳定错误码和非敏感扩展名。由于 Go 不能强制终止不返回的第三方函数，超时会让 CLI 停止等待并释放网络资源，但忽略截止信号的回调 goroutine 仍须由企业实现自行保证最终退出。

Credential 使用自定义 Header 传递 Token 时，MCP Client 会清除 profile 中残留的 `Authorization` 和旧自定义认证值，只写入当前 Token。无论使用默认 `Authorization: Bearer` 还是自定义 Header，只要请求携带凭证，MCP Client 都会在首个请求发出前冻结初始 origin，保留 10 跳重定向上限，后续每一跳都拒绝降级、换端口和跨域；已有 `CheckRedirect` 即使篡改两跳之间的历史请求也不能改变比较基准或取消跳数上限。该保护位于认证请求层，即使只启用 Credential、不启用 Transport 也会生效。

### Platform

Observer 用于审计，Wrap 用于执行包装，Restrict 是最终本地守卫，Lifecycle 管理启动和关闭资源。被 Restrict 拒绝的命令仍通知 Observer.After，但不执行 Wrap 和业务 Handler。每次 Wrapper 调用最多执行一次 `next`，并须在 Wrapper 返回前发起；Wrapper 返回 nil 表示成功时必须已经调用 `next`，否则运行时返回 `CLIENT_EXTENSION_RUNTIME_FAILED`，不能静默跳过业务命令。`next` 返回非 nil 时，即使 Wrapper 忽略该结果并返回 nil，运行时仍保留原下游错误，最终退出码和 Observer.After 不会把失败误报成成功。运行时等待已经进入的 `next` 完成，避免业务 Handler 在命令返回后继续访问 Cobra、IO 或连接资源，因此 Wrapper 和下游 Handler 都必须遵守传入 Context 的取消信号。Restrict 插件必须 fail-closed，且同一进程只有一个策略所有者；Builder 在调用 `Restrict` 时自动切换为 fail-closed，手写 Plugin 若声明 `Restricts=true` 与 `FailurePolicy=FailOpen`，CLI 会在调用 `Install` 前启动失败，绝不静默跳过全部策略。该插件注册的多条 Rule 按共同约束合并，命令必须满足全部 Rule，任意 Deny 则全局拒绝。普通审计插件可以 fail-open。显式使用 `--format json|ndjson|table` 时，Policy 拒绝通过统一错误 formatter 输出稳定的 `CLIENT_COMMAND_DENIED` 结构；运行期回调 panic、nil 或 Lifecycle 失败输出 `CLIENT_EXTENSION_RUNTIME_FAILED`，主动 `AbortError` 输出 `CLIENT_EXTENSION_ABORTED`。回调原始 cause 只保留在受保护的 Go error chain，不进入公开消息；Credential、Transport、Platform、统一 formatter 和进程入口均使用 panic-safe 的错误遍历，即使扩展 error 自定义的 `Is`、`As`、`Unwrap`、`Error` 或 `ErrorPayload` 再次 panic，也只会得到稳定的脱敏错误，不会穿透到 Cobra 或进程入口。

每个 Lifecycle Hook 都收到独立的事件对象。插件元数据/Install 以每个插件两秒为界，每个 Startup Hook 也有独立两秒窗口；fail-open 超时不阻断后续插件，fail-closed 超时终止 CLI，超时后返回的 Install 不能再写入已冻结的 staging registrar。Shutdown 按注册逆序执行；成功控制流（包括首次初始化完成）的 `LifecycleContext.Err` 为 nil，真正的命令失败才会作为 Err 传入。某个 fail-closed Hook 失败时仍会在共享的两秒预算内继续尝试其余清理，最终返回第一个失败。Shutdown Hook 应监听 Context 取消并及时释放资源；运行时不会在清理总预算耗尽后继续启动 Hook，但 Go 无法强制终止忽略 Context 的扩展 goroutine。Platform 安装失败和 Credential 解析失败会分别映射为稳定的 `CLIENT_EXTENSION_INSTALL_FAILED`、`CLIENT_CREDENTIAL_RESOLUTION_FAILED`，同时保留原始 Go error chain；即使失败发生在 CLI App 构造完成前，显式 JSON/NDJSON 模式仍通过统一错误信封暴露稳定 code。

### 动态 MCP 工具

`tools/list` 返回的工具如果携带 `metadata.resource` 和 `metadata.method`，CLI 和 SDK 会把它映射到对应的动态命令；已有工具仍使用优先级更高、不可被服务端 metadata 改写的内置 fallback 映射。没有 metadata 且不在 fallback 表中的未知工具不会暴露为命令，并产生稳定的 `missing_mapping` 跳过诊断。

服务端工具定义属于不可信输入。`auth`、`config`、`inspect`、`completion`、`url`、`version`、`extension` 等静态根命令是保留路径，动态工具不能覆盖；wire 字段类型错误、metadata 不完整、非法命令名、重复路径、内置/全局/隐式 Flag 冲突等问题按单个工具隔离，不会让整个 CLI/SDK Registry 启动失败。JSON Schema 的 nullable union（例如 `type=[string,null]`）会归一为唯一的非 null 参数类型，数组 items 同样支持；包含多个非 null 类型的 union 暂不映射为一个 CLI Flag，该工具输出稳定的 `unsupported_schema_union` 诊断。所有 JSON-RPC 响应都在解码前限制大小：`tools/list` 上限为 8 MiB，其他调用的单响应上限为 32 MiB；单工具定义、工具数、参数数和文本长度也有独立上限，超限单工具不会影响合法兄弟。帮助文本会移除 ANSI 和控制字符，缺少 description 时使用固定安全描述。

CLI 可通过 `meegle extension discovery` 或 `meegle extension doctor` 查看只包含 quoted tool/path 和稳定 code 的跳过记录；公开 SDK Client 可通过 `Client.DiscoveryIssues()` 读取同一组稳定 code/tool/path 诊断。Registry Rebuild 在锁外完成发现和命令树构造，再在短临界区原子发布 commands、issues、auth 状态的深拷贝一致快照，因此慢 MCP 请求不会阻塞现有命令读取；合法新工具会重新进入同一组 Platform 治理链。

## 兼容性与发布门禁

- 无扩展企业入口必须与官方二进制保持相同的既有帮助、版本、补全、输出和退出码语义；新增的 `extension` 诊断命令是明确公开的增量。当前官方入口与无扩展入口做同版本对照，同时使用固定 `main@6326b7d` 契约验证内部改造没有让两边一起偏离旧行为。
- `cmd.Execute()` 使用官方入口的链接版本；声明 `RequireCLI` 的企业发行版使用 `cmd.ExecuteWithVersion(version)`。`dev` 无法满足非空约束，错误链会明确提示该入口，不会放宽 fail-closed 校验。
- README、中文 README、CHANGELOG、示例和本架构文档必须与公开 API 同步；本架构文档随开源同步产物一起发布，并由测试校验 README 链接目标存在。
- CI 和发布前检查执行全量 Race、扩展关键包 `-shuffle=on -count=3`、静态检查、License 检查，并在生成的开源目录内独立执行 `go build ./...` 和 `go test ./...`。

### 需求—测试证据矩阵

| 用户可见承诺 | 公共测试缝 | 失败场景 | 重复证据 | 状态 |
|---|---|---|---|---|
| 启用 Transport 后，分段返回的 `tools/list`、`tools/call` 仍能完整读取 | 注册公开 Transport Provider，调用真实 MCP Client | `RoundTrip` 返回后 Context 提前取消 | `TestExtensionTransport_PreservesStreamingMCP*` | PASS |
| Bearer 与自定义 Token Header 不跨精确 origin 重定向 | MCP Client + TLS/HTTP 测试服务 | HTTPS 降级、换端口、跨域、两跳回调污染重定向历史 | `TestWithToken_RejectsHTTPSDowngrade*`、`TestWithAuthHeader_*Redirect*`、`TestCredentialRedirectGuard_FreezesInitialOriginAcrossRedirects` | PASS |
| 扩展 Token 不能单独掩盖内置配置解析失败 | 公开 `ResolveCLIIdentity` + 进程隔离的 Credential Provider | 配置失败且仅返回 Token、配置失败且完整返回 Host/Token、无扩展 Token | `TestResolveCLIIdentity_ProviderFailureContracts/invalid-config-token-only`、`/invalid-config-takeover`、`/invalid-config-no-token` | PASS |
| 携带凭证的初始请求不能被前置回调改到其他 authority | 公开 Interceptor + `http.Client.Do` | 修改 `URL.Host`、端口或 `Request.Host`，Bearer 与自定义 Token Header 不得到达底层 Transport，请求体和后置回调仍正确清理 | `TestInterceptingRoundTripper_BlocksAuthorityChangesBeforeCredentialedBaseTransport`、`TestInterceptingRoundTripper_AllowsNonAuthorityRequestChanges` | PASS |
| 不可信 Transport Hook 不会让 CLI panic、消费真实 Body 或制造 Body 数据竞争 | 公开 Interceptor + HTTP Client，Race 模式 | URL=nil、post panic、post 尝试读取 Body | `TestInterceptingRoundTripper_NilURL*`、`*PostMetadataSnapshotDoesNotConsumeLiveBody` | PASS |
| pre hook 读取请求 Body 时，deadline 关闭并解除阻塞且不产生数据竞争 | 公开 Interceptor + `http.Client.Do`，Race 模式 | POST Body 非空、Read 等待输入、Context 超时、底层 Transport 被误调用 | `TestInterceptingRoundTripper_PreTimeoutClosesAndUnblocksRequestBodyRead` | PASS |
| post hook 永久阻塞也不会占住真实响应流；abort 路径仍按 deadline 返回 | 公开 Interceptor + `http.Client.Do` | post 永不返回、abort 后 post 超时、响应 Body/连接未释放 | `TestInterceptingRoundTripper_HungPostCannotRetainLiveResponseBody`、`TestInterceptingRoundTripper_AbortPostTimeoutReturnsAndClosesRequestBody` | PASS |
| Credential Provider 选择 Profile 后，首次初始化沿用同一 Profile | `SessionStep.Execute` + first-run 调用边界 | argv Profile 与 Provider Profile 不同 | `TestSessionStep_FirstRunUsesIdentityProfileSnapshot` | PASS |
| 内置动态命令和风险目录不会漂移，关键写操作不会误标为只读 | 最终 fallback 工具目录 + 风险注解 | 新增工具漏风险、删除工具残留风险、未知风险值、创建/更新/发布类工具风险误标 | `TestCommandRiskDirectoryExactlyCoversFallbackTools`、`TestCommandRiskDirectory_ClassifiesCriticalOperations` | PASS |
| 手写 Restrict 插件不能声明 fail-open 后被静默跳过 | 公开 `Plugin` + 实际企业 CLI 进程 | `Restricts=true` 与 `FailurePolicy=FailOpen` 矛盾、元数据回调失败掩盖矛盾、Install 被误调用、CLI 以 0 退出 | `TestInstallPlugins_FailOpenRestrictFailsBeforeInstall`、`TestInstallPlugins_FailOpenRestrictCannotHideBehindMetadataFailure`、`TestEnterpriseBinary_FailOpenRestrictDeclarationFailsStartup` | PASS |
| 多条 Restrict Rule 都是有效约束 | 企业二进制最终 Cobra 帮助/直接执行 + Runtime 组合测试 | 宽 Rule 绕过窄 Rule 的 MaxRisk/Allow | `TestEnterpriseBinary_Restrict*`、`TestRuntime_AllRestrictionRulesConstrainOverlappingCommand` | PASS |
| Wrapper 不会跳过命令却返回成功 | 企业二进制公开插件 + 最终 Handler | 晚到异步 `next`、双重 `next`、panic | `TestEnterpriseBinary_IgnoredDuplicateNextFailsClosed`、`TestBuildWrapper_LateAsyncNext*` | PASS |
| 扩展错误链的 `Is`、`As`、`Unwrap`、`Error` 或载荷构造 panic 不会逃逸或泄露敏感值 | 企业二进制进程 + 公开 Platform、Transport、Credential、formatter 边界 | 回调 panic 出自定义 error、安装返回恶意 error、错误遍历再次 panic、显式结构化输出、错误分类与渲染 | `TestEnterpriseBinary_PanickingErrorTraversalIsContained`、`TestEnterpriseBinary_CredentialPanickingErrorTraversalIsContained`、`TestInstallPlugins_ContainsPanickingErrorTraversal`、`TestInterceptingRoundTripper_ContainsPanickingErrorTraversal`、`TestGuardCause_ContainsPanickingTraversalAndPreservesSafeMatches`、`TestValidate_ContainsPanickingMetadataErrorTraversal`、`TestSafeMessage_ContainsPanickingErrorMethod`、`TestBuildErrorRecord_PanickingTraversalFallsBackWithoutPanic` | PASS |
| 携带凭证的 MCP 请求始终保留 10 跳重定向上限 | 公开 MCP Client + 真实 HTTP 重定向服务 | 同源循环、已有自定义 `CheckRedirect`、Bearer 与自定义 Header | `TestCredentialRedirectGuard_StopsSameOriginLoopAtTenHops` | PASS |
| 所有 JSON-RPC 响应都有内存读取上限 | 公开 MCP Client `ListTools` / `CallTool` + 真实 HTTP 响应 | `tools/list` 超过 8 MiB、`tools/call` 超过 32 MiB、边界内正常响应 | `TestCallToolRejectsOversizedResponse`、`TestListToolsRejectsOversizedResponse` | PASS |
| 最终静态命令树的风险分类与命令目录同步 | 企业 CLI 最终 Cobra 树 | 新增只读命令遗漏、写命令误标只读、高风险命令降级 | `TestStaticCommandRiskDirectoryMatchesFinalTree` | PASS |
| Transport post-hook 只能修改隔离的 TLS 元数据副本 | 公开 Interceptor + `http.Client.Do` | 修改 snapshot TLS 字段、真实 Response TLS 保持不变、nil TLS | `TestInterceptingRoundTripper_PostMetadataSnapshotClonesTLSState`、`TestInterceptingRoundTripper_PostMetadataSnapshotDoesNotConsumeLiveBody` | PASS |
| 首次初始化的成功控制流不被当作错误，且不吞掉 Shutdown 失败 | CLI App `ExecuteWithIO` + 实际 first-run marker + 进程错误映射 | 假错误 envelope、Shutdown 收到伪错误、fail-closed Shutdown 被映射为退出 0 | `TestAppExecuteWithIO_SuccessfulControlFlow*`、`TestLifecycleCommandError_TreatsSuccessfulFirstRunAsSuccess`、`TestFinalizePlugins_SuccessfulFirstRunYieldsToShutdownError`、`TestRenderExecutionError_MapsSuccessfulExitToZeroWithoutOutput` | PASS |
| 公开诊断命令和版本约束可直接使用 | 外部 Go Module 构建的企业二进制 | README 漏命令、诊断命令不可执行、比较符与版本间有空格 | `TestEnterpriseBinary_SharesCredentialAndExtensionsAcrossDynamicCommand`、`TestPublicEntryBuildsFromAnExternalGoModule`、`TestExtensionDiagnostics_AreDocumentedInBothReadmes` | PASS |
| 坏 profile 不会锁死自救入口，业务命令仍 fail-closed | 官方 CLI 真实进程 + 动态命令缓存 | `${VAR}` 未设置时帮助、登录帮助、版本或配置修复不可用；业务命令错误地继续或丢失变量名 | `TestOfficialBinary_BrokenProfileStillAllowsRecoveryCommands` | PASS |
| `--version` 只有作为 Flag 时才路由到版本命令 | 最终 CLI `ExecuteWithIO` + Cobra Flag 定义 | 字符串 Flag 的值恰好为 `--version` 时被误吞 | `TestAppExecuteWithIO_PreservesVersionLiteralAsStringFlagValue`、`TestMeegleBinary_UsesPublicEntryVersion` | PASS |
| 离线/自救命令不等待 Credential Provider | 企业二进制真实进程 | Provider 永久等待时帮助、版本、配置修复或登录帮助超时；带值的全局 Flag 被误解析 | `TestEnterpriseBinary_OfflineCommandsBypassCredentialProvider` | PASS |
| 业务命令仍对 Credential 错误 fail-closed | 企业二进制真实进程 | Provider 普通错误或 `BlockError` 被静默回退；设备拒绝只因帮助绕过而失效 | `TestEnterpriseBinary_BusinessCommandStillFailsClosedOnCredentialErrors`、`TestEnterpriseBinary_CredentialBlockStopsBusinessCommand`、`TestResolveCLIIdentity_ProviderFailureContracts` | PASS |
| `dev` 不绕过版本约束并给出修复入口 | Platform 兼容性检查 + 外部企业入口构建 | 未注入版本时误放行，或错误未提示 `ExecuteWithVersion` | `TestCheckCLIVersion_DevBuildExplainsEnterpriseVersionEntry`、`TestPublicEntryBuildsFromAnExternalGoModule` | PASS |
| Wrapper 不能把下游命令错误改写成成功 | 企业二进制最终退出码 + Platform After Observer | 同步或异步 `next` 返回错误后 Wrapper 忽略错误并返回 nil，最终误报 exit 0 或审计成功 | `TestBuildWrapper_IgnoredDownstreamErrorStillFails`、`TestEnterpriseBinary_IgnoredAsyncDownstreamErrorFailsCommand` | PASS |
| Transport Hook 的 30 秒保护不能截断业务网络传输 | 公开 Transport Provider + `http.Client.Do` + 附件会话 Client | Hook deadline 传入底层网络、原 Client timeout 被缩短、附件绕过会话 Client | `TestExtensionTransport_DoesNotCapBusinessRequestLifetime`、`TestNewHTTPClient_PreservesCallerTimeoutAndRedirectTLSBaseline`、`TestAttachmentShortcutStep_UsesSessionHTTPClient` | PASS |
| 合法 nullable JSON Schema 参数不会让动态工具消失 | 公开 MCP Client + CLI/SDK discovery | `type=[string,null]`、数组 items nullable、多个非 null 类型的稳定诊断 | `TestListToolsAcceptsNullableUnionParameters`、`TestListToolsDiagnosesUnsupportedMultiTypeUnion`、`TestEnterpriseBinary_SharesCredentialAndExtensionsAcrossDynamicCommand`、`TestNewCommandClient_RegistersMetadataDefinedToolFromToolsList` | PASS |
| pre-hook 替换 Request Body 后，新旧 Body 都被正确释放 | 公开 Interceptor + `http.Client.Do` | 成功下发、pre 拒绝、安全基线拒绝、重复 Close | `TestInterceptingRoundTripper_ReplacedRequestBodyClosesOriginalAndReplacement`、`TestInterceptingRoundTripper_AbortAfterBodyReplacementClosesBothBodies`、`TestInterceptingRoundTripper_SecurityRejectAfterBodyReplacementClosesBothBodies` | PASS |
| 坏 profile 的业务命令错误不依赖本地工具缓存 | 官方 CLI 真实进程 + 临时 Profile/Cache | `${VAR}` 未设置且空缓存时退化成 unknown command；有缓存和无缓存错误不一致 | `TestOfficialBinary_BrokenProfileBusinessCommandFailsWithoutCache`、`TestOfficialBinary_BrokenProfileStillAllowsRecoveryCommands` | PASS |
| Platform 安装和 Startup 回调不能永久阻塞 CLI | 外部企业二进制 + 公开 Plugin/Lifecycle API | metadata/Install/Startup 永久阻塞，FailOpen/FailClosed，超时后迟到注册、重复和 Race | `TestEnterpriseBinary_PlatformCallbacksAreBounded`、`TestInstallPlugins_MetadataTimeoutFailsClosed`、`TestInstallPlugins_TimeoutFreezesStagingRegistrar`、`TestRuntime_StartupTimeoutHonorsFailurePolicy` | PASS |
| `inspect` 使用 Cobra 最终解析的 `--profile` | 官方 CLI 真实命令 + 两个独立 Profile Cache | 指定 Profile 与 current Profile 不同、前置/后置 flag、空缓存 | `TestOfficialBinary_InspectUsesSelectedProfile` | PASS |
| Extension 诊断区分未解析、已激活和失败 | `meegle extension doctor/credentials/transport` | 离线命令跳过解析却误报 built-in active；Provider 失败仍误报 active | `TestEnterpriseBinary_ExtensionDiagnosticsReportResolutionState` | PASS |
| 扩展启动阶段错误遵循显式结构化输出格式 | 企业二进制真实进程 + `cmd.ExecuteWithVersion` | Credential Provider 或 Platform 安装在 CLI App 构造期失败；`--format json/ndjson` 退化成纯文本、丢失稳定 `error.code`；原始 cause 泄露 | `TestEnterpriseBinary_BusinessCommandStillFailsClosedOnCredentialErrors`、`TestEnterpriseBinary_FailOpenRestrictDeclarationFailsStartup` | PASS |
| 发布的 readonly 示例保留扩展排障入口 | `examples/enterprise-cli` 真实进程 + 公开 Restrict Rule | Allow 列表只含业务域，导致 `extension policy` 等诊断命令被隐藏并返回 `CLIENT_COMMAND_DENIED` | `TestEnterpriseBinary_ReadonlyPolicyKeepsExtensionDiagnosticsAccessible` | PASS |
| Transport post-hook 不能通过证书指针修改真实 TLS metadata | 公开 Interceptor + `http.Client.Do`，Race 模式 | 修改 PeerCertificates/VerifiedChains、Request TLS、nil/重复证书对象 | `TestInterceptingRoundTripper_PostMetadataSnapshotDeepClonesCertificates` | PASS |

完整接入代码和所有公开用法见 [`examples/README.md`](../../examples/README.md)。
