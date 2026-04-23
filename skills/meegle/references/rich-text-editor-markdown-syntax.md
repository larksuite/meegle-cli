# 富文本编辑器 Markdown 格式规范

## 概述

富文本编辑器使用基于 **GFM（GitHub Flavored Markdown）** 的扩展 Markdown 格式。对于标准 Markdown 无法表达的功能，通过 HTML 注释和标签进行扩展。该格式可转换为 DSL、Delta 和 doc_html。

**核心原则：** 标准 GFM + HTML 注释承载元数据 + `<span>`/`<u>` 标签补充样式能力。

## 速查表

| 功能 | 语法 | 说明 |
|------|------|------|
| 加粗 | `**文本**` | |
| 斜体 | `*文本*` | |
| 删除线 | `~~文本~~` | |
| 下划线 | `<u>文本</u>` | HTML 标签，非标准 MD |
| 行内代码 | `` `代码` `` | |
| 字体颜色 | `<span style="color: rgb(R, G, B)">文本</span>` | 必须使用 `rgb()` 格式 |
| 背景颜色 | `<span style="background-color: rgb(R, G, B)">文本</span>` | 必须使用 `rgb()` 格式 |
| 字体大小 | `<span style="font-size: Npx">文本</span>` | 值为 px 单位 |
| 标题 | `#` 到 `######` | h1-h6 |
| 有序列表 | `1. 项目` | 嵌套用 4 空格缩进 |
| 无序列表 | `- 项目` | 嵌套用 4 空格缩进 |
| 任务列表 | `- [ ] 待办` / `- [x] 已完成` | |
| 引用块 | `> 文本` | 内部支持嵌套块级元素 |
| 代码块 | ` ```语言 ... ``` ` | 开头栅栏后跟语言标识 |
| 链接 | `[文本](url)` | |
| 图片 | `![描述](url)<!-- 图片uuid -->` | 注释前无空格 |
| 链接预览 | `[文本](url)<!-- linkPreview -->` | 注释前无空格 |
| 分割线 | `---` | |
| 表情 | `:ShortCode:` | 大小写敏感的规范键名 |
| 居中对齐 | `<!-- center:start -->` ... `<!-- center:end -->` | 区域式 |
| 右对齐 | `<!-- right:start -->` ... `<!-- right:end -->` | 区域式 |
| 两端对齐 | `<!-- justify:start -->` ... `<!-- justify:end -->` | 区域式 |
| @提及 | `@名字<!-- mention:{JSON} -->` | 注释前无空格 |

## 扩展语法详解

### 对齐方式（区域式）

用 start/end 注释对包裹一个或多个段落，标签必须独占一行：

```markdown
<!-- center:start -->
这段文字居中显示。

这段也是居中的。
<!-- center:end -->

<!-- right:start -->
右对齐内容。
<!-- right:end -->
```

支持的值：`center`（居中）、`right`（右对齐）、`justify`（两端对齐）。左对齐为默认值，无需标记。

### @提及（带元数据）

为了在转换过程中保留用户身份信息，使用元数据格式。`@名字` 和注释之间**不能有空格**：

```markdown
@张三<!-- mention:{"id":"lark_user_id_7361251974161006596","cn_name":"张三","en_name":"Zhang San","email":"zhangsan@example.com","blockType":"AT_USER_BLOCK"} -->
```

注释中必填的 JSON 字段：

| 字段 | 说明 | 示例 |
|------|------|------|
| `id` | 用户 ID | `"lark_user_id_7361251974161006596"` |
| `cn_name` | 中文名 | `"张三"` |
| `en_name` | 英文名 | `"Zhang San"` |
| `email` | 邮箱地址 | `"zhangsan@example.com"` |
| `blockType` | 固定值 | `"AT_USER_BLOCK"` |

可选字段：`blockId`（UUID v4）、`type`（0 = 用户）、`avatar_url`。

同一行多个提及（之间不加空格）：

```markdown
@张三<!-- mention:{"id":"id_1","cn_name":"张三","en_name":"Zhang San","email":"zhangsan@example.com","blockType":"AT_USER_BLOCK"} -->@李四<!-- mention:{"id":"id_2","cn_name":"李四","en_name":"Li Si","email":"lisi@example.com","blockType":"AT_USER_BLOCK"} -->
```

如果没有元数据，纯 `@名字` 也可接受，但无法在格式转换中完整还原。

### 图片
在url后紧跟`<!-- 图片uuid -->`（**无空格**）：

图片uuid 是与图片平台约定的唯一凭证。

```markdown
`![描述](https://example.com/page/***)<!-- *****-*****-**** -->`
```

### 链接预览

在链接后紧跟 `<!-- linkPreview -->`（**无空格**）：

```markdown
[https://example.com/page](https://example.com/page)<!-- linkPreview -->
```

### 下划线

标准 Markdown 不支持下划线，使用 HTML `<u>` 标签：

```markdown
<u>带下划线的文本</u>
```

可与其他格式嵌套：

```markdown
*<u>斜体加下划线</u>*
**<u>加粗加下划线</u>**
```

### Span 样式

字体颜色、背景颜色、字体大小使用 `<span>` 的 `style` 属性。**颜色必须用 `rgb(R, G, B)` 格式**（不支持 hex 和颜色名）：

```markdown
<span style="color: rgb(245, 74, 69)">红色文字</span>
<span style="background-color: rgb(53, 189, 75)">绿色背景</span>
<span style="font-size: 18px">大号文字</span>
```

### 表情短代码

使用 `:CODE:` 格式。键名大小写敏感，解析时会归一化到 lark 规范形式：

```
:OK: :DarkThumbsup: :THANKS: :DarkFightOn: :DarkFingerHeart: :APPLAUSE: :LightFistBump: :JIAYI: :DONE: :SMILE: :Delighted: :BeamingFace: :BLUSH: :LAUGH: :SMIRK: :LOL: :FACEPALM: :LOVE: :ERROR: :CRY: :SOB: :THINKING: :SCOWL: :SMART: :WITTY: :PROUD: :WINK: :NOSEPICK: :HAUGHTY: :SLAP: :SPITBLOOD: :TOASTED: :ColdSweat: :BLACKFACE: :FullMoonFace: :GLANCE: :DULL: :ROSE: :HEART: :PARTY: :INNOCENTSMILE: :SHY: :CHUCKLE: :JOYFUL: :WOW: :OBSESSED: :DROOL: :SMOOCH: :KISS: :EMBARRASSED: :TEARS: :ENOUGH: :YEAH: :TRICK: :MONEY: :TEASE: :SHOWOFF: :COMFORT: :CLAP: :PRAISE: :STRIVE: :XBLUSH: :SILENT: :HUG: :WHIMPER: :CRAZY: :WAIL: :LOOKDOWN: :DIZZY: :FROWN: :WHAT: :WAVE: :BLUBBER: :WRONGED: :HUSKY: :SHHH: :SMUG: :ANGRY: :HAMMER: :SHOCKED: :TERROR: :PUKE: :SICK: :YAWN: :DROWSY: :SLEEP: :SPEECHLESS: :SWEAT: :SKULL: :PETRIFIED: :BETRAYED: :HEADSET: :EatingFood: :Typing: :Lemon: :Get: :LGTM: :OnIt: :OneSecond: :YouAreTheBest: :Shrug: :ThanksFace: :SaluteFace: :GoGoGo: :Partying: :VRHeadset: :MeMeMe: :Sigh: :DarkSalute: :DarkShake: :LightHighFive: :DarkWavingHand: :DarkClick: :DarkThumbsDown: :ClownFace: :SLIGHT: :TONGUE: :LIPS: :SiSiASYouWish: :HappyDragon: :JubilantRabbit: :RoarForYou: :CALF: :BULL: :BEAR: :EYESCLOSED: :BEER: :CAKE: :GIFT: :CUCUMBER: :Drumstick: :Pepper: :CANDIEDHAWS: :BubbleTea: :Coffee: :Pin: :AWESOMEN: :Hundred: :MinusOne: :CrossMark: :CheckMark: :OKR: :No: :Yes: :Alarm: :Loudspeaker: :Trophy: :Fire: :RAINBOWPUKE: :Music: :TV: :Movie: :Pumpkin: :LUCK: :FORTUNE: :REDPACKET: :BeAtTheForefront: :2026: :FIREWORKS: :XmasHat: :Snowman: :XmasTree: :FIRECRACKER: :StickyRiceBalls: :Mooncake: :MoonRabbit: :HEARTBROKEN: :BOMB: :POOP: :18X: :CLEAVER: :GeneralWorkFromHome: :GeneralBusinessTrip: :StatusFlashOfInspiration: :StatusReading: :GeneralInMeetingBusy: :Status_PrivateMessage: :GeneralDoNotDisturb: :Basketball: :Soccer: :StatusEnjoyLife: :GeneralTravellingCar: :StatusBus: :StatusInFlight: :GeneralSun: :GeneralMoonRest: 
```

解析器会归一化大小写（`:smile:` → `:SMILE:`，`:beamingface:` → `:BeamingFace:`），但建议直接使用规范写法。

### 列表 — 4 空格缩进

嵌套列表使用 **4 个空格**（不是 2 个）缩进：

```markdown
1. 第一级有序
    1. 第二级（4 空格）
        1. 第三级（8 空格）
    - 混合：有序中嵌套无序（4 空格）
- 第一级无序
    - 第二级
        - 第三级
    1. 混合：无序中嵌套有序
```

任务列表：

```markdown
- [ ] 未完成任务
- [x] 已完成任务
    - [ ] 嵌套未完成
    - [x] 嵌套已完成
```

### 引用块

支持嵌套和内部块级内容：

```markdown
> 带 **加粗** 和 *斜体* 的引用
> 1. 引用内有序列表
> 2. 第二项
>     1. 引用内嵌套列表
> - 引用内无序列表
```

### 代码块

使用围栏式代码块，开头标注语言：

````markdown
```TypeScript
function add(a: number, b: number): number {
  return a + b;
}
```

```Go
func add(a, b int) int {
    return a + b
}
```
````

### GFM 表格（简单内容）

表格内仅包含行内内容（文本、加粗、链接等）时使用：

```markdown
| 表头1 | 表头2 | 表头3 |
|-------|-------|-------|
| 单元格1 | **加粗** | [链接](url) |
| 单元格3 | 单元格4 | 单元格5 |
```

### HTML 表格（单元格内含富内容）

当表格单元格需要块级元素（标题、列表、代码块、对齐、图片）时，使用 HTML `<table>` 语法。**`<td>` 后和 `</td>` 前必须留空行**，这样内部的 Markdown 才能被正确解析：

```markdown
<table>
<tr>
<td>

# 单元格内标题

**加粗段落**

</td>
<td>

1. 有序列表
2. 在单元格中
    - 嵌套项

</td>
<td>

<!-- center:start -->
单元格内居中
<!-- center:end -->

</td>
</tr>
<tr>
<td>

```TypeScript
// 单元格内代码块
const x = 1;
```

</td>
<td>

> 单元格内引用块

</td>
<td>

<span style="color: rgb(245, 74, 69)">单元格内彩色文字</span>

</td>
</tr>
</table>
```

空单元格：`<td></td>`

## 格式组合

行内样式可以嵌套使用：

```markdown
**~~加粗删除线~~**
*<u>斜体下划线</u>*
[**加粗链接**](https://example.com)
<span style="color: rgb(245, 74, 69)">**红色加粗**</span>
```

## 段落分隔

每个块级元素（段落、标题、列表组、表格、代码块、引用块）之间用空行分隔：

```markdown
# 标题

第一段正文。

第二段正文。

- 列表项 1
- 列表项 2

列表后面的段落。
```

## 常见错误

| 错误写法 | 正确写法 |
|----------|----------|
| `<b>加粗</b>` | `**加粗**` |
| `<i>斜体</i>` | `*斜体*` |
| `<s>删除</s>` 或 `<del>删除</del>` | `~~删除~~` |
| `<span style="color: #ff0000">` | `<span style="color: rgb(255, 0, 0)">` |
| `<span style="color: red">` | `<span style="color: rgb(255, 0, 0)">` |
| `<!-- center -->文本<!-- /center -->` | `<!-- center:start -->\n文本\n<!-- center:end -->` |
| `@名字 <!-- mention:... -->`（有空格） | `@名字<!-- mention:... -->`（无空格） |
| `[链接](url) <!-- linkPreview -->`（有空格） | `[链接](url)<!-- linkPreview -->`（无空格） |
| 2 空格嵌套列表缩进 | 4 空格嵌套列表缩进 |
| `:smile:`（全小写） | `:SMILE:`（使用规范大小写） |
| `![图片](url) <!-- 图片uuid -->`（有空格） | `![图片](url)<!-- 图片uuid -->`（无空格） |
| `![图片](url)<!-- linkPreview -->` | `<!-- linkPreview -->` 仅用于 `[文本](url)` 链接 |
| `<td>文本</td>`（`<td>` 后无空行） | `<td>\n\n文本\n\n</td>`（需要空行） |

## 完整示例

```markdown
# 项目进展

## 状态

<!-- center:start -->
**Alpha 项目 — 迭代评审**
<!-- center:end -->

本迭代完成了以下工作：

1. 用户认证
    1. 登录流程
    2. 密码重置
2. 仪表盘改版
    - 新布局
    - 性能优化

### 关键指标

| 指标 | 改版前 | 改版后 |
|------|--------|--------|
| 加载耗时 | 3.2s | **1.1s** |
| 错误率 | 2.4% | <span style="color: rgb(53, 189, 75)">0.3%</span> |

### 代码变更

```TypeScript
export function authenticate(token: string): boolean {
  return validateJWT(token);
}
```

> 注意：部署前需要更新 <u>环境配置</u>。

- [x] 代码评审已完成
- [x] 测试通过
- [ ] 部署到预发环境

负责人：@张三<!-- mention:{"id":"lark_user_id_001","cn_name":"张三","en_name":"Zhang San","email":"zhangsan@example.com","blockType":"AT_USER_BLOCK"} -->@李四<!-- mention:{"id":"lark_user_id_002","cn_name":"李四","en_name":"Li Si","email":"lisi@example.com","blockType":"AT_USER_BLOCK"} -->

参考文档：[迭代看板](https://example.com/sprint/42)<!-- linkPreview -->

:DONE: :DarkThumbsup:
```