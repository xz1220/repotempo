# 使用 GitHub 账号管理 RepoTempo

设置 `GITHUB_RADAR_PUBLIC_SIGNUP=1` 后，任意 GitHub 账号均可登录。
管理员 ID 名单只决定采集历史和新项目入库权限；普通用户可以管理已收录项目的个人关注、备注和 Agent 访问密钥。
关注和备注按不可变 GitHub 用户 ID 隔离。项目不再记录已读或未读状态。

## 注册登录应用

在 [GitHub OAuth App 注册页](https://github.com/settings/applications/new) 创建独立的登录应用。
这里只使用公开身份，不申请私有仓库、邮箱或代码读写权限；采集任务继续使用原来的独立 GitHub 凭据。

填写以下内容：

- Application name 填写 `RepoTempo`。
- Homepage URL 填写 `https://github-radar.43-160-242-46.sslip.io`。
- Authorization callback URL 填写 `https://github-radar.43-160-242-46.sslip.io/auth/github/callback`。

保持回调地址精确匹配，不启用通配回调或设备授权。
应用创建后可取得 Client ID，再生成 Client Secret。不要把 Secret 放入聊天、代码、截图或日志。

## 在服务器设置四个值

使用安全的服务器编辑方式，将四个值一起写入现有环境配置。当前部署使用 `/etc/github-radar/github-radar.env`。
保留原来的采集令牌和数据盘路径，只增加下面四项；示例中的说明文字必须替换，不能直接用来上线。

```sh
GITHUB_RADAR_GITHUB_OAUTH_CLIENT_ID='替换为真实 Client ID'
GITHUB_RADAR_GITHUB_OAUTH_CLIENT_SECRET='替换为真实 Client Secret'
GITHUB_RADAR_PUBLIC_URL='https://github-radar.43-160-242-46.sslip.io'
GITHUB_RADAR_GITHUB_ADMIN_IDS='41764150'
GITHUB_RADAR_PUBLIC_SIGNUP=1
```

`41764150` 是 `xz1220` 的 GitHub 数值 ID。权限按这个稳定 ID 判断，不按可更改的用户名或邮箱判断。
其他部署应查询并填写自己的 GitHub ID；多个管理员 ID 使用英文逗号分隔。

四项全部未设置时，保留原有管理方式。只填写其中一部分时，程序拒绝启动，不会默默退回旧口令验证。
完整配置启用后，关注、备注和 API 密钥管理需要登录。导入任务、采集历史和新项目入库另外要求管理员权限；旧管理口令和本机入口不再绕过登录。未设置公开登录开关时仍只接受管理员登录。

配置文件应仅允许管理员和服务进程读取。完成配置后重启 `github-radar-web.service`，并从上面的主域名测试登录。
其他站点别名的登录入口会转到这个主域名；登录 Cookie 不在不同域名之间共享。

两个 HTTPS 站点都应给 `/auth/` 单独配置代理规则，不记录该路径的原始访问或错误日志。
授权回调地址含一次性 code 和 state，不能沿用记录完整请求地址的默认日志格式；配置示例见 `deploy/tencent2/nginx-server.conf.example`。

## 回退时保留访问限制

旧程序可以读取新增认证表后的数据库，但不认识 GitHub 登录配置，换回旧程序就会恢复原来的访问权限。
因此不能仅替换旧程序后直接重新公开站点；需要先关闭外部访问或启用额外的站点访问验证，再处理回退。业务数据不需要降级或覆盖。

## 登录后的数据边界

公开项目与趋势仍允许匿名浏览。匿名页面不显示其他用户的关注标记或备注。
现有公开研究简读继续保留；已经公开或人工改写进简读的内容，不会被自动重新判定为私人信息。

用户、个人项目状态、Agent 密钥和请求 nonce 使用独立表。会话凭据仅保存哈希，GitHub access token、refresh token 和 Client Secret 不写入数据库。Agent 的派生签名密钥属于敏感凭据，数据库与备份必须保持私有。
一次登录请求最多有效 10 分钟，本站会话有效 12 小时；退出登录撤销本站会话，不会退出 GitHub 本身。
同一来源 IP 每 10 分钟最多发起 10 次登录，超过时返回 429。站点只信任本机反向代理追加的真实来源地址；分布式攻击仍需代理层防护。

不要公开 SQLite 备份、原始 CSV/JSON 导出、环境文件或运行目录。
这些文件可能包含管理员备注、任务信息或认证状态，不等同于公开网页。

## 本地开发与验收

本地使用单独的测试应用或单独注册的回调地址，例如 `http://127.0.0.1:8878/auth/github/callback`。
`GITHUB_RADAR_PUBLIC_URL` 相应设为 `http://127.0.0.1:8878`。非本机地址必须使用 HTTPS。

验收时检查这些结果：

- 匿名用户可看公开项目；直接访问导入、历史或关注查询会要求登录。
- 管理员登录后回到原项目或筛选页面，不需要再次填写管理口令。
- 公开登录开启时，名单外账号能登录、管理个人数据，但不能读取运维记录或执行全局导入。
- 退出后，即使重放旧 Cookie，也不能再提交导入。
- 无效、过期、重复的回调及伪造退出请求均被拒绝。
- 原有项目、Star 历史和简读不变。旧关注与私有备注会一次性复制到第一个明确配置的管理员账号，来源记录保留。

模拟授权服务器的自动化测试不能替代真实登录验收。必须配置有效应用凭据，并由管理员实际完成一次 GitHub 授权。

配置依据见 [GitHub 官方授权流程](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps)和 [Go OAuth 库](https://pkg.go.dev/golang.org/x/oauth2)。
