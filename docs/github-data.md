# GitHub 数据发现与分类

RepoTempo 以 GitHub Trending 官网榜单作为主要发现渠道，GitHub Search API 补充覆盖。OSS Insight 适配器保留用于兼容历史数据，默认关闭；省略整个 `ossinsight` 配置也不会初始化或请求该服务。

每日任务读取 Trending 的全语言日、周、月榜，并执行到期的搜索补充。所有持续监测项目都有独立的每日 Star 快照；榜单或搜索失败不阻断已有项目采集。

## Trending 保留上榜证据，不代替 Star 快照

榜单记录保存在发现任务的 `details_json.trending.windows`，包括采集时间、页面地址、周期、原始名次和页面显示的 Star 数。`resolved_repository_ids` 保存名字对应的 GitHub 永久 ID。

榜单内同名项目只核验一次，多个渠道按永久 ID 去重。已在库项目保留首次来源，并追加后续发现渠道。网页来源筛选匹配任一历史渠道，不仅限首次来源；它不是“当前仍在榜上”的筛选。

上榜项目没有额外 Star 门槛，离榜后仍持续采集。日常任务同日已成功抓取并完成身份核验时跳过重复读取；直接执行 `discover --source github-trending` 可明确触发一次新读取。

官网没有找到公开的 Trending API，本模块低频读取公开 HTML，不携带账号 cookie 或 GitHub token。空页面、挑战页、解析不完整、403 和 429 都保留失败状态，不记成零个项目。认证与限流错误不会触发身份轮换。

页面总 Star、榜单窗口增量与我们每天通过仓库 API 采集的绝对 Star 分别保存。页面值不能补造历史快照，也不替代我们自己计算的日、周、月净变化。

## 新项目与长期热门项目

`config/discovery.example.yaml` 包含三种互补查询：

- 每日寻找近 30 天创建且达到 500 Star 的项目；AI Agent、编程 Agent 使用 30 Star 的较低门槛，LLM 项目用 100 Star / 14 天，便于发现较早期的项目。
- 每日寻找近期推送的高关注项目和已有 200 Star 的 AI Agent。推送时间仅表示代码活跃，不代表 Star 增长。
- 每周覆盖高 Star 项目、细分 Agent topic，以及按 owner 限定的成熟标杆项目。

搜索命中会直接入库并开始追踪。页面上的“新发现”表示首次进入本系统；项目的 GitHub 创建时间另行保存，二者不能混为一谈。Star 排序是绝对关注量排序，增长必须来自不同日期的真实快照。

GitHub Search 每个查询最多提供 1,000 个结果。系统按 Star 或创建/推送日期拆分宽查询；无法继续拆分时保留 `truncated`，GitHub 超时返回的 `incomplete_results` 也会记录到采集历史，不能宣称已全量覆盖。搜索结果与仓库详情用 GitHub 永久 repository ID 去重。

GitHub Core 与 Search 使用不同限额。默认串行请求并读取响应限额、reset、Retry-After；遇到限流等待，达到重试上限就记录错误，不轮换身份或绕过限制。配置阈值应匹配账户实际额度和每日监测项目数量。

官方依据：[Repository Search API](https://docs.github.com/en/rest/search/search#search-repositories)、[仓库搜索限定符](https://docs.github.com/en/search-github/searching-on-github/searching-for-repositories)、[REST API 限额](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api)、[API 最佳实践](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api)。

## 大类和保守分类

大类使用编程 Agent、研究 Agent、浏览器与电脑操作 Agent、工作流 Agent、Agent 构建与协作平台、通用 Agent、AI 基础设施。组件标签如 Skills、Memory、Harness 位于基础设施下；多 Agent、工作台、远程客户端位于平台下。旧 slug 和项目映射保留，父子结构调整不会删除历史映射。

自动分类只采用 GitHub topics、仓库名称和描述中的明确证据。常见通用词如 `skills`、`agent`、`browser-automation` 单独出现时不作 Agent 分类。题库和精选列表不会被误作 Agent 产品。无法确定的项目保留未分类，且允许一个项目属于多个明确大类。

自动结果为未确认分类，保留置信度；人工添加和人工移除（持久否决）的标签均受保护。已有错误历史映射不会被批量删除，可使用 `topic remove --repo owner/name --topic skills` 人工否决。

为既有项目补充大类，可离线执行：

```sh
go run ./cmd/github-radar --db /absolute/path/github-radar.db --json topic reclassify --dry-run
go run ./cmd/github-radar --db /absolute/path/github-radar.db topic reclassify
```

`--dry-run` 输出候选类别、理由、已有映射和人工保护状态，不写数据库、不联网。正式执行只补充自动映射。由于早期数据库未保存所有原始 GitHub topics，离线处理以已存名称、描述和已有 GitHub 标签为依据，覆盖率可能低于下一次在线发现；不会为填满分类而猜测。
