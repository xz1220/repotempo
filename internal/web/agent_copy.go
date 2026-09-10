package web

func init() {
	for key, pair := range map[string][2]string{
		"personal_add_help":  {"输入已收录项目的 GitHub 地址，保存到你的个人关注。新项目入库由管理员处理。", "Enter an existing project's GitHub URL to save it to your watchlist. Administrators manage new catalogue imports."},
		"personal_note_help": {"关注与备注仅属于你的账户，其他用户看不到。", "Your watchlist and notes belong to your account and are not visible to other users."},
		"save_watch":         {"保存关注与备注", "Save watchlist and note"},
	} {
		messageCatalog[localeChinese]["account."+key] = pair[0]
		messageCatalog[localeEnglish]["account."+key] = pair[1]
	}
	for key, pair := range map[string][2]string{
		"account_settings": {"账户 / 开发者设置", "Account / Developer settings"},
		"lead":             {"让你的 Agent 读取项目、研究简介与 Star 观测记录。", "Let your agent read projects, research briefs, and Star observations."},
		"close":            {"关闭 API 访问", "Close API access"},
		"keys":             {"访问密钥", "Access keys"}, "connect": {"Agent 接入", "Connect an agent"},
		"created":     {"密钥已生成，请保存 SK", "Key created. Save your secret."},
		"secret_once": {"SK 仅在这次创建时显示，关闭后无法再次查看。请保存到你的凭据管理器。", "The secret is shown only after creation. Save it in your credential manager before closing."},
		"copy_ak":     {"复制 AK", "Copy AK"}, "copy_sk": {"复制 SK", "Copy SK"}, "show": {"显示", "Show"},
		"saved":   {"我已保存，查看接入指令", "Saved. Show connection instructions"},
		"my_keys": {"我的密钥", "My keys"}, "create": {"创建密钥", "Create key"},
		"name": {"密钥名称", "Key name"}, "name_hint": {"例如：研究 Agent", "For example: Research agent"},
		"expiry": {"有效期", "Lifetime"}, "days": {"天", "days"}, "permissions": {"权限", "Permissions"},
		"public_read": {"读取公开项目与观测", "Read public projects and observations"},
		"watch_read":  {"读取我的关注", "Read my watchlist"},
		"readonly":    {"密钥仅有读取权限。为每个 Agent 创建独立密钥，可以分别撤销。", "Keys are read-only. Separate keys let you revoke each agent independently."},
		"cancel":      {"取消", "Cancel"}, "generate": {"生成 AK/SK", "Generate AK/SK"},
		"revoked": {"已撤销", "Revoked"}, "expires": {"到期", "Expires"}, "created_on": {"创建于", "Created"},
		"revoke": {"撤销密钥", "Revoke key"}, "revoke_help": {"撤销后，该密钥的新请求会立即被拒绝。", "New requests using this key will be rejected immediately."},
		"revoke_confirm": {"确认撤销", "Confirm revocation"}, "empty": {"还没有访问密钥", "No access keys yet"},
		"empty_help": {"创建一把只读密钥，连接你的研究 Agent。", "Create a read-only key to connect your research agent."},
		"download":   {"下载接入包", "Download the integration"}, "download_bundle": {"下载 MCP、Skill 与 Plugin", "Download MCP, Skill and Plugin"},
		"download_help":  {"包含 MCP 本地连接器、命令行工具、Skill 和 Codex Plugin，使用 Node.js 运行。", "Includes a local MCP connector, CLI, Skill, and Codex Plugin. Runs with Node.js."},
		"configure":      {"在 Agent 的运行环境中配置凭证", "Configure credentials in your agent environment"},
		"configure_help": {"从账户中创建 AK/SK，SK 留在本地用于签名。不要把它粘贴到聊天中。", "Create an AK/SK pair here. Keep the secret locally for signing; do not paste it into chat."},
		"your_ak":        {"你创建的 AK", "your access key"}, "your_sk": {"创建时保存的 SK", "the secret saved at creation"},
		"env_help":          {"通过系统凭据或受保护的环境文件提供这些值；不要保存到代码仓库。", "Supply these through protected environment settings; never commit credentials."},
		"install":           {"选择 MCP、Skill 或 Plugin", "Choose MCP, Skill, or Plugin"},
		"install_help":      {"MCP 客户端以 stdio 启动下面的命令，路径替换为解压位置。", "Configure your MCP client to run this stdio command, using your extracted path."},
		"skill_plugin_help": {"Skill 和 Plugin 的安装命令见接入包 README；三种方式使用同一组账号权限。", "See the bundle README for Skill and Plugin installation. All three use the same account permissions."},
		"prompt":            {"交给 Agent 的指令", "Instructions for your agent"},
		"prompt_text":       {"使用 RepoTempo 的 MCP 工具或配套 Skill 查询开源项目。先验证当前连接身份，再按我的要求检索项目、我的关注、研究简读和 Star 历史。需要更多结果时继续真实分页。缺少观测时保留缺失，不把未知值写成零；不把 Star 增长解释为投资建议。凭据从运行环境读取，不要要求我在聊天中发送 SK。", "Use the RepoTempo MCP tools or bundled Skill to research open-source projects. Check the connected identity, then query projects, my watchlist, saved briefs, and Star history. Follow real pagination when more results are needed. Preserve missing observations; never treat unknown as zero or Star growth as investment advice. Read credentials from the runtime environment, never from chat."},
		"copy_prompt":       {"复制 Agent 指令", "Copy agent instructions"}, "protocol": {"接口与签名", "Endpoints and signing"},
		"protocol_help": {"配套连接器自动签名。每个请求校验有效期、只读权限、时间戳与唯一 nonce，详细格式见接入包。", "The connector signs requests and the server checks expiry, read scope, timestamp, and unique nonce. The bundle documents the exact format."},
		"copied":        {"已复制", "Copied"}, "copy_failed": {"复制失败，请选择文本后手动复制。", "Copy failed. Select the text and copy it manually."},
		"request_failed": {"操作未完成，请刷新后查看当前状态。", "The request did not complete. Refresh to check its status."},
	} {
		messageCatalog[localeChinese]["agent."+key] = pair[0]
		messageCatalog[localeEnglish]["agent."+key] = pair[1]
	}
}
