# GitHub Trending 替代品：竞争格局与 RepoTempo 的可守定位

## 结论

有，而且数量不少。

如果把问题严格限定为“让用户发现近期正在上升的 GitHub 仓库”，这已经是一个拥挤但高度碎片化的市场。当前可验证的供给至少覆盖五种形态：更透明的增速榜、GitHub Trending 历史归档、垂直领域雷达、编辑精选与长期复盘，以及 RSS、邮件、机器人等被动投递。GitHub 的 `github-trending` Topic 本身已有 266 个公开仓库，说明做一个榜单镜像、API 或客户端的进入门槛很低；这不是产品数量，却是供给拥挤度的有效代理。[^1]

对 RepoTempo 而言，最重要的判断不是“有没有人做”，而是：

1. **“更好的 GitHub Trending 榜单”已经是红海。** Trendshift、Star History、RepoFOMO、Cresting、GitStar、GitGem 等都在用 Star 增量、相对增长、Fork、活跃度或主题分组重新排序。
2. **“发现后继续追踪”也不是空白。** 自动化的 RepoRadar 会把 GitHub Trending 项目入库并持续累积快照；编辑型的 Repository Radar 把“首次报道以来的增长”定义为全站核心指标；Trendshift 保存 GitHub Trending 上榜史和单仓库活动历史。[^2][^3][^4]
3. **最接近 RepoTempo 当前产品命题的是两个不同的 Radar：**
   - **RepoRadar**：机制最接近，自动抓榜、永久报告、继续记录 Star/Fork、项目页、AI 摘要、Watchlist、RSS、MCP。
   - **Repository Radar**：产品逻辑最接近，人工发现、解释为何值得看、保存首次覆盖时的基线，并持续复盘后来是否真正起飞。
4. **市场尚未形成赢家通吃。** 强产品各占一段：Trendshift 强在公开榜单与分发，Star History 强在曲线与比较，Repository Radar 强在解释与复盘，Cresting 强在 AI 垂直动量，GitStarClub 强在历史榜单。大量新站仍然空数据、停更或口径含混，说明长期稳定采集和用户习惯比做页面更难。
5. **“拥有 Star 历史”刚刚失去独立壁垒。** GitHub 于 2026-09-04 发布了隐私安全的 Star History REST API，公开仓库可直接取得按周分组、内含每日新增数的历史序列。[^5][^6] 以后历史曲线更像基础能力，差异化必须来自数据选择、可信语义、解释和工作流。

因此，RepoTempo 不宜把自己定义成另一个“GitHub Trending 替代品”。更可守的定位是：

> **不只告诉你今天谁红，而是告诉你昨天上榜的项目后来怎么样。**

具体产品类别可以定义为 **GitHub Trending cohort outcome tracker**：完整保存每次日榜、周榜和月榜的上榜证据，然后自动回答这些项目在 7、30、90 天后是持续增长、迅速冷却、二次爆发还是失速。

## 研究口径

本报告以 2026-09-10 为状态核验日。事实优先来自产品官网、官方文档、官方 API 和官方代码仓库；流量、覆盖规模和算法等自报数字均按“产品方自述”处理，不等同于独立审计。

“直接替代品”需同时满足：

1. 核心对象是 GitHub repository；
2. 用户无需先知道仓库名，也能浏览或收到候选项目；
3. 依据时间窗口内的变化排序，或持续归档 GitHub Trending；
4. 官网、API 或自动化仓库显示近期运行证据。

只提供总 Star 排名、单仓库曲线、GitHub 客户端、RSS 镜像或人工月刊的产品，列为相邻替代。GitLab、Codeberg 等代码托管平台不是这里所说的“GitHub Trending 替代品”。

## GitHub Trending 的基线能力

GitHub 当前 Trending 页面仍提供仓库与开发者榜、Today / This week / This month 三个窗口，以及编程语言和自然语言过滤。[^7] GitHub 在推出该功能时说明，榜单每天计算八次，只展示前 25 个结果；排序综合 Stars、Forks、Commits、Follows 和 Pageviews，并对近期事件加权。[^8]

这意味着官方产品本来就不是简单的“过去 24 小时新增 Star 榜”。它的优势是默认入口、GitHub 原生身份和多信号排序；弱点是权重不透明、结果短、没有面向普通用户的历史榜单，也没有把一次发现自动转成后续研究记录。GitHub 的项目发现文档还把 Explore、Search、Stars 和 Following 列为互补路径，其中 Explore 可以基于用户活动给出个性化推荐。[^9]

独立替代品主要在补四个缺口：扩大结果集、公开或替换排序公式、保存历史，以及把榜单推到用户常用的阅读渠道。

## 当前最值得关注的产品

| 产品 | 核心做法 | 历史与后续追踪 | 工作流与分发 | 当前判断 |
| --- | --- | --- | --- | --- |
| [RepoRadar](https://reporadar.spreix.com/) | 每天两次抓 GitHub Trending 日/周/月榜，再读取 GitHub API | 入库后继续保存 Star/Fork 快照；报告永久在线；项目页含曲线、Jump 检测、提交与发布信息 | Watchlist、RSS、MCP、AI 摘要、项目请求 | **机制最像 RepoTempo；现役但很新** |
| [Repository Radar](https://repositoryradar.dev/) | 两位编辑每两周精选约 7 个项目，偏重动量与新颖性 | 把“首次报道以来的 Star 增量”定义为 Momentum；每日刷新，逐期 retrospective | 解读、比较、Watchlist、RSS、公开 JSON | **产品命题最像 RepoTempo；编辑型而非全量榜单** |
| [Trendshift](https://trendshift.io/) | 自有日/周/月/年趋势榜，并保存 GitHub 原榜 | 仓库页有自有榜与 GitHub Trending 上榜史、日/月活动 | Topic、Like、Bookmark、社交提及、付费 API | **最强通用型直接对手** |
| [Cresting](https://cresting.dev/) | AI agents、MCP、开发工具垂直榜；小时级透明 Momentum | 记录 On radar 日期、24h/7d 位次变化和 Star/Fork 快照；冷却后衰减但保留项目页 | Topic 榜、周报、RSS | **AI 垂直动量最接近；不是全 GitHub** |
| [Star History](https://www.star-history.com/) | 周增星榜加任意仓库 Star 曲线 | 完整历史、多个仓库按日期或生命周期对齐比较 | 邮件订阅、Chrome 扩展、图片/CSV、README 嵌入 | **曲线与比较最成熟，研究台账较弱** |
| [GitStarClub](https://gitstarclub.com/) | 预计算周、月、年增星榜和总榜 | 可回看冻结的年度/月度/周度排名，提供仓库和组织曲线 | 多语言、比较、导出 | **历史榜单最深，早期发现与个人动作较弱** |
| [RepoFOMO](https://repofomo.com/) | 每周从约 1,800 个高速增长项目中计算 FomoRank | 7/30/60 日 Star 增量、Fork velocity、曲线预览 | 搜索、筛选、榜单 Badge | **量化透明，但不是实时产品且无研究工作流** |
| [GitStar](https://gitstar.space/) | 日/周/月 Star 增长与累计榜，并引入 npm/PyPI 下载 | 连续日快照形成动量；长期历史榜较弱 | Category、Language、Compare、本地 Watchlist、周报 | **“关注度 × 使用量”方向有价值，当前覆盖仍有限** |
| [GitGem](https://gitgem.org/) | Stars、Forks、活动速度；编辑评选 Gems；垃圾过滤 | Today / Week / Month，跨 GitHub、GitLab、Codeberg | Topic、语言、Forge、提交 Showcase | **跨 Forge 与编辑精选有区分度** |
| [LibHunt](https://www.libhunt.com/) | 监测 Reddit、Hacker News、Dev.to 中的仓库提及，并提供 Star 增长榜 | 有项目提及史和语言垂直增长榜；不形成个人长期研究记录 | Alternatives、Topic、语言站、周报邮件 | **社交热度替代路径，覆盖广** |
| [Best of JS](https://bestofjs.org/) | 人工维护约 2,000 个 Web/Node 项目，每日读取 GitHub 数据 | 展示日、周、月等 Star 变化和历史月榜 | 标签、GitHub 登录 Bookmark、开源代码 | **成熟垂直先例，仅限 Web/JS** |
| [Git Breakout](https://github.com/Changroro/git-breakout) | Trending + Search + GH Archive + 既有候选，每约两小时采集 | 区分新发现、再崛起、当前热度与官方上榜；保存快照和“是否提前发现”成绩 | 搜索、Topic、已读、多语言、自托管 | **最有意思的早发现新进入者，验证样本仍小** |
| [OSS Insight](https://ossinsight.io/trending) | 原本基于数十亿 GitHub Events 做 24h/周/月/3 月活动排名 | 仓库、集合、开发者长期分析和公开 API | 语言、Collections、Compare、开源 | **当前核心趋势榜已暂停，不能算现役强替代** |

### RepoRadar：机制层面的正面对手

RepoRadar 官网写得非常直接：每天两次读取 GitHub Trending，将仓库的 Star、Fork、Watchers、License、Languages、Topics、Contributors、Releases 和活动信息存下来，早期历史用公开事件档案重建，之后由自己的快照继续曲线。它还用“当天增长超过过去 7 日日均三倍且超过最低门槛”标记异常跃升。[^2]

其公开代码说明，GitHub Actions 负责抓榜、更新仓库、生成摘要和日报/周报/月报，仓库里的数据文件就是数据库；Star 曲线每天增加一个真实快照。[^10] 官网在核验日显示 1,145+ 个已跟踪仓库、当天 Trending、项目说明和最近日报，RSS 正常；邮件订阅则明确尚未接通。[^11]

这已经覆盖 RepoTempo 的大部分表层叙事：Trending-first、持续快照、历史图、项目解读、Watchlist、标签与报告。它的弱点是产品非常新，公开采用度尚低，部分页面还暴露重复数据和文案/数据口径不一致。它证明的是**功能可复制**，还不能证明已经建立分发或信任优势。

### Repository Radar：产品逻辑上的正面对手

Repository Radar 不抓取全量 Trending，而是两人编辑团队每两周挑选一组项目。官网在核验日显示 42 期、269 个仓库和 2026-09-09 的最新一期。[^3] 它明确把“从第一次报道到现在的 Star 增量”定义为全站核心 Momentum，并同时观察包下载、二进制下载、Bus factor、头部维护者提交占比、发布节奏和项目活跃状态。全部指标来自 GitHub 公共 API，并按日刷新。[^12]

其优势不在榜单，而在闭环：发现时写观点，项目页保留当时基线，后续给出 Breakout / Steady / Cooling 等轨迹，再对整期项目做 retrospective。Movers 页同时显示 24h、7d、28d 变化和 since-covered 排名；公共 JSON 与 RSS 还能供其他工具复用。[^13][^14]

它与 RepoTempo 的目标用户也高度重叠：开发者、Builder 和投资人。真正的差异在覆盖方法——它是两位编辑双周精选，RepoTempo 可以做 GitHub Trending 全量 cohort 的自动、可审计长期复盘。

### Trendshift：最强通用型产品

Trendshift 同时提供自有 Daily / Weekly / Monthly / Yearly 榜、Topic、Repository、Developer、Insights 和 Stats 页面。它还保存 GitHub Trending 的历史出现次数，并在单仓库页展示自有榜与 GitHub 原榜的首次上榜日期、名次、Star/Fork/PR/Issue 活动及社交提及。[^4][^15]

它的 Signal API 可查询任意时期的自有榜、GitHub Trending 历史和 engagement spike，当前 Starter 价格为每月 9 美元；广告页面自报每月 150K+ 技术受众，这是营销口径，不是独立流量审计。[^16][^17]

Trendshift 的优势是产品完整度和分发，弱点是自有趋势分数的详细公式未找到公开说明，也未发现当前可自托管的完整产品。若 RepoTempo 只说“更清楚的 GitHub 趋势与历史”，会与它正面重叠。

### Cresting：AI 垂直动量的强样本

Cresting 只覆盖 AI agents、MCP、Skills、Dev tools、RAG 与评测等 AI Builder Stack。它的 Momentum 公式完整公开：以 7 日和 24 小时 Star 等信号做对数压缩和 cohort 百分位归一，再乘新项目加成、信号衰减、多来源佐证和质量门槛；异常突增会被 capped，赞助数据与自然排名物理隔离。[^18]

项目页给出 On radar 日期、Star 周增量、24h/7d 位次变化、原始快照数和每个因子的分解；榜单按小时重算，冷却后项目会滑出榜单但仍保留详情页。[^19] 这是当前“透明算法 + 小项目公平比较 + 反短期炒作”做得最清楚的产品之一。局限也很明确：它是 AI 垂直站，历史从进入雷达后开始，并不覆盖 GitHub 全域。

### Star History 与 GitStarClub：历史正在产品化

Star History 已从“输入仓库后画曲线”的工具扩展出每周增星榜、分类、订阅、比较和 Star Race。其核心仍是任意仓库完整曲线、多仓库比较、图片/CSV 导出、浏览器扩展和 README 嵌入。[^20][^21]

GitStarClub 则把时间本身做成主导航：当前周、月、年增量榜与永久历史榜都由预计算数据生成，并明确标注数据截止日。[^22] 两者共同说明，“历史排名”和“单仓库曲线”都已经成为独立产品面，不再只是后台数据。

### RepoFOMO、GitStar、GitGem 与 LibHunt：四种不同信号

RepoFOMO 公开 FomoRank：组合 7、30、60 日新增 Stars 与 Fork velocity，在候选集内做百分位归一；产品也明确声明它不是实时 Feed，数据最多可能滞后一周，且热度不是质量。[^23]

GitStar 将 GitHub 动量与 npm/PyPI 下载量放在一起，用来识别“高关注、低采用”与“低曝光、高使用”的项目，并提供本地 Watchlist、Compare 和周报。[^24] 这是比单纯 Star 榜更接近技术选型的方向，但当前榜单只显示有限缓存集合，尚未形成全域覆盖。

GitGem 用 Stars、Forks 和 Activity velocity 排序，并把人工 Gem、Showcase 所有权验证、垃圾过滤和人工复核加入同一产品；它还跨 GitHub、GitLab 和 Codeberg。[^25]

LibHunt 的主信号不是 GitHub Trending，而是 Reddit、Hacker News 和 Dev.to 对开源仓库的提及；与此同时，各语言 Trending 页按 GitHub Star 相对增长每日更新，并提供 Alternatives 与周报。[^26][^27] 它代表一条重要替代路径：把开发者社区的讨论转成仓库发现信号。

### Best of JS 与 agents-radar：垂直化比“全 GitHub”更容易成立

Best of JS 从 2015 年起维护 Web/Node 项目精选集，每天抓取约 2,000 个项目的数据，再展示最近几天、几周和几个月的变化；用户可用 GitHub 登录保存 Bookmark。[^28] 其长期生命力来自明确用户群和人工分类，而不是更复杂的全域算法。

中文与 AI 场景还有更直接的替代：agents-radar 每天汇总 GitHub Trending、GitHub Search、Hacker News、Product Hunt、ArXiv、Hugging Face 等十个来源，用模型过滤和分类 AI 项目，再生成中英双语 Markdown、GitHub Issues、Web、RSS、Telegram、飞书和 MCP。[^29] 它不提供完整的个人项目库，却直接争夺“每天给我一份 AI 开源动态”的阅读习惯。

## 相邻替代品也在争夺同一段注意力

用户未必会专门打开一个趋势网站。下列产品虽不重做排序，却会替代访问 GitHub Trending 的行为：

- **邮件与个性化 Digest**：Digest 可把 GitHub Trending 作为一个来源，按日/周/月、语言和数量加入个人邮件；当前起价为每月 6 美元。[^30]
- **GitHub 原生通知**：`vitalets/github-trending-repos` 为不同语言建立 Issue，由机器人每天或每周发表评论，订阅 Issue 即可收到 GitHub 通知；核验日仍有当天机器人输出。[^31]
- **RSS**：GitHubTrendingRSS 由 GitHub Actions 每天生成日/周/月 Feed，也可以 Fork 后自行托管。[^32]
- **历史原榜档案**：`antonkomarev/github-trending-archive` 每天把各语言 Trending 的仓库和开发者列表存为 JSON，以弥补官方页面不能回溯的问题。[^33]
- **中文人工精选**：HelloGitHub 提供月刊和每周热点速递；GitHubDaily 自 2015 年起通过公众号、微博、知乎和 X 分发项目介绍。它们解决的不是“精确趋势”，而是“帮我读完并告诉我什么值得看”。[^34][^35]

这类产品提示了一个现实：RepoTempo 的竞争对手不只是数据站，还包括已经占据邮箱、RSS、公众号、飞书或 GitHub 通知的内容渠道。

## 当前失效与口径陷阱

### OSS Insight：技术实力强，但当前榜单不可用

OSS Insight 的公开文档仍把 Trending API 称作 GitHub Trends 的开源替代，并提供 24 小时、周、月、3 个月和语言参数。[^36] 但其现行 Trending 页面明确写明：GitHub 公共事件 Feed 的分页变化导致系统从 2025 年中以后严重漏采 Star、PR 和 Issue 事件，因此暂停依赖这些计数的榜单。[^37]

本次核验直接调用其 `past_week` API 时，HTTP 虽返回 200，响应却是 0 行，并带 `degraded data: star-event derived ranking unavailable` 警告。这是一个很有价值的反例：**接口存活不等于数据可用，空榜也不能被解释成“没有项目”。** RepoTempo 现有“失败不记成零、缺失日期保留缺口”的语义是实际产品优势。

### 名叫 Trending，不代表测量了趋势

不少产品使用以下替代口径：

- 最近创建的仓库按总 Stars 排序；
- 用总 Stars 除以仓库年龄估算 `stars/day`；
- 用总 Stars、Forks、Watchers、Issues 再乘最近更新时间；
- 把付费 Boost、曝光或人工提交混入排名。

这些方法可以帮助发现新项目或活跃项目，却不等同于两个真实时间端点之间的 Star 增量。若产品没有连续快照或官方 Star History 数据，它就不能可靠回答“这周到底增长了多少”。报告因此没有把所有带 Trending 字样的站点都视为直接竞品。

### 新项目很多，但采用度很弱

RepoRadar、Git Breakout、GitHub Discovery、GitHub Trending+、GitHub Trending Intelligence 等都已经实现快照、复合分数、摘要、Watchlist 或通知中的若干项，但多个公开仓库在核验日只有个位数 Stars。它们证明功能组合容易复制，不证明用户愿意长期使用。

相反，Star History、Best of JS、LibHunt 和中文编辑品牌的持续存在说明，真正的壁垒更可能是：长期数据可靠性、固定阅读习惯、清晰的受众范围和可信解释。

## 对 RepoTempo 的逐项影响

| 用户任务 | 当前最强参照 | RepoTempo 当前状态 | 竞争含义 |
| --- | --- | --- | --- |
| 看今天什么在涨 | Trendshift、Cresting、RepoFOMO | 1/7/30 日最快增长 Top 10 | 已商品化，不能单独定位 |
| 回看过去榜单 | Trendshift、GitStarClub、归档仓库 | 保存发现任务证据，但前台历史 cohort 不突出 | 有数据基础，产品表达不足 |
| 看单仓库长期曲线 | Star History、Repository Radar | 自入库起的真实每日绝对快照 | 能做，但官方新 API 已降低壁垒 |
| 离榜后继续观察 | RepoRadar、Repository Radar、Trendshift | 已实现 | 不是唯一能力 |
| 看增长是否降温 | Cresting 的衰减、Repository Radar 的 Cooling | 明确比较相邻等长窗口的 momentum decline | 仍有差异，但容易被复制 |
| 理解“为什么涨” | Repository Radar、GitStar、agents-radar | 有保存摘要，但尚未自动关联 Release、Commit、社区事件 | 明显缺口，也是最有价值的增量 |
| 形成个人研究记录 | Repository Radar、GitStar 的轻量 Watchlist | Watchlist、Note、已读、标签、来源、导出 | 是好基础，应继续深化而非再做榜单 |
| 自己拥有数据 | 少量自托管项目 | Go + SQLite + CSV/JSON/SQLite | 对个人研究者和小团队有明确价值 |
| 核验采集质量 | 少数产品显示 Freshness | Job run、失败记录、缺失不归零、固定可比 cohort | 隐性但真实的可信度优势 |

RepoTempo 的现有优势不是某一项独有功能，而是把以下能力放在同一个可自托管工作区：发现证据、真实绝对快照、严格比较语义、项目说明、个人标记，以及可导出的监测数据与 SQLite 研究记录。[^38] 这个组合仍有价值，但首页若只宣传“更好的趋势榜和 Star 历史”，用户不会理解它与 Trendshift、RepoRadar 或 Star History 的区别。

## GitHub 新 Star History API 改变了什么

GitHub 新接口 `GET /repos/{owner}/{repo}/stargazers/history` 对公开仓库开放，返回从最近周向仓库创建周回溯的序列；每周记录包含 `total` 和从星期日开始的 `days` 数组。[^6]

它带来三个直接影响：

1. **冷启动历史可以回填。** 新产品无需多年运行，就能为任意公开仓库生成历史新增 Star 曲线。
2. **Star 曲线不再是独占资产。** Star History 已在接口发布后迅速切换，其他产品也会跟进。[^39]
3. **RepoTempo 的绝对快照仍有不同价值。** 官方接口给的是每个日期产生的 Star 数，不是 RepoTempo 在每个观察日看到的绝对存量。持续绝对快照仍能保存当时状态、观察净下降、标记采集失败，并提供不会随着以后 Unstar 而悄然改写的观测记录。

最合理的做法不是拒绝新接口，而是把它用于历史回填和冷启动，同时保留 `observed snapshot` 与 `reconstructed acquisition history` 两条明确分开的证据线。这样既获得完整性，也不牺牲当前产品最强的数据语义。

## 仍然存在的市场空白

### 1. GitHub Trending 全量 cohort 的榜后复盘

Repository Radar 会复盘每期约 7 个编辑精选项目，但尚未发现成熟产品系统性地对 GitHub Trending 每次日/周/月榜形成不可变 cohort，并在 7、30、90 天后自动报告：

- 还有多少项目保持原速度；
- 哪些项目快速冷却；
- 哪些项目出现二次爆发；
- 哪些项目总 Star 回落或项目归档；
- 整批项目相对同期 cohort 的中位数和分布；
- 当时榜单名次与后来表现是否有关系。

这正好利用 RepoTempo 已有的榜单来源证据、永久仓库 ID、每日观察和固定可比样本逻辑。

### 2. 从数值变化到有证据的“为什么”

绝大多数竞品只能说“涨了”。少数产品会补充 Release、Commit、PR、下载或 Hacker News 提及，但很少把事件与曲线、原始来源和不确定性放在同一时间线上。

理想输出不是武断地声称因果，而是：

> 该项目在 9 月 6 日发布 v2.0，同日进入 GitHub Trending；随后 48 小时新增关注显著高于此前基线。两件事时间上相关，现有证据不足以证明发布导致增长。

这类解释比再造一个复合分更难复制，也更符合研究者和投资人的真实任务。

### 3. 个人或小团队的开源情报台账

Bookmark 很常见，但以下链路仍少见：为什么关注、是否读过、当前判断、下一次复核日期、判断如何变化、相关证据、谁做过复核，以及能否完整导出。RepoTempo 已有关注、Note、已读、标签、摘要来源和 SQLite 导出，是天然起点。

### 4. 可信度与反操纵

Stars 是注意力指标，不是质量或采用的同义词。既有研究发现，开发者常把 Star 当成受欢迎程度和选型线索，但快速增长也经常来自社交推广。[^40] ICSE 2026 的大规模研究进一步报告约六百万个疑似虚假 Stars，并指出相关活动会污染决策信号和带来软件供应链风险。[^41]

因此，最值得补的不是“更聪明的 AI 分数”，而是可解释的风险提示：异常 Star 模式、Fork/Star 偏离、Release 与贡献者活跃度、包下载、项目年龄、归档状态及数据缺失。每个提示都应展示证据，不把热度包装成质量评分。

## 产品建议

### 定位

不建议使用：

> A better GitHub Trending / GitHub Trending alternative

建议使用：

> **RepoTempo 把 GitHub Trending 从一次性榜单变成可追溯的开源项目研究档案：看清谁上榜、谁退潮，以及 30 或 90 天后谁还在增长。**

### 优先级

1. **P0：做“这批后来怎么样”。** 每次 Trending 日/周/月榜形成不可变 cohort，自动生成 +7、+30、+90 天复盘页。
2. **P0：接入官方 Star History API 做冷启动回填。** 明确区分历史新增序列与本地真实绝对快照，不混成一条伪造的每日存量曲线。
3. **P1：把事件标在曲线上。** 先做 Release、Repository archived/unarchived、重大描述或 README 变化、Trending 再上榜；之后再接社区提及。
4. **P1：把 Watchlist 升级成研究队列。** 增加待读、已读、已复核、下一次复核日期和判断变更历史，并继续保持可导出。
5. **P1：做变化触发的 Digest。** 只有首次上榜、明显加速、明显降温、二次爆发或采集失败时通知，避免每天重复一张静态榜。
6. **P2：增加可解释的信号可信度。** 多信号并列，不给单一“质量分”，不把 Star、Fork 或 LLM 判断冒充采用和投资价值。

### 不建议投入的方向

- 扩成一个覆盖全 GitHub 的巨大总榜；
- 只做更漂亮的 Star History 图；
- 再发明一个无法审计的综合热度分；
- 用“实时”作为主卖点，与小时级产品竞争采集频率；
- 依靠 AI 自动摘要作为差异化，当前多数新产品已经具备这一层。

### Go / No-go

**有条件继续。**

若目标是面向大众做另一个“今天 GitHub 什么最火”的公网站点，建议 No-go：竞品多、默认入口强、数据能力快速同质化，分发成本会高于开发成本。

若目标是服务技术研究者、投资人或小团队，把 GitHub Trending 变成可审计、可复盘、可拥有的长期研究台账，则建议 Go。最窄且最清晰的切口是：

> **GitHub Trending 全量上榜项目的 7/30/90 天后续表现。**

这个切口与 Repository Radar 的区别清楚：对方是两人每两周选择约 7 个项目做编辑复盘；RepoTempo 可以对 GitHub Trending 的完整 cohort 自动化、长期、可核验地回答“后来怎么样”。

## Sources

[^1]: GitHub, “[github-trending Topic](https://github.com/topics/github-trending),” accessed September 10, 2026.
[^2]: RepoRadar, “[About RepoRadar](https://reporadar.spreix.com/about),” accessed September 10, 2026.
[^3]: Repository Radar, “[Repository Radar](https://repositoryradar.dev/),” accessed September 10, 2026.
[^4]: Trendshift, “[GitHub trending repositories](https://trendshift.io/github-trending-repositories),” accessed September 10, 2026.
[^5]: GitHub, “[New API endpoint provides privacy-safe star history data](https://github.blog/changelog/2026-09-04-new-api-endpoint-provides-privacy-safe-star-history-data/),” September 4, 2026.
[^6]: GitHub Docs, “[Get repository star history](https://docs.github.com/en/rest/activity/starring#get-repository-star-history),” accessed September 10, 2026.
[^7]: GitHub, “[Trending repositories on GitHub today](https://github.com/trending),” accessed September 10, 2026.
[^8]: Jon Rohan, GitHub, “[Explore what is Trending on GitHub](https://github.blog/news-insights/company-news/explore-what-is-trending-on-github/),” August 13, 2013; updated December 6, 2019.
[^9]: GitHub Docs, “[Discovering projects on GitHub](https://docs.github.com/en/get-started/exploring-projects-on-github/discovering-projects-on-github),” accessed September 10, 2026.
[^10]: Shashank Saxena, “[Shashank2577/reporadar](https://github.com/Shashank2577/reporadar),” GitHub, accessed September 10, 2026.
[^11]: RepoRadar, “[RepoRadar homepage](https://reporadar.spreix.com/),” accessed September 10, 2026.
[^12]: Repository Radar, “[About Repository Radar](https://repositoryradar.dev/about),” accessed September 10, 2026.
[^13]: Repository Radar, “[Movers](https://repositoryradar.dev/movers),” accessed September 10, 2026.
[^14]: Repository Radar, “[Data](https://repositoryradar.dev/data),” accessed September 10, 2026.
[^15]: Trendshift, “[Repository detail and GitHub Trending history example](https://trendshift.io/repositories/23399),” accessed September 10, 2026.
[^16]: Trendshift, “[Trendshift Signal](https://trendshift.io/signal),” accessed September 10, 2026.
[^17]: Trendshift, “[Advertise on Trendshift](https://trendshift.io/advertise),” accessed September 10, 2026.
[^18]: Cresting, “[How momentum is scored](https://cresting.dev/methodology),” accessed September 10, 2026.
[^19]: Cresting, “[CyberVerse tool detail](https://cresting.dev/tool/cyberverse),” accessed September 10, 2026.
[^20]: Star History, “[GitHub Star History](https://www.star-history.com/),” accessed September 10, 2026.
[^21]: Star History, “[How to use GitHub Star History](https://www.star-history.com/blog/how-to-use-github-star-history/),” April 9, 2026.
[^22]: GitStarClub, “[Open Source Pulse & GitHub Star History](https://gitstarclub.com/),” accessed September 10, 2026.
[^23]: RepoFOMO, “[How RepoFOMO ranks trending GitHub repositories](https://repofomo.com/methodology),” accessed September 10, 2026.
[^24]: GitStar, “[Methodology & Editorial Standards](https://gitstar.space/methodology),” accessed September 10, 2026.
[^25]: GitGem, “[How ranking works](https://gitgem.org/methodology),” accessed September 10, 2026.
[^26]: LibHunt, “[About LibHunt](https://www.libhunt.com/about),” accessed September 10, 2026.
[^27]: LibHunt, “[Trending Python Projects](https://www.libhunt.com/l/python/trending),” accessed September 10, 2026.
[^28]: Best of JS, “[bestofjs/bestofjs](https://github.com/bestofjs/bestofjs),” GitHub, accessed September 10, 2026.
[^29]: duanyytop, “[agents-radar](https://github.com/duanyytop/agents-radar),” GitHub, accessed September 10, 2026.
[^30]: Digest, “[Discover Trending GitHub Repositories with Digest](https://usedigest.com/blog/discover-trending-github-repositories/),” February 5, 2025; and “[Pricing](https://usedigest.com/pricing/),” accessed September 10, 2026.
[^31]: vitalets, “[vitalets/github-trending-repos](https://github.com/vitalets/github-trending-repos),” GitHub, accessed September 10, 2026.
[^32]: mshibanami, “[mshibanami/GitHubTrendingRSS](https://github.com/mshibanami/GitHubTrendingRSS),” GitHub, accessed September 10, 2026.
[^33]: Anton Komarev, “[github-trending-archive](https://github.com/antonkomarev/github-trending-archive),” GitHub, accessed September 10, 2026.
[^34]: HelloGitHub, “[HelloGitHub introduction](https://gitbook.hellogithub.com/),” accessed September 10, 2026.
[^35]: GitHubDaily, “[GitHubDaily/GitHubDaily](https://github.com/GitHubDaily/GitHubDaily),” GitHub, accessed September 10, 2026.
[^36]: OSS Insight Docs, “[List trending repos](https://ossinsight.io/docs/api/list-trending-repos),” accessed September 10, 2026.
[^37]: OSS Insight, “[Trending GitHub Repositories](https://ossinsight.io/trending),” accessed September 10, 2026.
[^38]: RepoTempo, “[xz1220/repotempo](https://github.com/xz1220/repotempo),” GitHub, accessed September 10, 2026.
[^39]: Star History, “[The New GitHub Star History API](https://www.star-history.com/blog/new-github-star-history-api/),” September 5, 2026.
[^40]: Hudson Borges and Marco Tulio Valente, “[What’s in a GitHub Star? Understanding Repository Starring Practices in a Social Coding Platform](https://doi.org/10.1016/j.jss.2018.09.016),” *Journal of Systems and Software* 146, December 2018, pp. 112–129.
[^41]: Hao He et al., “[Six Million (Suspected) Fake Stars on GitHub: A Growing Spiral of Popularity Contests, Spam, and Malware](https://cmustrudel.github.io/papers/icse2026fakestars.pdf),” ICSE 2026.
