# Agent 访问单页流程已部署

线上地址：https://github-radar.43-160-242-46.sslip.io

运行版本：`d4e7626`。服务：`github-radar-web.service`，数据库结构保持版本 9。

## 本次变化

- 账户入口统一为「Agent 访问」，取消访问密钥/Agent 接入页签。
- 同页生成 AK/SK，显示含当前凭据的安装命令，也可复制完整 Agent 指令。
- 默认安装一个 Skill，使用 sh、curl、OpenSSL、tar 与常见系统工具，无需 Node/Python；支持 macOS、Linux、WSL。
- 凭据只在创建响应中展示，下载 URL 不携带凭据，本地按 0600 保存。完成、关闭、离开时清除网页中的 SK 和两种指令。
- 删除已读/未读标记及其脚本、样式与旧测试；关注、研究简读、详细/紧凑视图和详情返回定位保留。
- 原 Node MCP/Plugin 包保留为可选兼容入口，不再进入默认设置流程。

本次用户批准的增量要求记录在 `.design/repotempo-approved-v2/APPROVED-CHANGES.md`；v1 冻结文件全部 SHA256 未变。

## 验证

- 最终代码 `make ci` 通过：格式、静态检查、Go 全包测试、71 项 JavaScript 测试、竞态检查和 Linux 构建。
- 服务器 Linux 上 Web 与 integration 测试通过。已修复 GNU awk 内建函数名冲突，并覆盖 GNU/BSD 元数据、macOS 继承 ACL。
- 在无 Node、Python、jq 的 PATH 中完成安装与真实 Go HMAC/API 调用，覆盖身份、搜索、Unicode 筛选、分页、项目详情、个人关注、撤销、权限、重定向拒绝及安全重装。
- 浏览器测试使用隔离账户，从网页刚生成的命令实际安装 Skill，再通过 API 验证该账户；完成后网页不再包含 SK。
- 单页/弹窗/关闭流程与 360–1920px 十档宽度检查通过；桌面和手机截图已检查。敏感文本控件的 value、defaultValue、textContent 清除有回归测试。
- 线上 readiness 200；新安装脚本和 Skill 压缩包 200；无签名 API 401；旧 reading-state.js 404。线上列表无已读控件与旧脚本。
- 生产数据副本导出完整 4,543 个项目，返回 200，最终候选版本检查约 4.3 秒；未修改生产数据。

本次没有新建生产测试密钥，也没有代用户进行 GitHub 授权。完整生成/安装/API 回归使用隔离的真实 Go 服务和测试账户。

## 备份与恢复

程序：`/home/xingzheng/data/github-radar/releases/20260911-agent-access-d4e7626/repotempo`。
切换前备份：`/home/xingzheng/data/github-radar/deployment-backups/20260911-agent-access-d4e7626`。
前一程序：`/home/xingzheng/data/github-radar/releases/20260910-multiuser-3fd43ac/repotempo`。

升级脚本在现有 daily.lock 下备份数据库与环境，再切换程序；项目、观测、简读与分类关系校验一致。未执行数据库结构迁移。旧程序与备份保留，不删除任何生产记录。
