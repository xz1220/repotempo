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
		"lead":        {"生成一组密钥，安装 Skill，让你的 Agent 访问项目与个人关注。", "Create a key pair and install the Skill to give your agent access to projects and your watchlist."},
		"close":       {"关闭 Agent 访问", "Close Agent access"},
		"created":     {"AK/SK 已生成", "AK/SK created"},
		"secret_once": {"下方指令已填入你的 AK/SK。SK 仅本次可见，请在关闭前完成安装或保存。", "The instructions below contain your AK/SK. The secret is shown once; install or save it before closing."},
		"view_keys":   {"查看 AK/SK", "View AK/SK"},
		"copy_ak":     {"复制 AK", "Copy AK"}, "copy_sk": {"复制 SK", "Copy SK"}, "show": {"显示", "Show"},
		"my_keys": {"管理已有密钥", "Manage existing keys"},
		"name":    {"名称", "Name"}, "default_name": {"我的 Agent", "My agent"},
		"options": {"权限与有效期", "Permissions and lifetime"},
		"expiry":  {"有效期", "Lifetime"}, "days": {"天", "days"}, "permissions": {"权限", "Permissions"},
		"public_read": {"读取公开项目与观测", "Read public projects and observations"},
		"watch_read":  {"读取我的关注", "Read my watchlist"},
		"readonly":    {"默认只读，有效期 90 天，可随时撤销。", "Read-only by default, valid for 90 days, revocable at any time."},
		"generate":    {"生成 AK/SK", "Generate AK/SK"},
		"revoked":     {"已撤销", "Revoked"}, "expires": {"到期", "Expires"}, "created_on": {"创建于", "Created"},
		"revoke": {"撤销密钥", "Revoke key"}, "revoke_help": {"撤销后，使用这组密钥的 Agent 将无法继续访问。", "Revoking this key immediately stops agents using it from accessing the API."},
		"revoke_confirm":    {"确认撤销", "Confirm revocation"},
		"existing_help":     {"这里不会再次显示 SK。需要重新安装且未保存密钥时，请生成一组新密钥，并撤销旧密钥。", "Secrets cannot be shown again. To reinstall without a saved secret, create a new key pair and revoke the old one."},
		"install":           {"安装 Skill，开始使用", "Install the Skill and get started"},
		"install_help":      {"在本地终端运行指令，自动下载 Skill 并配置访问凭据。支持 macOS、Linux 和 WSL；使用 sh、curl、OpenSSL 和 tar，无需 Node 或 Python。", "Run the command in your local terminal to download the Skill and configure access. Supports macOS, Linux and WSL with sh, curl, OpenSSL and tar; no Node or Python required."},
		"generate_first":    {"生成 AK/SK 后，这里会出现你的专属安装指令。", "Your personal installation command will appear here after you generate AK/SK."},
		"terminal_command":  {"本地安装指令", "Local installation command"},
		"copy_install":      {"复制安装指令", "Copy installation command"},
		"sensitive_command": {"指令包含 SK，请仅在自己的设备或可信 Agent 中使用，不要公开分享或提交到代码仓库；终端和聊天可能保留历史。", "This command contains your secret. Use it only on your device or with a trusted agent. Do not share or commit it; terminals and chats may retain history."},
		"prompt":            {"也可以让 Agent 帮你安装", "Or ask your agent to install it"},
		"prompt_help":       {"复制包含当前密钥的完整指令，交给有本地终端权限的可信 Agent 即可。", "Copy the complete instructions with your current key pair to a trusted agent that can use your local terminal."},
		"prompt_before":     {"请安装 RepoTempo Skill 并验证连接。以下指令已包含我的专属 AK/SK，请在本地终端执行，不要在回复、日志或代码仓库中回显或保存这些凭据：", "Install the RepoTempo Skill and verify the connection. Run the following command locally; it contains my AK/SK. Do not echo or save the credentials in replies, logs, or repositories:"},
		"prompt_after":      {"安装后读取 ~/.agents/skills/repotempo/SKILL.md，使用其中的工具验证当前账户，再按我的要求查询项目、关注、研究简读和 Star 历史。缺失数据保留为空，更多结果使用真实分页；不要修改关注或其他账户数据。", "After installation, read ~/.agents/skills/repotempo/SKILL.md and use its helper to verify the account, then query projects, my watchlist, briefs, and Star history as requested. Preserve missing values, follow real pagination, and do not modify account data or watchlists."},
		"copy_prompt":       {"复制 Agent 指令", "Copy agent instructions"},
		"after_install":     {"安装完成后，在 Agent 中开启新对话，告诉它「用 RepoTempo 帮我找项目」。", "After installing, start a new agent conversation and ask it to find projects using RepoTempo."},
		"finish":            {"已完成安装，清除本页密钥", "Installation complete — clear this page's secret"},
		"copied":            {"已复制", "Copied"}, "copy_failed": {"复制失败，请选择文本后手动复制。", "Copy failed. Select the text and copy it manually."},
		"request_failed": {"操作未完成，请刷新后查看当前状态。", "The request did not complete. Refresh to check its status."},
	} {
		messageCatalog[localeChinese]["agent."+key] = pair[0]
		messageCatalog[localeEnglish]["agent."+key] = pair[1]
	}
}
