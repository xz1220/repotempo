# Star 采集故障修复

修复已上线，9 月 15 日补采已完成：5,460 个监控目标全部处理，5,428 项 Star 成功（99.41%），32 项因 GitHub 不可访问而失败。Star 排序、组合筛选和导出验证通过。

## 根因与修复

9 月 9 日至 14 日，09:15 的采集任务每天都启动，但同一个仓库 `1537271403/paypal-agreement-protocol`（GitHub ID `1305340248`）返回 `403 Repository access blocked`。原代码将所有 403 都判为全局访问故障，写下该仓库失败后立即终止全批。9 月 9–13 日每天只成功 17 项，9 月 14 日只成功 20/5,460 项。

直接只读核查 GitHub 响应确认该仓库因 `tos` 被限制，而账户还有 4,992 次核心 API 额度。修复在 GitHub 客户端新增 `repository_access_blocked` 错误分类；快照服务记录该仓库失败并继续。未知权限 403、401、真正的主/次级限流、429 仍停止批次，保留请求退避与额度重置等待。限流信号优先于仓库封禁消息。

失败记录的 Star 值仍为 NULL，不改变用户的关注/备注，不删除或擅自停止该项目。之后同日重跑跳过已有成功快照，允许失败记录被真实成功响应修复。

## 验证证据

- 新增真实 HTTP 回归先复现原实现中断，再验证封禁后第二个仓库仍被采集，以及同日重跑能修复失败、跳过成功。
- 覆盖封禁响应有/无额度头、泛权限 403、主限流、次级限流、Retry-After、401 和 429。
- 新增 `TestStarCollectionFeedsNumericSortingFiltersAndExports`，完整经过 GitHub HTTP 响应、Runtime Snapshot、SQLite、项目库查询、Go 模板和 CSV。验证数字 Star 排序、增长排序、标签与搜索组合、分页、当天项目、真实零值、旧快照和精确 1/7/30 天增长。
- 全量 `make ci` 通过：格式、Go vet、Go 测试、83 项 JS 测试、race 检查和 Linux amd64 构建。
- 服务器使用生产数据库副本验证 9 月 8 日、9 月 14 日，分别按 1/7/30 天及 Stars/增长排序，共 12 组全量 CSV；每一行的顺序、Star 和增长值均与独立 SQL 计算一致，测试未修改生产库。

## 发布

用户明确授权修复、验证后上线。运行版本 `bc5d02feab0c6b62e3566de02c954ae1c1349ab8`，由干净保存版本构建（`vcs.modified=false`），SHA256：

```text
5c4ff623bc36a21473e9d7ba8e4540fa7228a09633d72d72b7381719d616d08f
```

仅切换程序，数据库结构仍为 9，原环境文件字节不变；Go 模板、原生 JS/CSS、OAuth、账户隔离、CSRF 和 v1 冻结稿均未改动。切换在现有 `daily.lock` 内执行，备份后对公共数据和个人数据逐表摘要核对一致，readiness 正常。

- 程序：`/home/xingzheng/data/github-radar/releases/20260915-star-recovery-bc5d02f/repotempo`
- 备份：`/home/xingzheng/data/github-radar/deployment-backups/20260915-star-recovery-bc5d02f`
- 前一程序：`/home/xingzheng/data/github-radar/releases/20260913-user-audit-bdef6bf/repotempo`
- 回退只切程序，不用旧数据库覆盖采集后产生的新记录。

## 生产补采

9 月 15 日 00:38:40（北京时间）启动一次性 `repotempo-star-recovery-20260915.service`，使用原账户配置、原采集锁和新程序的 `--json snapshot`，不重复发现项目。原 09:15 计划继续执行新程序；临时服务不新增定时任务。

补采于 01:21:09 完成，运行约 42 分 28 秒。任务 ID 为 `73ecef00-b11b-4b6d-bf64-44aafad78b70`；临时服务已结束，Web 服务仍 active，readiness 正常且无异常重启。

| 指标 | 结果 |
| --- | --- |
| 全库项目 / 本次监控目标 | 5,472 / 5,460（12 项原为暂停） |
| Star 成功 / 失败 / 未处理 | 5,428 / 32 / 0 |
| Star 失败分类 | 31 项 404（不存在或私有）；1 项 `repository_access_blocked` |
| 元数据更新告警 | 2 项 SQLite 锁冲突，Star 快照成功保留 |
| 可比较的 1 天 / 7 天项目 | 20 / 4,419 |
| 完成时 GitHub 核心 API 剩余额度 | 4,528 |

作业状态为 `partial`，包括 32 项 Star 失败和 2 项元数据告警，作业的失败操作数为 34，不代表有 34 个项目缺少 Star。两项元数据告警分别为 `Agent-Field/SWE-AF`（994 Stars）与 `nanocoai/nanoclaw`（30,762 Stars）；这两项最新 Star 均写入成功。其描述、标签、ETag/检查时间的更新遇到 `SQLITE_BUSY`，本轮保留旧元数据，没有为修正告警而改写成功快照或个人状态。下一个日期的采集仍会尝试更新元数据；这项偶发元数据写冲突未在本次扩展修复。

补采后再次使用生产数据库副本独立验证 9 月 14 日和 15 日、1/7/30 天、Stars/增长排序，共 12 组，每组 5,472 行；全量顺序和 Star/增长数值与独立 SQL 完全一致。

线上真实浏览器验证：

- 今天全库按 Star 数排序，前几项为 `build-your-own-x` 547,204、`awesome` 505,984、`public-apis` 480,024；第二页 21–40 项继续按数字降序。
- `q=audio.cpp&tag=ai` 保留组合条件，只返回 1 项；列表、详情和 CSV 都为 2,704 Stars、7 天增长 356。详情同时显示 1 天增长 24、30 天增长 1,053。
- 详情返回保留日期、周期、排序、搜索和标签，390px 页面截图无重叠或横向溢出。
- 数据库 `integrity_check=ok`。与发布前备份做双向逐行差集，9 月 15 日之前的历史快照、简读、用户、个人关注/备注、密钥及账户迁移记录均为 0 差异。

源码与测试已同步至 GitHub。本机 CI 日志和截图保存在本任务的 `repotempo-audit/star-recovery-20260915` 验收产物目录。

9 月 9–14 日没有成功采集的历史日期继续为空。今天采集不能还原过去的逐日 Star 值，因此不能伪造每日增长；各周期只比较两个准确日期都有的成功快照。
