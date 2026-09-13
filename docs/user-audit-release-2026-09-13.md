# 用户视角问题修复已部署

2026-09-13 18:06:37（北京时间），经用户授权将 10 项修复发布至 [RepoTempo](https://github-radar.43-160-242-46.sslip.io/repositories?lang=zh-CN)。运行版本为 `bdef6bf2c2dbdfb06ed1ae1a7899357fa7124cb5`，取代 `86e05b4`。

修复内容与完整回归见[修复验收记录](user-audit-fixes-2026-09-12.md)。程序从干净的保存版本重新构建，`vcs.modified=false`；Go 模板和静态资源随二进制发布，冻结 v1 文件未改动。

## 发布和数据保留

- 仅切换现有程序软链接；保留环境文件的全部字节，不改 Nginx、服务定义、采集计划或公开注册设置。
- 在 `daily.lock` 下停止 Web、生成原生 SQLite 备份、切换程序、恢复服务并验证 readiness；失败处理只切回原程序，不恢复旧数据库覆盖新写入。
- 数据库结构保持版本 9，无迁移。切换前没有进行中的导入任务。
- 4,600 个项目、43,607 条观测、1,515 份简读，以及分类、用户、个人关注/备注、Agent 密钥、旧工作区归属记录的逐表内容摘要一致。分类仅排除启动会刷新的 `topics.updated_at`。
- 数据库完整性为 `ok`，外键检查结果与切换前一致；备份目录权限 `0700`，数据库备份权限 `0600`。

运行程序 SHA256：

```text
e5394358f17cf2a8d5e0a49d9cc90d61f67f52f6b0a21a26af931ec2b0f9c6bf
```

已核对服务器实际运行进程的 `/proc/<pid>/exe`，摘要与本地候选程序一致。

## 上线验证

- 服务器上的隔离数据库副本完整导出 4,600 个项目，HTTP 200，耗时 1.219 秒；该验证没有修改生产库。
- 公网 `/healthz`、`/readyz`、CSS、JS 和 Skill 安装脚本均为 200；服务 `active/running`，无自动重启或近期 warning 日志。
- 无签名 `/api/v1/me` 为 401；匿名访问 Agent 页面和添加页为 303 登录跳转，语言继续保留。
- 真实浏览器操作 `audio.cpp + ai`，改为 7 天后仍保留两项条件和 1 条结果；九种排序始终可选。清除后恢复 1 天、Star 排序，badge 和清除按钮隐藏。
- 英文 Trends 导航保持 `lang=en`。320px 页面无横向溢出，搜索文字起点 65px、图标右边界 58px，页签容器可滚动。
- 手机菜单展开聚焦 Trends，Tab 进入 GitHub projects，背景不可交互；Esc 回到菜单按钮，关闭侧栏不可聚焦。

线上交互核查仅使用公开浏览和读取接口，没有修改生产关注、备注、密钥或进行 OAuth 授权。账户写入和单次秘密显示的完整回归已在发布前使用隔离账户通过。

## 版本与回退位置

- 当前程序：`/home/xingzheng/data/github-radar/releases/20260913-user-audit-bdef6bf/repotempo`
- 源码快照和本次仅切换程序的升级脚本：同目录下 `source.tar.gz`、`upgrade-binary.py`
- 切换前备份：`/home/xingzheng/data/github-radar/deployment-backups/20260913-user-audit-bdef6bf`
- 前一程序：`/home/xingzheng/data/github-radar/releases/20260911-sidebar-86e05b4/repotempo`
- 服务：`github-radar-web.service`

需要回退时仅恢复上述旧程序指向并重启服务，保留当前数据库。服务重启前已打开的一次性提交表单需要刷新；账户和密钥格式没有变化。
