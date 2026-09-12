# 2026-09-12 用户视角问题修复

10 项问题均已修复，最终代码 `fe69cdf` 已通过完整 CI 和真实浏览器验收，具备上线条件，尚未部署生产。

修复基于线上版本 `86e05b4700e4223210cd80f0221f025e0284fc52`。新工作副本最初基于旧默认版本，开始编辑前已校正。原项目目录未改动；本次没有部署或对生产账户执行写操作。

## 修复范围

| 问题 | 修复后的行为 | 自动回归 |
| --- | --- | --- |
| 1. 重复添加清除关注和备注 | 添加操作只补充空白个人状态，保留已有关注和非空备注；详情提供“编辑我的备注”，普通用户和管理员均可明确编辑、清空；管理员重复导入也保留备注 | `TestPersonalWatchDuplicateAddPreservesSQLiteState`、`TestPersonalWatchExplicitEditCanClearButCannotRetarget`、`TestPersonalWatchAddFillsEmptyStateAndKeepsExistingNote`、`TestAdministratorRepeatedImportPreservesPersonalState` |
| 2. 多页面关注 409 | 每页拥有独立的一次性 nonce；已登录请求绑定原登录会话，另一页的 Cookie 不会使旧页面失效；无 OAuth 模式使用稳定浏览器绑定 | `TestPersonalWatchFormsSurviveOtherPagesAndKeepOneTimeSessionBinding`、`watch_multitab_test.go` |
| 3. 搜索加标签丢条件 | `q` 始终显示并提交，`tag` 独立显示和提交；标签模式的输入框明确为标签范围内搜索；各条件可单独清除 | `TestLibraryCombinedSearchAndTagSurviveRenderedFormSubmission`、JavaScript 搜索提交回归 |
| 4. 清除筛选不完整 | 清除搜索、标签、旧分类/来源/状态及分页，恢复 1 天和 Star 排序；保留观察日期、语言、当前页签、每页数量；浏览全库是独立动作 | `TestLibraryClearFiltersRestoresDefaultsWithinTheCurrentView`，含无 `view` 的原始链接 |
| 5. 排序选项消失 | 九种受支持的排序始终可选，切换后可返回原排序 | `TestLibrarySortOptionsRemainAvailableAfterChangingOrder` |
| 6. Agent 表单错误后重置 | 保留自定义名称、有效期和所有未勾选权限，展开设置并聚焦错误；无效有效期要求重新选择；成功响应才显示一次 SK | `TestAgentCreateErrorPreservesSettingsWithoutGrantingScopes`、`TestAgentCreateErrorsPreserveOnlySubmittedChoices`、现有密钥隔离/一次性测试 |
| 7. 搜索文字压图标 | 搜索控件的 32px 左内边距不再被全局输入样式覆盖 | 实际浏览器几何检查 |
| 8. 英文窄屏页签裁切 | 页签容器可横向滚动，文字和计数保持完整 | 实际浏览器 320/360px 检查 |
| 9. 手机导航焦点错误 | 关闭时侧栏不可聚焦，打开时焦点进入主导航、背景不可交互；Tab 循环、Esc/关闭回焦点、桌面切换和 Agent 对话框协作 | `scripts/mobile-navigation.test.mjs` |
| 10. 主要导航丢语言 | 品牌、趋势、项目库及相关内部入口保留语言；详情返回链接保留筛选和定位片段 | `TestShellNavigationKeepsSelectedLocale`、详情返回回归 |

## 安全和设计差异

沿用 Go 模板、原生 JavaScript 和本地 CSS，没有数据库结构迁移。GitHub OAuth、账户隔离、管理员权限、来源校验和一次性提交保护继续生效。个人状态合并在 SQLite 事务中完成，显式编辑只作用于表单指定的同一仓库。

必要的交互差异：显示组合筛选条件、清除操作恢复默认周期/排序、排序列表完整可选、错误时展开权限设置、窄屏页签可滚动，以及导航的隐藏与焦点管理。详情增加个人备注编辑入口；自己的备注也会在已有共享研究简读时显示。管理员“重新导入”保留独立导入语义。Agent 访问仍为已批准的单页流程。v1 冻结目录未编辑，manifest 中五项 SHA256 校验一致。

## 验证记录

新增回归已先在原实现上确认失败，再验证修复。使用隔离的 Go HTTP + SQLite 测试和本地浏览器服务，测试账户和种子数据不连接生产身份服务或数据库。

浏览器修复前证据：320px 英文页签容器宽 286px、内容宽 339px，最后一项右边界约 356px，`overflow-x: visible`；390px 搜索文字起点 45px，小于图标右边界 58px；关闭导航后反向 Tab 落到屏外 Star 链接，展开后 Tab 落到背景 Export。

最终代码执行 `make ci`，退出码 0：格式检查、`go vet ./...`、全量 Go 测试、83 项 JavaScript 测试、全量 `go test -race ./...`、Linux amd64 构建均通过。临时浏览器 fixture 默认跳过，不会让 CI 启动服务。未新增必须安装的运行依赖。

真实浏览器使用 Ego Lite 的独立任务空间访问 loopback fixture，覆盖中文、英文、桌面和 320/360/390/768px。实际截图已检查，宽度测量如下：

| 视口宽度 | 页面 scrollWidth | 输入文字起点 / 图标右边界 | 页签 clientWidth / scrollWidth | 结果 |
| --- | --- | --- | --- | --- |
| 320 | 320 | 65 / 58 | 286 / 342 | 可水平滚动 56px，最后一项完整显示，右边界约 300px |
| 360 | 360 | 65 / 58 | 326 / 342 | 可水平滚动 |
| 390 | 390 | 65 / 58 | 356 / 356 | 中英文均无重叠 |
| 768 | 768 | 73 / 66 | 366 / 366 | 菜单和列表无溢出 |
| 1440 | 1440 | 289 / 282 | 366 / 366 | 桌面布局通过 |

浏览器中的实际操作结果：

- 从“添加项目”输入已有 `fixture/audio.cpp`，不勾关注、备注留空，提交后仍是 `focus=true` 和原备注。
- 先打开列表 A，再打开添加页 B，A 取消关注成功；B 的独立表单随后提交也成功，备注保留。详情真实编辑入口能清空自己的关注/备注，账户 202 的关注及备注不变。
- `q=audio.cpp` 点击 `ai` 标签后，两个条件可见；改 7 天仍为 1 条结果。`delta → stars` 后仍有九种排序选项，清除后回到 `1d + stars`、28 条结果，badge 和清除按钮隐藏。
- 原始 `new=1&tag=python` 旧链接清除后仍在当天页签，显示 4 条当天记录；ArrowRight 正常进入全部项目。
- 列表、详情和 CSV 一致：`audio.cpp` 为 3,100 Stars，7 天增长 196。分页第 1 页 20 条、第 2 页 8 条，末页无下一页；详情返回保留搜索、标签、周期、排序。
- 768px 打开菜单后聚焦 Trends，连续 8 次 Tab 都留在菜单；Esc 和背景点击关闭后焦点回汉堡按钮。切至 1024px 后侧栏与主内容解除 inert，桌面 Tab 正常。
- 390px 手机菜单内打开 Agent 对话框，焦点进入弹窗。自定义名称、30 天、两权限均未勾选的错误响应保持设置并展开选项，错误获得焦点；Tab 留在弹窗，Esc 关闭后回账户按钮，再次 Esc 回汉堡按钮。
- 测试账户创建 30 天、仅 `repositories:read` 的密钥，单次响应有 SK 和安装指令；关闭后 DOM 中秘密字段为 0。
- 英文页面实际点击 Trends、默认 GitHub projects、品牌、添加页和详情页返回链接，目标均保持 `lang=en`；默认项目库仍进入当天新入库。

本机截图与完整 CI 日志保存在 `/Users/danielxing/.codex/visualizations/2026/09/12/01a09587-8b48-7e91-bd6c-3a73bc94bbe3/repotempo-audit/`，包含修复前后截图。截图没有生产数据或生成的 SK。

## 复跑方式与发布边界

```sh
make ci
REPOTEMPO_BROWSER_RUNTIME_FIXTURE=1 go test ./internal/app -run '^TestBrowserRuntimeFixture$' -count=1 -v
```

fixture 仅监听 `127.0.0.1:18879`，在临时 SQLite 中生成 28 个测试项目和三类测试身份。打开 `/fixture/sign-in` 登录普通用户 101，`?user=202` 为另一用户，`?user=42` 为管理员。它不访问生产数据、真实 OAuth 或 GitHub。测试服务有 30 分钟上限，验收结束后已主动关闭。

未修复项：本次确认的 10 项中没有遗留。浏览器验收基于 Chromium，没有声称覆盖所有浏览器引擎。部署重启仍会使内存中的旧一次性表单失效，已打开页面需要刷新；本次修复解决同一次服务运行中正常多开页面导致的失效。没有数据库结构迁移，没有生产部署或线上写入。
