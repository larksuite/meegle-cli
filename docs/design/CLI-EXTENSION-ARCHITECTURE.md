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

## 显式产品装配

`NewProductApp` 供本 Go Module 内的产品装配层调用，位于 `internal/products/meegle`；它不是可由外部 Go Module 导入的新插件包。官方 `NewCLIAppContext` 入口继续使用原身份、配置、目录缓存、静态命令及扩展流程。

- `ProductConfig.Registry` 接收独立目录来源。自定义产品优先使用 `NewCommandRegistry` 接收已经完成映射和选择的定义；只有需要 Meegle 既有目录策略时才复用 `NewDynamicRegistrySetup(ToolLister, cache, ...)`。显式注册的空集合表示不注册命令；shortcut 不会自动添加。目录错误直接返回，不会由产品装配入口重新选择目录或身份；DynamicRegistrySetup 保留 Meegle fallback 映射、保留路径校验及 user me、批量、附件 shortcut 自动注入，缓存和降级由既有选项决定；它不是中性的目录映射器。调用方保留该实例，在 App 构造后调用 `MappingIssues()` 获取被隔离工具的诊断；App 不会自动展示这些诊断。
- `BusinessClient` 复用 `mcpclient.New` 和 `WithHTTPClient`，用于普通请求、批量、分页及附件预处理。必填的 `FileClient` 单独负责附件字节传输。客户端由调用方持有，需在 App 生命周期内保持有效，不应在并发使用时修改。
- `Identity` 提供已解析的附件路由头；业务鉴权由 BusinessClient 管理，不会重新解析本地配置。头部和命令定义在构造时复制。默认认证头和指定认证头不会转发到下载地址。
- `CommandDefinition.Result` 和当前用户 shortcut 的 `Result` 可选择默认行为、原始结果或现有当前用户转换；批量与附件继续使用其固定结果结构。`ShortcutBinding` 明确提供场景种类、依赖工具名和参数定义，不要求同时暴露该工具的普通命令。
- `DecodeToolDirectory` 接收 `{"tools":[...]}` 结果对象，保留 8 MiB 总输入、256 KiB 单工具、2048 个工具及既有参数/schema 校验。非法工具继续作为诊断条目隔离，合法条目保留。调用方自行处理外层协议和 metadata 策略；入口没有忽略校验选项。

中性装配示例（位于本模块内，省略 import）：

```go
setup, err := meegle.NewCommandRegistry(
    []meegle.CommandDefinition{{
        Command: types.MappedCommand{
            Resource: "record", Method: "get", ToolName: "sample_get",
            Parameters: []types.ToolParameter{{Name: "work_item_id", Type: "integer", Required: true}},
        },
        Result: meegle.RawResult,
    }},
    []meegle.ShortcutBinding{{
        Kind: meegle.ShortcutBatchGet, ToolName: "sample_get",
        Parameters: []types.ToolParameter{{Name: "work_item_id", Type: "integer", Required: true}},
    }},
)
if err != nil { return err }
app, err := meegle.NewProductApp(ctx, meegle.ProductConfig{
    Name: "sample-cli", Version: "1.0.0", Registry: setup,
    BusinessClient: mcpclient.New(endpoint, mcpclient.WithHTTPClient(businessHTTP)),
    FileClient: fileHTTP,
})
if err != nil { return err }
return app.Execute(ctx, []string{"record", "get", "--work-item-id", "123"})
```

`CustomizeRoot` 复用现有根命令定制能力。显式装配不会自动安装官方静态命令或插件；调用方通过这个独立选项决定所需命令。构造期目录与自定义器的 panic 转换为不含原始异常内容的 `CLIENT_MISCONFIGURED`；Context 取消和普通错误返回给调用方。命令快照只在构造时读取，运行期不回调目录；需要更新目录时重新构造 App。

产品装配按一次执行创建一个 App 使用，顺序或并发的下一次执行均重新构造；目录定义和客户端可以按各自契约复用。同一 App 顺序执行时 flag 值可能残留，不能把“不并发”视为跨请求隔离保证。既有 SDK 的 `CommandClient` 也复用 App，该问题独立修复，不属于本装配接口的复用保证。

必填、附件、批量建议及后端工具名替换使用当前 CLI 名称，缺失调用来源时回退 `meegle`；普通调用、后续分页、批量终止和批量逐项错误遵循同一规则。显式批量 shortcut 的帮助描述绑定工具，不假设它存在同名普通命令。调用方提供的认证刷新失败建议仍沿用客户端既有行为，本接口不承诺所有认证提示均已产品化。

## 显式产品装配验收

基线：`07e8c81c9e7a9cd50893554e342e901c81664be6`。以下结果来自同一输入与可控 HTTP 返回下的对照；Mock、导出产物验证和真实联调分别记录。

| 需求 | 可观察行为与测试边界 | 正向与失败场景 | 证据 | 状态 |
|---|---|---|---|---|
| 默认行为 | 基线与最终导出 CLI 进程的命令、help、inspect、请求、输出、退出码一致 | 配置/身份优先级、缓存/刷新/空目录/坏目录、错误、普通/批量/分页/user me/附件、无关环境变量不改变默认行为 | `scripts/compare-cli-exports.py`：31 组导出二进制差分；现有 CLI/SDK 集成测试 | PASS |
| 显式依赖 | 产品 App → 真实 HTTP 服务；业务与文件路由独立 | 成功、缺失依赖、typed nil、请求错误/取消/超时、Body 关闭；失败不回退配置或身份 | `TestProductAssembly*`：真实本地 HTTP、构造期快照/panic、业务取消、文件失败与清理 | PASS |
| 目录解码 | 严格解码入口与 HTTP ListTools 返回一致 | 合法、坏 schema/metadata、非法 JSON、工具/参数/数量/总字节超限 | `TestDecodeToolDirectory*` 及既有 `TestListTools*` | PASS |
| 显式注册 | App 命令树和实际调用/结果 | 空集合、普通/shortcut 筛选、工具重绑、原始/已有转换、路径/参数冲突、输入后续修改、交替/并发实例 | `TestCommandRegistry*` / `TestProductAssembly*`；全量 race、三轮乱序 | PASS |
| 当前产品建议 | CLI stderr、结构化错误、退出码 | 普通/附件/批量建议；默认、sample-cli、空产品、缺失 Source | `TestProductSuggestions*`：CLI JSON 输出及下载错误 | PASS |
| 后端错误中的命令名 | 产品 CLI → 本地 HTTP | 普通失败、后续分页失败、批量逐项失败；默认/自定义名称、错误码和退出码 | `TestProductBackendErrorsCLI`；race + 三轮乱序 | PASS |
| 批量终止错误 | 批量执行器 → 注入调用边界 | 鉴权终止、默认/自定义名称、空/缺失 Source 回退、错误身份与状态保留 | `TestBatchTerminalErrorProductName`；HTTP 401 的既有错误不携带工具名，此处用注入错误覆盖替换分支 | PASS |
| 批量帮助 | 产品 CLI help 与默认命令树 | 仅绑定 shortcut、不同工具名；默认帮助保持原文 | `TestProductBatchHelpCLI`、`TestCommandRegistryDefaultTreeUnchanged` | PASS |
| 最终产物 | 实际开源导出树编译与测试 | 无私有实现/私有变量读取/替代映射；无内仓依赖 | `opensource-sync-build.sh`；导出扫描、构建、测试及产物差分 | PASS |
| 真实联调 | 导出后的官方 CLI | `user me --format json`，退出 0，返回既有五个用户字段 | `ecab2ef7` 导出产物的既有只读证据，未记录用户信息；本轮未复跑 | PASS（仅该产物及命令） |
| 其他真实场景 | 真实批量、分页及附件请求 | 需要指定测试记录及附件 | 已有本地 HTTP 证据；未构造真实测试数据 | BLOCKED |

扩展的 HTTP Transport、目录来源和既有自定义器为调用方提供的进程内代码。取消依赖它们遵守 Context；不承诺终止不配合取消的代码。同一 Cobra App 不承诺顺序复用或并发执行；并发验证针对独立 App 实例及现有批量执行器。


本地 `scripts/review.sh`（lint、许可证与全部测试）、`go test ./... -race -count=1 -timeout 300s` 和 CI 指定扩展包的三轮乱序测试均通过。默认行为差分覆盖官方入口的帮助/版本/配置/诊断，以及使用相同测试 CA 的公开入口请求、身份优先级、目录缓存与错误、批量/分页/user me、单段附件文件与退出码。比较忽略请求序号，保留业务参数、相关请求头和单段文件内容哈希；multipart 行为由既有附件测试验证，不计作二进制差分已覆盖。真实批量、分页与附件联调和发布验证不由本地服务测试替代。

### 复验导出产物

`scripts/compare-cli-exports.py` 仅依赖 Python 3.9+、Go 和 OpenSSL，接收两份已导出的源码树及一个不存在的输出目录。它构建两份官方入口和使用相同本地测试 CA 的请求入口，输出逐项结果与 `comparison.json`，任何差异使进程失败。测试身份、目录、HTTP 返回和附件均为本地合成数据，不读取真实登录身份。31 组包含一个无关环境变量场景；这项不等同于扫描或验证任何特定私有集成。

在内部源码仓库根目录执行（导出脚本另需 rsync）：

```bash
comparison_dir=$(mktemp -d)
mkdir "$comparison_dir/baseline-src"
git archive 07e8c81c9e7a9cd50893554e342e901c81664be6 | tar -x -C "$comparison_dir/baseline-src"
bash "$comparison_dir/baseline-src/scripts/opensource-sync-build.sh" "$comparison_dir/baseline-export"
bash scripts/opensource-sync-build.sh "$comparison_dir/candidate-export"
python3 scripts/compare-cli-exports.py \
  "$comparison_dir/baseline-export" "$comparison_dir/candidate-export" \
  "$comparison_dir/results" --candidate-ref "$(git rev-parse HEAD)"
```

候选树应为干净的已提交状态；有未提交修改时，`--candidate-ref` 应明确标记快照，不能把结果归于未包含这些修改的提交。脚本比较 stdout/stderr、退出码、业务请求及相关头部和单段附件内容哈希；请求 ID 忽略，批量请求顺序排序。它不覆盖真实服务、多段附件或同一 App 跨请求复用，也不替代 CI 的 Go 环境验证。

## 显式 Go Handler 命令

`ProductConfig.Handlers` 可选，类型为 `map[string]executor.Handler`。命令节点通过 `Meta.Tags[TagExecutorKind] = ExecutorKindHandler` 显式选择 Handler，并以 `HandlerRef` 查找实现；未声明执行类型的命令继续使用 MCP。空引用、缺失或 nil 实现、未知执行类型、命令路径或别名冲突均在产品构造时失败，不回退到其他执行路径或身份。

Handler 可以位于现有命令树的任意层级，也可以与普通 Tool 命令共享同一父节点。公共层不规定业务 namespace、命名或筛选规则；这些策略由私有产品层维护。自定义 `ProductRegistry.Setup` 返回最终命令树，`MappedCommands` 保留工具定义快照用于既有提示；移除某个普通命令节点不需要删除 Handler 持有的工具定义或业务客户端。不要为了注册业务 Handler 扩展内置 `ShortcutKind`。

以下中性示例位于同一 Go Module 内，省略 import；`businessClient` 和 `fileClient` 沿用显式装配的必填依赖：

```go
type sampleRegistry struct { registry.RegistrySetup }
func (sampleRegistry) MappedCommands() []types.MappedCommand { return nil }

setup := sampleRegistry{registry.NewStaticSetup(&registry.CommandTree{
    Nodes: []*registry.CommandNode{{
        Name: "sample", Help: registry.HelpText{Brief: "Sample commands"},
        Children: []*registry.CommandNode{{
            Name: "record", Help: registry.HelpText{Brief: "Record commands"},
            Children: []*registry.CommandNode{{
                Name: "echo", HandlerRef: "sample.echo",
                Help: registry.HelpText{Brief: "Echo a sample value"},
                Flags: []registry.FlagDef{{Name: "value", Type: registry.FlagTypeString, Required: true}},
                Meta: registry.NodeMeta{Tags: map[string]string{
                    meegle.TagExecutorKind: meegle.ExecutorKindHandler,
                }},
            }},
        }},
    }},
})}
app, err := meegle.NewProductApp(ctx, meegle.ProductConfig{
    Name: "sample-cli", Version: "1.0.0", Registry: setup,
    BusinessClient: businessClient, FileClient: fileClient,
    Handlers: map[string]executor.Handler{
        "sample.echo": func(ctx context.Context, req *executor.Request) (*executor.RawResult, error) {
            if err := ctx.Err(); err != nil { return nil, err }
            return &executor.RawResult{Data: map[string]any{"value": req.Values["value"]}}, nil
        },
    },
})
if err != nil { return err }
return app.Execute(ctx, []string{"sample", "record", "echo", "--value", "hello", "--format", "json"})
```

Handler 收到本次调用的 Context、合并后的参数和输出选项。参数采用声明的 CLI flag 名称，复用 `--params`、`--set` 与显式 flag 优先级；已声明的 snake_case 参数名归一为对应 kebab-case flag。必填、枚举及声明类型在调用前校验，数值保留精度。全局参数、父节点参数和当前节点参数均参与校验。`--params` / `--set` 中未声明的顶层参数在执行前报 `CLIENT_INVALID_VALUE`，dry-run 也遵守此校验；内置选项允许使用，object 内部字段不受顶层参数名校验限制。`help`、自行装配的 `inspect` 和 `--dry-run` 不调用 Handler；dry-run 使用现有归一化请求输出。

未显式传入且未配置默认值的可选 `object` / `object-slice` flag 沿用现有框架默认值，Handler 收到空字符串；显式传入并校验通过后分别为 `map[string]any` / `[]map[string]any`。Handler 可通过 `req.Command.ExplicitFlags` 判断是否显式传入，再读取对应值，避免对未传参数直接做对象类型断言。

Handler 返回 `executor.RawResult` 或 error，不直接输出最终 JSON，也不退出进程。nil 结果、结果内错误、typed nil 错误、panic 和不可序列化结果均失败；合法 JSON null 数据允许返回。Data/Metadata 在共享输出前转换为独立的 JSON 数据快照，数值保留为 `json.Number`，自定义序列化器不会在输出阶段重复调用。`--select`、`--envelope`、格式选项和错误输出继续由现有流程处理。

注册集合和命令路由元数据由每个 App 独立保存，无全局 Handler 表。继续遵守“一次执行创建一个 App”；Handler 可调用独立 App，不能重入同一 App。闭包及客户端由调用方管理，复用时须满足自身的并发约束。Handler 与序列化器同步执行，必须配合 Context 并清理自己的资源；取消或超时后不发布成功结果，不承诺强制终止不配合取消的进程内代码。

## Go Handler 接入验收

基线：`cdec89e056931b6092bc6ceea3cebb58b909503c`。本节仅覆盖同一 Go Module 内的显式产品装配，不代表私有产品或 Sidecar 已完成联调。

| 需求 | 可观察行为与测试边界 | 正向与失败场景 | 证据 | 状态 |
|---|---|---|---|---|
| 默认行为 | 基线与候选开源 CLI 进程 | 命令、参数、请求、输出、错误、退出码一致 | `scripts/compare-cli-exports.py`，既有 CLI 集成测试 | PASS |
| 显式 Handler | 产品 CLI → Handler / 本地 HTTP | 参数及 Handler 实收声明类型、输出选项、结果；与动态命令共存；隐藏普通命令仍可调用后端工具 | `TestProductHandler*` | PASS |
| 无意外执行 | CLI help / inspect / dry-run / 参数校验 | Handler 与业务调用次数为零；缺失、非法及未知顶层参数执行前失败 | `TestProductHandler*` | PASS |
| 失败与取消 | CLI 错误输出及 Handler Context | 缺失/nil 引用、返回错误、nil 结果、结果错误、不可序列化结果、panic、取消、超时；不回退 | `TestProductHandler*` | PASS |
| 隔离与冲突 | 独立产品 App 与最终命令树 | 多级路径及祖先参数、相同引用不同实现、输入快照、路径/别名冲突、重入到独立实例、重复/乱序/race | `TestProductHandler*`，完整 race 与三轮乱序 | PASS |

验证在与提交源码一致的干净目录运行，使用同一 Go 工具链和模块依赖。`scripts/review.sh`、全量 `go test ./... -race -count=1 -timeout 300s`、CI 指定扩展包的 `-shuffle=on -count=3`、开源导出扫描/构建/测试全部通过。`compare-cli-exports.py --baseline-ref cdec89e056931b6092bc6ceea3cebb58b909503c` 的 31 组二进制对照全部通过；基线和候选分别使用对应源码导出的目录。真实私有产品与 Sidecar 未联调。
