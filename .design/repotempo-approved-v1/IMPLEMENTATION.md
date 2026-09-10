# 在 github-radar 中落地

## 先把基准放进仓库

在解压目录执行：

```sh
python3 github-radar-ui-handoff/install.py ~/Projects/github-radar --dry-run
python3 github-radar-ui-handoff/install.py ~/Projects/github-radar
python3 github-radar-ui-handoff/install.py ~/Projects/github-radar --verify
```

安装只新增 `.design/repotempo-approved-v1/`，并向根部 `DESIGN.md`、`AGENTS.md` 追加基准指针。既有正文保留；已有不同版本的同名基准会拒绝覆盖。此操作不会迁移运行代码，也不会自动提交、推送或部署。

## 真实前端的落点

| 内容 | 当前仓库文件 |
| --- | --- |
| 主题变量 | `internal/web/static/tokens.css` |
| 侧栏、顶栏、公共外壳 | `internal/web/templates/base.gohtml`、`internal/web/static/app.css` |
| 项目列表、过滤和分页 | `internal/web/templates/repositories.gohtml`、`internal/web/static/feed.css`、`internal/web/static/app.js` |
| 添加、详情、趋势页的共用组件 | `internal/web/templates/watch.gohtml`、`repository.gohtml`、`home.gohtml` |
| 登录入口与菜单 | `internal/web/templates/base.gohtml`、`login.gohtml`、`internal/web/static/auth.css`、`auth.js` |
| 服务端列表与分页上下文 | `internal/web/library.go`、`feed.go`、`models.go`、`handlers.go` |
| 真实会话与授权 | `internal/web/auth.go`、`internal/app/auth.go`、`internal/service/auth/`、`internal/store/sqlite/auth.go` |

## 建议执行顺序

1. 保存设计基准和根部指针，作为独立的可回退保存点。
2. 在隔离的开发线中迁移主题、公共外壳和项目库。保留现有 Go 模板和原生脚本技术栈，把原型结构转换成现有模板的数据绑定。
3. 对接现有分页、标签、日期、关注、已读、添加与 OAuth；不要把整个 HTML 当成生产首页替换进去。
4. 依照 `DESIGN-LOCK.md` 做对照验收。记录明确的必要偏差，避免“顺手优化”改变布局。
5. AK/SK 与 JSON API 单独开发。检查账户权限模型、数据可见范围、密钥保存方式、签名规范、时钟窗口、防重放及撤销后再开启入口。
6. 测试通过后保存并同步，在用户指定的发布流程中上线。

## 现有检查命令

以下是读取仓库 Makefile 后记录的命令；迁移时先确认它们没有变化。

```sh
make test
make test-js
make ci
```

本交接包未修改生产代码，也没有宣称上述生产检查已通过。

## 不要覆盖的业务事实

- `PRODUCT.md` 的匿名可见性、管理员白名单、私有笔记与关注权限。
- 今日入库视图应使用真实上海日期，空日期不能悄悄换成有数据的日期。
- Star 增长来自真实端点快照；项目首次入库时间不是 GitHub 创建时间。
- 页码式控件若与现有游标接口不匹配，明确实现后端适配或报告差异，不能伪造总页数。
- 原型里对 GitHub API 的浏览器直连只用于演示，生产应遵循现有采集与数据读取路径。
