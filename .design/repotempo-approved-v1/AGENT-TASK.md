# 给仓库内 Agent 的任务

请在 github-radar 仓库中落地已确认的 RepoTempo 前端。

先阅读根部 AGENTS.md、PRODUCT.md、DESIGN.md，以及 `.design/repotempo-approved-v1/DESIGN-LOCK.md` 和 `IMPLEMENTATION.md`。以 `.design/repotempo-approved-v1/repotempo-approved.html` 为外观与交互基准，先校验 manifest 中的 SHA256，禁止重新设计或修改冻结稿。

当前目标是把已批准 UI 接进现有 Go 模板、CSS 和原生 JavaScript，保留真实路由、数据和业务行为。不切换前端框架，不把静态 HTML 直接当成生产应用。重点迁移浅色侧栏、顶栏右侧操作、头像入口、纯文字 Star 链接、项目列表列宽、弱化标签、折叠筛选和分页控件。

保留现有 GitHub OAuth、白名单、CSRF 和服务端数据可见性。演示密码登录、内存账户、演示 AK/SK 和静态快照不能进入生产。AK/SK 与 `/api/v1/*` 尚待真实后端实现，单独列为后续任务；不得为了完成 UI 而假装已经可以鉴权调用。尚未开放的功能在生产界面中明确表达状态。

开始前检查仓库当前状态及适用的 AGENTS.md。优先建立隔离开发线，保留现有修改。按组件迁移，运行合适的 Go 和 JavaScript 检查，并按 DESIGN-LOCK.md 覆盖宽度和交互状态。使用一致的测试数据做实际页面对照；已有测试通过不等于视觉验收通过。

交付时报告修改文件、生产数据连接情况、验证结果、任何必要的视觉偏差，以及仍未实现的真实服务能力。冻结文件不变，用户未确认的布局不修改。未经明确发布授权不要部署。
