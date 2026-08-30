# GitHub Radar 设计 Skills 调研

> 调研快照：2026-08-30
> 目标：为 GitHub Radar 选择一套能稳定指导 Agent 优化产品界面的 Skills，而不是照抄其他项目的页面排版。

## 结论

这次去 GitHub 研究的主要对象应当是“设计方法如何被写成可执行的 Agent Skill”，而不是“哪个项目首页看起来更漂亮”。优秀项目的排版可以作为旁证，但不能直接决定 GitHub Radar 的页面结构。

GitHub Radar 更接近一个长期使用的开源情报工作台，而不是营销网站。最终建议采用下面这条组合流程：

1. **Information Architecture**：先确定用户任务、页面层级、导航、URL 和数据钻取路径。
2. **Impeccable**：以产品界面为对象，统一视觉层级、密度、响应式、可访问性和设计系统。
3. **Anthropic frontend-design**：让设计具有明确的产品语境和视觉主张，避免生成通用后台模板。
4. **UI UX Pro Max**：按仪表盘、图表、导航、表格等具体问题查询规则，补齐可操作的 UX 细节。
5. **Hallmark + Vercel Web Interface Guidelines**：在交付前分别做“去 AI 模板味”和工程可用性审计。

`KPI Dashboard Design` 和 Emil 的设计工程 Skills 作为专项参考；`design-taste-frontend` 不作为主 Skill，因为其当前定位明确排除了 dashboard、data table 和多步骤产品界面。

## 研究边界：选 Skill，不是抄排版

学习开源项目页面依然有价值，但它只能回答“别人如何解决某个局部问题”，不能代替产品设计判断。主要原因有三点：

- **用户任务不同**：一个面向开发者的 landing page，重点是表达价值；GitHub Radar 的重点是发现趋势、比较项目和追溯数据。
- **数据结构不同**：导航、表格、图表和详情页必须由真实数据关系决定，不能由参考页面的视觉结构倒推。
- **技术约束不同**：本项目是 Go 服务端渲染、轻量 CSS/JavaScript、中文优先。照搬 React/Tailwind/动画型项目，会引入不必要的框架和维护成本。

因此，本轮调研采用以下判断标准：

- Skill 是否先理解产品目标、用户和现有代码，再提出设计方案。
- Skill 是否覆盖信息架构、数据可视化、响应式、可访问性和交互状态。
- 规则能否落实到现有 Go + `gohtml` + CSS 架构，而不要求重写技术栈。
- 是否有明确的审计清单，可以在实现后验证结果。
- 是否能约束常见 AI 设计套路，而不是再生产一套新的固定模板。

## 候选 Skills 对比

Stars 为所属 GitHub 仓库的约数，只用于判断社区关注度，不代表单个 Skill 的实际质量或适配度。数值是 2026-08-30 的查询快照，会持续变化。

| Skill / 仓库 | 社区量级快照 | 在本项目中的角色 | 适合 | 不适合 / 使用边界 |
| --- | ---: | --- | --- | --- |
| [Anthropic frontend-design](https://github.com/anthropics/skills/blob/main/skills/frontend-design/SKILL.md) | 仓库约 17.3 万 Stars | 设计方向与审美主张 | 在编码前明确产品目的、受众、视觉语气；让结构表达意义；避免千篇一律的 AI 界面 | 规则偏创意方向，不能单独解决复杂 IA、数据口径和仪表盘信息密度 |
| [Impeccable](https://github.com/pbakaus/impeccable) | 约 6.4 万 Stars | 产品界面的主设计与审计 Skill | 产品型 dashboard、设计系统、排版层级、响应式、无障碍、文案与状态完善 | 部分“独特字体、强视觉变化”建议需要服从中文可读性和现有设计上下文，不能机械执行 |
| [UI UX Pro Max](https://github.com/nextlevelbuilder/ui-ux-pro-max-skill) | 约 12.3 万 Stars | 大型 UX / 图表规则库 | 按产品类型、图表、导航、无障碍和技术栈检索具体规则；适合查漏补缺 | 规则数量多，不宜整套无差别套用；不运行其中不必要的安装、持久化或强制覆盖脚本 |
| [Hallmark](https://github.com/Nutlope/hallmark) | 约 2.8 万 Stars | 最终反模板审计 | 检查卡片套卡片、装饰性渐变、无意义图标、营销式 hero、过度圆角与动画等 AI 痕迹 | 不是 IA 或业务逻辑设计工具；内部产品各页面应共享同一设计系统，不需要每页追求不同主题 |
| [Vercel Web Design Guidelines](https://github.com/vercel-labs/agent-skills/tree/main/skills/web-design-guidelines) | `agent-skills` 仓库约 3.1 万 Stars | 工程质量终检 | 语义 HTML、键盘操作、焦点、表单标签、URL 状态、触摸目标、响应式、图表可访问性 | 包装 Skill 会读取持续变化的远程规则；正式使用应审阅并固定规则版本，不应无条件执行远程内容。原始规则见 [web-interface-guidelines](https://github.com/vercel-labs/web-interface-guidelines/blob/main/command.md) |
| [Information Architecture](https://github.com/julianoczkowski/designer-skills/blob/main/information-architecture/SKILL.md) | 所属仓库约数百 Stars | 设计前的结构骨架 | 站点地图、主/次/工具导航、内容优先级、关键用户流、命名、组件复用和 URL 策略 | 社区量级较小，且不负责视觉细节；价值来自与 GitHub Radar 的任务高度匹配，而非 Star 数量 |
| [KPI Dashboard Design](https://github.com/wshobson/agents/blob/main/plugins/business-analytics/skills/kpi-dashboard-design/SKILL.md) | 所属 `agents` 仓库约 3.9 万 Stars | 数据看板专项规则 | 控制首页核心指标数量，给指标增加趋势和上下文，建立 overview → drill-down 层级，明确指标口径 | 偏通用商业 KPI，需要翻译成项目、Topic、Star 增长和采集健康度，不能直接照搬销售或财务指标模板 |
| [design-taste-frontend](https://github.com/Leonxlnx/taste-skill/blob/main/skills/taste-skill/SKILL.md) | 约 8.2 万 Stars | 参考其“先审计再改造”和 anti-slop 方法 | Landing page、作品集、品牌页面，以及现有项目的视觉审计方法 | Skill 自身明确写明不面向 dashboard、data table 和 multi-step product UI，因此不作为 GitHub Radar 的主设计 Skill；也不引入其默认 GSAP/Tailwind 路线 |
| [Emil Design Engineering](https://github.com/emilkowalski/skills/blob/main/skills/emil-design-eng/SKILL.md) | 约 3.4 万 Stars | 交互与动效专项复核 | 微交互、缓动、进入/退出动画、反馈时机，以及“高频操作应迅速而克制”的设计工程细节 | 不负责整体信息架构；本项目以查数据为主，不应为了展示技巧而增加动效 |

## 每个 Skill 能为 GitHub Radar 解决什么

### 1. Information Architecture：先让页面关系正确

它解决的不是“导航要做成什么颜色”，而是“用户进入系统后，首先应该看到什么，如何继续深入”。应用到 GitHub Radar，应先确定：

- 主导航围绕 `总览 / 项目 / 主题 / 新发现 / 采集状态` 组织。
- 首页回答“现在发生了什么”，项目页回答“哪些项目在增长”，Topic 页回答“哪个方向在聚集注意力”。
- 项目和 Topic 详情页承担钻取，不把所有信息挤进首页。
- 时间范围、筛选和排序应进入 URL，便于分享、刷新和复现研究结果。
- 中文名称和概念保持一致，例如统一使用“新发现”，不要在其他页面混用“新增”“发现流”“今日项目”。

这是本轮设计中优先级最高的一步。只要 IA 错了，再精致的视觉也只是把错误结构做得更漂亮。

### 2. Impeccable：把它做成可信的研究工具

Impeccable 适合承担整个产品界面的主线规范：

- 根据产品使用场景控制信息密度，不把 dashboard 做成营销 landing page。
- 让标题、关键数字、图表、筛选和明细形成稳定的阅读顺序。
- 使用一致的颜色、间距、排版、边框和状态语义，减少一次性样式。
- 系统处理移动端、长文本、空数据、错误状态、键盘焦点和中文文案。
- 在交付前进行 critique、audit、distill、harden 和 polish。

对本项目需要做一层取舍：中文界面的字体选择必须优先保证跨平台可读和部署简单，不能因为反对默认字体就强行加载复杂 Web Font。

### 3. Anthropic frontend-design：建立明确但克制的视觉主张

这个 Skill 的核心价值不是某种固定视觉风格，而是要求设计先回答目的、受众和语气，并作出明确选择。对 GitHub Radar，可将视觉主张定义为：

> 冷静、可信、信息密度适中的开源情报工作台；真实数据是视觉主角，界面本身退到背景。

据此可以排除营销式大标题、无意义插画、渐变背景和“每块数据都装进一张卡片”的通用后台做法，同时保留一个有辨识度的结构选择，例如双层顶部导航或首页“趋势主区 + 采集健康侧栏”。

### 4. UI UX Pro Max：按问题调用规则，而不是整库套用

这个仓库更适合作为查询型知识库。例如：

- 首页需要选图时，查询时间序列、排名和构成图的适用条件。
- 项目列表改造时，查询表格密度、移动端折叠、筛选与排序规则。
- 导航调整时，查询桌面端与移动端模式、触摸目标和焦点顺序。
- 交付前，查询可访问性、国际化、长文本和错误状态清单。

它的价值来自覆盖面，而不是一次性采用全部建议。所有规则仍应由真实用户任务和现有技术栈筛选。

### 5. Hallmark 与 Vercel：两个不同维度的交付闸门

二者不负责替代前面的设计过程，而是在最后问两个不同的问题：

- **Hallmark**：这个界面是否一眼就像 AI 套出来的？是否有营销 hero、渐变字、无意义装饰、卡片套卡片、夸张阴影和过度动效？
- **Vercel Guidelines**：这个界面能否被真实使用？语义结构、键盘焦点、表单标签、URL 状态、触摸目标、移动端、数字格式和图表替代信息是否正确？

只有同时通过“视觉不套路”和“工程可使用”，界面优化才算完成。

## 为什么采用 IA → Impeccable → Anthropic → UIUX Pro Max → Audit

这不是按 Star 从高到低排序，而是按设计决策的依赖关系排序：

```text
用户任务与数据关系
        ↓
Information Architecture
页面、导航、URL 与钻取路径
        ↓
Impeccable
产品层级、设计系统、响应式与状态
        ↓
Anthropic frontend-design
明确视觉主张，避免通用模板
        ↓
UI UX Pro Max + KPI Dashboard Design
针对图表、指标、表格和交互补充专项规则
        ↓
Hallmark + Vercel Guidelines + Emil（按需）
反 AI 模板审计、工程可用性审计、微交互复核
```

如果一开始就从视觉 Skill 入手，容易先做出“好看的模块”，再勉强把数据塞进去。先做 IA，可以确保每个视觉决定都服务于研究流程；最后再用审计型 Skills 约束偏差。

## 对 GitHub Radar 的具体应用原则

### 整体产品

- 产品定位为 **Research Workbench（研究工作台）**，不是开源项目宣传站。
- 使用顶部业务导航，避免为当前数量有限的一级页面引入常驻侧栏和命令面板。
- 页面都围绕一个主要研究问题展开，不在同一屏平均展示所有指标。
- 数据更新时间、覆盖范围和采集状态属于研究可信度，应在总览中可见，但不抢占趋势主图。

### 首页

- 第一屏优先显示数据新鲜度、核心增长趋势和当前最快增长项目。
- 控制 headline 指标数量，避免八九张同权重 KPI 卡片形成噪声。
- 只保留一张主图；其他视图下沉到 Topic 或项目详情页。
- 首页提供明确的“继续研究”入口，例如从增长项目进入详情、从热门 Topic 进入 Topic 详情。

### 项目与 Topic 页面

- 项目列表在桌面端可使用密集表格，移动端转为保留关键字段的记录卡，而不是让整张表横向溢出。
- 项目名称、Star、时间窗增量、Topic 和首次发现时间保持可比较。
- Topic 页面先给排名或趋势，再给精确明细表；图表与表格不是重复装饰，而是“发现模式”和“核对数字”两种阅读方式。
- 详情页使用 breadcrumb，帮助用户从钻取结果回到列表上下文。

### 图表与交互

- 颜色承担数据含义，不承担装饰；同一个 Topic 或状态在不同页面保持一致。
- 图表提供单位、时间范围、口径和可读的替代数据。
- 避免 `transition: all`、弹跳效果和大面积滚动动画；高频筛选与导航应即时响应。
- 交互控件满足键盘焦点和移动端触摸尺寸要求；移动导航在无 JavaScript 时仍应可访问。

## 不采用的做法

- 不因为某个高 Star 项目的首页漂亮，就复制它的 Tab、Hero 或卡片结构。
- 不为了“高级感”引入 React、Tailwind、GSAP、Three.js、shadcn 或第三方 CDN。
- 不运行来源不明或会从 `main` 动态下载内容的设计脚本。
- 不使用 `--force` 或持久化选项覆盖现有项目设计上下文。
- 不把 Skill 仓库 Star 数当作唯一排序依据；与产品问题的匹配度优先。
- 不让每个页面采用不同主题。GitHub Radar 是一个产品，应该共享一套导航、Token、交互和数据语义。

## 建议的后续使用方式

以后每次新增页面或做较大改版，可以复用下面的轻量流程：

1. 先写清楚这个页面要回答的唯一核心问题。
2. 用 Information Architecture 检查入口、层级、URL 和下一步钻取。
3. 用 Impeccable 检查阅读顺序、密度、响应式和边界状态。
4. 用 Anthropic frontend-design 检查设计是否有明确语境，而非默认模板。
5. 只从 UI UX Pro Max / KPI Dashboard Design 中读取该页面需要的专项规则。
6. 在真实中文数据、长项目名和多个屏幕宽度下测试。
7. 最后运行 Hallmark、Vercel 和必要的动效审计，再决定是否交付。

这套组合的目标不是让 Agent “更会装修页面”，而是让它能按产品目标组织信息、做出克制的设计判断，并用可重复的标准验证结果。
