package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type viewMode int

const (
	listView viewMode = iota
	addView
	editView
	detailView
	deleteConfirmView
)

type model struct {
	tunnelManager *TunnelManager
	mode          viewMode
	cursor        int
	tunnelList    []string
	width         int
	height        int

	// Add/Edit form fields
	formFields    map[string]string
	formCursor    int
	formFieldKeys []string

	// Detail view
	selectedTunnelID string

	// Delete confirmation
	deleteTarget string
	deleteName   string

	// Error/success messages
	message     string
	messageType string // "error" or "success"
}

var (
	// Styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	listStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			Padding(1, 2)

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EE6FF8")).
			Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF00")).
			Bold(true)

	formStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			Padding(1, 2)
)

func initialModel() (model, error) {
	tm, err := NewTunnelManager()
	if err != nil {
		return model{}, err
	}

	m := model{
		tunnelManager: tm,
		mode:          listView,
		formFields:    make(map[string]string),
		formFieldKeys: []string{
			"name", "type", "local_port", "remote_host", "remote_port",
			"ssh_host", "ssh_port", "ssh_user", "ssh_key_path", "description",
			"background", "no_command", "gateway_ports", "compression", "quiet",
			"keep_alive", "server_alive_interval",
			"auto_reconnect", "reconnect_interval", "max_reconnect_attempts", "reconnect_backoff",
		},
	}

	m.updateTunnelList()
	return m, nil
}

func (m *model) updateTunnelList() {
	tunnels := m.tunnelManager.GetTunnels()
	m.tunnelList = make([]string, 0, len(tunnels))

	for id := range tunnels {
		m.tunnelList = append(m.tunnelList, id)
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case listView:
			return m.updateListView(msg)
		case addView, editView:
			return m.updateFormView(msg)
		case detailView:
			return m.updateDetailView(msg)
		case deleteConfirmView:
			return m.updateDeleteConfirmView(msg)
		}
	}

	return m, nil
}

func (m model) updateListView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.tunnelManager.Close()
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < len(m.tunnelList)-1 {
			m.cursor++
		}

	case "a":
		m.mode = addView
		m.formCursor = 0
		m.formFields = make(map[string]string)
		m.formFields["type"] = "local"
		m.formFields["ssh_port"] = "22"
		m.formFields["background"] = "false"
		m.formFields["no_command"] = "true"
		m.formFields["gateway_ports"] = "false"
		m.formFields["compression"] = "false"
		m.formFields["quiet"] = "false"
		m.formFields["keep_alive"] = "true"
		m.formFields["server_alive_interval"] = "60"
		m.formFields["auto_reconnect"] = "true"
		m.formFields["reconnect_interval"] = "10"
		m.formFields["max_reconnect_attempts"] = "0"
		m.formFields["reconnect_backoff"] = "true"
		m.message = ""

	case "d":
		if len(m.tunnelList) > 0 && m.cursor < len(m.tunnelList) {
			id := m.tunnelList[m.cursor]
			tunnel, exists := m.tunnelManager.GetTunnel(id)
			if exists {
				m.deleteTarget = id
				m.deleteName = tunnel.Config.Name
				m.mode = deleteConfirmView
				m.message = ""
			}
		}

	case "enter", " ":
		if len(m.tunnelList) > 0 && m.cursor < len(m.tunnelList) {
			m.selectedTunnelID = m.tunnelList[m.cursor]
			m.mode = detailView
		}

	case "s":
		if len(m.tunnelList) > 0 && m.cursor < len(m.tunnelList) {
			id := m.tunnelList[m.cursor]
			tunnel, exists := m.tunnelManager.GetTunnel(id)
			if exists {
				if tunnel.Status == StatusRunning {
					if err := m.tunnelManager.StopTunnel(id); err != nil {
						m.message = err.Error()
						m.messageType = "error"
					} else {
						m.message = "隧道已停止"
						m.messageType = "success"
					}
				} else {
					if err := m.tunnelManager.StartTunnel(id); err != nil {
						m.message = err.Error()
						m.messageType = "error"
					} else {
						m.message = "隧道已启动"
						m.messageType = "success"
					}
				}
			}
		}

	case "e":
		if len(m.tunnelList) > 0 && m.cursor < len(m.tunnelList) {
			id := m.tunnelList[m.cursor]
			tunnel, exists := m.tunnelManager.GetTunnel(id)
			if exists {
				m.mode = editView
				m.selectedTunnelID = id
				m.formCursor = 0
				m.loadTunnelToForm(tunnel.Config)
				m.message = ""
			}
		}
	}

	return m, nil
}

func (m model) updateFormView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.tunnelManager.Close()
		return m, tea.Quit

	case "esc":
		m.mode = listView
		m.message = ""

	case "up", "k":
		if m.formCursor > 0 {
			m.formCursor--
		}

	case "down", "j":
		if m.formCursor < len(m.formFieldKeys)-1 {
			m.formCursor++
		}

	case "enter":
		if m.mode == addView {
			config, err := m.createTunnelConfig()
			if err != nil {
				m.message = err.Error()
				m.messageType = "error"
			} else {
				if err := m.tunnelManager.AddTunnel(config); err != nil {
					m.message = err.Error()
					m.messageType = "error"
				} else {
					m.message = "隧道添加成功"
					m.messageType = "success"
					m.mode = listView
					m.updateTunnelList()
				}
			}
		} else if m.mode == editView {
			config, err := m.createTunnelConfig()
			if err != nil {
				m.message = err.Error()
				m.messageType = "error"
			} else {
				config.ID = m.selectedTunnelID
				if err := m.tunnelManager.RemoveTunnel(m.selectedTunnelID); err != nil {
					m.message = err.Error()
					m.messageType = "error"
				} else if err := m.tunnelManager.AddTunnel(config); err != nil {
					m.message = err.Error()
					m.messageType = "error"
				} else {
					m.message = "隧道更新成功"
					m.messageType = "success"
					m.mode = listView
					m.updateTunnelList()
				}
			}
		}

	case "backspace":
		fieldKey := m.formFieldKeys[m.formCursor]
		if len(m.formFields[fieldKey]) > 0 {
			m.formFields[fieldKey] = m.formFields[fieldKey][:len(m.formFields[fieldKey])-1]
		}

	default:
		if len(msg.String()) == 1 {
			fieldKey := m.formFieldKeys[m.formCursor]
			if m.formFields[fieldKey] == "" {
				m.formFields[fieldKey] = msg.String()
			} else {
				m.formFields[fieldKey] += msg.String()
			}
		}
	}

	return m, nil
}

func (m model) updateDeleteConfirmView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.tunnelManager.Close()
		return m, tea.Quit

	case "y", "Y", "enter":
		// 确认删除
		if err := m.tunnelManager.RemoveTunnel(m.deleteTarget); err != nil {
			m.message = err.Error()
			m.messageType = "error"
		} else {
			m.message = "隧道删除成功"
			m.messageType = "success"
			m.updateTunnelList()
			if m.cursor >= len(m.tunnelList) && len(m.tunnelList) > 0 {
				m.cursor = len(m.tunnelList) - 1
			}
		}
		m.mode = listView
		m.deleteTarget = ""
		m.deleteName = ""

	case "n", "N", "esc":
		// 取消删除
		m.mode = listView
		m.deleteTarget = ""
		m.deleteName = ""
		m.message = ""
	}

	return m, nil
}

func (m model) updateDetailView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.tunnelManager.Close()
		return m, tea.Quit

	case "esc", "q":
		m.mode = listView

	case "s":
		tunnel, exists := m.tunnelManager.GetTunnel(m.selectedTunnelID)
		if exists {
			if tunnel.Status == StatusRunning {
				if err := m.tunnelManager.StopTunnel(m.selectedTunnelID); err != nil {
					m.message = err.Error()
					m.messageType = "error"
				} else {
					m.message = "隧道已停止"
					m.messageType = "success"
				}
			} else {
				if err := m.tunnelManager.StartTunnel(m.selectedTunnelID); err != nil {
					m.message = err.Error()
					m.messageType = "error"
				} else {
					m.message = "隧道已启动"
					m.messageType = "success"
				}
			}
		}
	}

	return m, nil
}

func (m *model) loadTunnelToForm(config TunnelConfig) {
	m.formFields["name"] = config.Name
	m.formFields["type"] = string(config.Type)
	m.formFields["local_port"] = strconv.Itoa(config.LocalPort)
	m.formFields["remote_host"] = config.RemoteHost
	m.formFields["remote_port"] = strconv.Itoa(config.RemotePort)
	m.formFields["ssh_host"] = config.SSHHost
	m.formFields["ssh_port"] = strconv.Itoa(config.SSHPort)
	m.formFields["ssh_user"] = config.SSHUser
	m.formFields["ssh_key_path"] = config.SSHKeyPath
	m.formFields["description"] = config.Description

	// SSH参数
	m.formFields["background"] = strconv.FormatBool(config.Background)
	m.formFields["no_command"] = strconv.FormatBool(config.NoCommand)
	m.formFields["gateway_ports"] = strconv.FormatBool(config.GatewayPorts)
	m.formFields["compression"] = strconv.FormatBool(config.Compression)
	m.formFields["quiet"] = strconv.FormatBool(config.Quiet)
	m.formFields["keep_alive"] = strconv.FormatBool(config.KeepAlive)
	m.formFields["server_alive_interval"] = strconv.Itoa(config.ServerAliveInterval)

	// 自动重连参数
	m.formFields["auto_reconnect"] = strconv.FormatBool(config.AutoReconnect)
	m.formFields["reconnect_interval"] = strconv.Itoa(config.ReconnectInterval)
	m.formFields["max_reconnect_attempts"] = strconv.Itoa(config.MaxReconnectAttempts)
	m.formFields["reconnect_backoff"] = strconv.FormatBool(config.ReconnectBackoff)
}

func (m model) createTunnelConfig() (TunnelConfig, error) {
	config := TunnelConfig{}

	config.Name = m.formFields["name"]
	if config.Name == "" {
		return config, fmt.Errorf("名称不能为空")
	}

	tunnelType := m.formFields["type"]
	if tunnelType == "local" {
		config.Type = LocalTunnel
	} else if tunnelType == "remote" {
		config.Type = RemoteTunnel
	} else if tunnelType == "dynamic" {
		config.Type = DynamicTunnel
	} else {
		return config, fmt.Errorf("类型必须是 'local'、'remote' 或 'dynamic'")
	}

	localPort, err := strconv.Atoi(m.formFields["local_port"])
	if err != nil || localPort <= 0 || localPort > 65535 {
		return config, fmt.Errorf("本地端口必须是有效的端口号 (1-65535)")
	}
	config.LocalPort = localPort

	// 对于动态隧道，远程主机和端口不是必需的
	if config.Type != DynamicTunnel {
		config.RemoteHost = m.formFields["remote_host"]
		if config.RemoteHost == "" {
			return config, fmt.Errorf("远程主机不能为空")
		}

		remotePort, err := strconv.Atoi(m.formFields["remote_port"])
		if err != nil || remotePort <= 0 || remotePort > 65535 {
			return config, fmt.Errorf("远程端口必须是有效的端口号 (1-65535)")
		}
		config.RemotePort = remotePort
	}

	config.SSHHost = m.formFields["ssh_host"]
	if config.SSHHost == "" {
		return config, fmt.Errorf("SSH主机不能为空")
	}

	sshPort, err := strconv.Atoi(m.formFields["ssh_port"])
	if err != nil || sshPort <= 0 || sshPort > 65535 {
		return config, fmt.Errorf("SSH端口必须是有效的端口号 (1-65535)")
	}
	config.SSHPort = sshPort

	config.SSHUser = m.formFields["ssh_user"]
	if config.SSHUser == "" {
		return config, fmt.Errorf("SSH用户不能为空")
	}

	config.SSHKeyPath = m.formFields["ssh_key_path"]
	config.Description = m.formFields["description"]

	// 解析SSH参数
	if background, err := strconv.ParseBool(m.formFields["background"]); err == nil {
		config.Background = background
	}
	if noCommand, err := strconv.ParseBool(m.formFields["no_command"]); err == nil {
		config.NoCommand = noCommand
	}
	if gatewayPorts, err := strconv.ParseBool(m.formFields["gateway_ports"]); err == nil {
		config.GatewayPorts = gatewayPorts
	}
	if compression, err := strconv.ParseBool(m.formFields["compression"]); err == nil {
		config.Compression = compression
	}
	if quiet, err := strconv.ParseBool(m.formFields["quiet"]); err == nil {
		config.Quiet = quiet
	}
	if keepAlive, err := strconv.ParseBool(m.formFields["keep_alive"]); err == nil {
		config.KeepAlive = keepAlive
	}
	if interval, err := strconv.Atoi(m.formFields["server_alive_interval"]); err == nil && interval > 0 {
		config.ServerAliveInterval = interval
	}

	// 解析自动重连参数
	if autoReconnect, err := strconv.ParseBool(m.formFields["auto_reconnect"]); err == nil {
		config.AutoReconnect = autoReconnect
	}
	if reconnectInterval, err := strconv.Atoi(m.formFields["reconnect_interval"]); err == nil && reconnectInterval > 0 {
		config.ReconnectInterval = reconnectInterval
	}
	if maxAttempts, err := strconv.Atoi(m.formFields["max_reconnect_attempts"]); err == nil && maxAttempts >= 0 {
		config.MaxReconnectAttempts = maxAttempts
	}
	if reconnectBackoff, err := strconv.ParseBool(m.formFields["reconnect_backoff"]); err == nil {
		config.ReconnectBackoff = reconnectBackoff
	}

	return config, nil
}

func (m model) View() string {
	switch m.mode {
	case listView:
		return m.listView()
	case addView:
		return m.formView("添加新隧道")
	case editView:
		return m.formView("编辑隧道")
	case detailView:
		return m.detailView()
	case deleteConfirmView:
		return m.deleteConfirmView()
	}
	return ""
}

func (m model) listView() string {
	title := titleStyle.Render("SSH隧道管理器")

	// 添加项目描述
	description := helpStyle.Render("一个功能完善的SSH隧道管理工具，支持本地、远程和动态端口转发，具备自动重连和智能监控功能")

	var content strings.Builder
	content.WriteString("隧道列表:\n\n")

	if len(m.tunnelList) == 0 {
		content.WriteString("暂无配置的隧道。按 'a' 添加新隧道。\n")
	} else {
		for i, id := range m.tunnelList {
			tunnel, exists := m.tunnelManager.GetTunnel(id)
			if !exists {
				continue
			}

			prefix := "  "
			if i == m.cursor {
				prefix = "▶ "
			}

			statusColor := ""
			switch tunnel.Status {
			case StatusRunning:
				statusColor = "🟢"
			case StatusStopped:
				statusColor = "🔴"
			case StatusError:
				statusColor = "🟡"
			}

			typeText := "本地"
			if tunnel.Config.Type == RemoteTunnel {
				typeText = "远程"
			} else if tunnel.Config.Type == DynamicTunnel {
				typeText = "动态"
			}

			var destination string
			if tunnel.Config.Type == DynamicTunnel {
				destination = fmt.Sprintf("SOCKS5:%d", tunnel.Config.LocalPort)
			} else {
				destination = fmt.Sprintf("%s:%d → %s:%d",
					"localhost", tunnel.Config.LocalPort,
					tunnel.Config.RemoteHost, tunnel.Config.RemotePort)
			}

			line := fmt.Sprintf("%s%s %s - %s (%s)",
				prefix,
				statusColor,
				tunnel.Config.Name,
				typeText,
				destination,
			)

			if i == m.cursor {
				line = selectedStyle.Render(line)
			}

			content.WriteString(line + "\n")
		}
	}

	help := helpStyle.Render("\n控制: ↑/↓ 导航 • 回车/空格 详情 • s 启动/停止 • a 添加 • e 编辑 • d 删除 • q 退出")

	if m.message != "" {
		var msgStyle lipgloss.Style
		if m.messageType == "error" {
			msgStyle = errorStyle
		} else {
			msgStyle = successStyle
		}
		help = msgStyle.Render("\n"+m.message) + help
	}

	body := listStyle.Render(content.String())

	return lipgloss.JoinVertical(lipgloss.Left, title, description, body, help)
}

func (m model) formView(formTitle string) string {
	title := titleStyle.Render(formTitle)

	var content strings.Builder

	fieldLabels := map[string]string{
		"name":                   "名称",
		"type":                   "类型 (local/remote/dynamic)",
		"local_port":             "本地端口",
		"remote_host":            "远程主机",
		"remote_port":            "远程端口",
		"ssh_host":               "SSH主机",
		"ssh_port":               "SSH端口",
		"ssh_user":               "SSH用户",
		"ssh_key_path":           "SSH密钥路径 (可选)",
		"description":            "描述 (可选)",
		"background":             "后台运行 -f (true/false)",
		"no_command":             "不执行远程命令 -N (true/false)",
		"gateway_ports":          "允许远程连接 -g (true/false)",
		"compression":            "启用压缩 -C (true/false)",
		"quiet":                  "安静模式 -q (true/false)",
		"keep_alive":             "保持连接活跃 (true/false)",
		"server_alive_interval":  "服务器活跃间隔 (秒)",
		"auto_reconnect":         "启用自动重连 (true/false)",
		"reconnect_interval":     "重连间隔 (秒)",
		"max_reconnect_attempts": "最大重连次数 (0=无限)",
		"reconnect_backoff":      "启用指数退避重连 (true/false)",
	}

	for i, key := range m.formFieldKeys {
		label := fieldLabels[key]
		value := m.formFields[key]

		prefix := "  "
		if i == m.formCursor {
			prefix = "▶ "
			label = selectedStyle.Render(label)
		}

		content.WriteString(fmt.Sprintf("%s%s: %s\n", prefix, label, value))
	}

	help := helpStyle.Render("\n控制: ↑/↓ 导航 • 输入文字 • 退格删除 • 回车保存 • Esc取消")

	if m.message != "" {
		var msgStyle lipgloss.Style
		if m.messageType == "error" {
			msgStyle = errorStyle
		} else {
			msgStyle = successStyle
		}
		help = msgStyle.Render("\n"+m.message) + help
	}

	body := formStyle.Render(content.String())

	return lipgloss.JoinVertical(lipgloss.Left, title, body, help)
}

func (m model) deleteConfirmView() string {
	title := titleStyle.Render("确认删除隧道")

	var content strings.Builder
	content.WriteString(fmt.Sprintf("您确定要删除隧道 '%s' 吗？\n\n", m.deleteName))
	content.WriteString("此操作无法撤销。\n\n")

	tunnel, exists := m.tunnelManager.GetTunnel(m.deleteTarget)
	if exists && tunnel.Status == StatusRunning {
		content.WriteString("⚠️  警告：该隧道当前正在运行中，删除后连接将立即断开。\n\n")
	}

	content.WriteString("请选择：\n")
	content.WriteString("  Y/Enter - 确认删除\n")
	content.WriteString("  N/Esc   - 取消删除\n")

	help := helpStyle.Render("\n控制: Y 确认 • N/Esc 取消")

	if m.message != "" {
		var msgStyle lipgloss.Style
		if m.messageType == "error" {
			msgStyle = errorStyle
		} else {
			msgStyle = successStyle
		}
		help = msgStyle.Render("\n"+m.message) + help
	}

	body := formStyle.Render(content.String())

	return lipgloss.JoinVertical(lipgloss.Left, title, body, help)
}

func (m model) detailView() string {
	tunnel, exists := m.tunnelManager.GetTunnel(m.selectedTunnelID)
	if !exists {
		return "隧道未找到"
	}

	title := titleStyle.Render("隧道详情: " + tunnel.Config.Name)

	var content strings.Builder

	config := tunnel.Config

	content.WriteString(fmt.Sprintf("ID: %s\n", config.ID))
	content.WriteString(fmt.Sprintf("名称: %s\n", config.Name))

	typeText := "本地转发"
	if config.Type == RemoteTunnel {
		typeText = "远程转发"
	} else if config.Type == DynamicTunnel {
		typeText = "动态转发 (SOCKS5)"
	}
	content.WriteString(fmt.Sprintf("类型: %s\n", typeText))

	statusText := "已停止"
	switch tunnel.Status {
	case StatusRunning:
		statusText = "运行中"
	case StatusError:
		statusText = "错误"
	}
	content.WriteString(fmt.Sprintf("状态: %s\n", statusText))
	content.WriteString(fmt.Sprintf("本地端口: %d\n", config.LocalPort))

	if config.Type != DynamicTunnel {
		content.WriteString(fmt.Sprintf("远程地址: %s:%d\n", config.RemoteHost, config.RemotePort))
	}
	content.WriteString(fmt.Sprintf("SSH: %s@%s:%d\n", config.SSHUser, config.SSHHost, config.SSHPort))

	if config.SSHKeyPath != "" {
		content.WriteString(fmt.Sprintf("SSH密钥: %s\n", config.SSHKeyPath))
	}

	// 显示SSH参数
	content.WriteString("\nSSH参数:\n")
	if config.Background {
		content.WriteString("  - 后台运行 (-f)\n")
	}
	if config.NoCommand {
		content.WriteString("  - 不执行远程命令 (-N)\n")
	}
	if config.GatewayPorts {
		content.WriteString("  - 允许远程连接 (-g)\n")
	}
	if config.Compression {
		content.WriteString("  - 启用压缩 (-C)\n")
	}
	if config.Quiet {
		content.WriteString("  - 安静模式 (-q)\n")
	}
	if config.KeepAlive {
		content.WriteString(fmt.Sprintf("  - 保持连接活跃 (间隔: %d秒)\n", config.ServerAliveInterval))
	}

	// 显示自动重连信息
	content.WriteString("\n自动重连:\n")
	if config.AutoReconnect {
		content.WriteString("  - 已启用\n")
		content.WriteString(fmt.Sprintf("  - 重连间隔: %d秒\n", config.ReconnectInterval))
		if config.MaxReconnectAttempts > 0 {
			content.WriteString(fmt.Sprintf("  - 最大重连次数: %d\n", config.MaxReconnectAttempts))
		} else {
			content.WriteString("  - 最大重连次数: 无限\n")
		}
		if config.ReconnectBackoff {
			content.WriteString("  - 启用指数退避\n")
		}
		if tunnel.ReconnectCount > 0 {
			content.WriteString(fmt.Sprintf("  - 已重连次数: %d\n", tunnel.ReconnectCount))
			if tunnel.LastReconnectTime != nil {
				content.WriteString(fmt.Sprintf("  - 最后重连: %s\n",
					tunnel.LastReconnectTime.Format("2006-01-02 15:04:05")))
			}
		}
	} else {
		content.WriteString("  - 已禁用\n")
	}

	if config.Description != "" {
		content.WriteString(fmt.Sprintf("\n描述: %s\n", config.Description))
	}

	if tunnel.StartTime != nil {
		content.WriteString(fmt.Sprintf("启动时间: %s\n", tunnel.StartTime.Format("2006-01-02 15:04:05")))
	}

	if tunnel.Error != "" {
		content.WriteString(fmt.Sprintf("\n错误: %s\n", errorStyle.Render(tunnel.Error)))
	}

	help := helpStyle.Render("\n控制: s 启动/停止 • q/Esc 返回")

	if m.message != "" {
		var msgStyle lipgloss.Style
		if m.messageType == "error" {
			msgStyle = errorStyle
		} else {
			msgStyle = successStyle
		}
		help = msgStyle.Render("\n"+m.message) + help
	}

	body := listStyle.Render(content.String())

	return lipgloss.JoinVertical(lipgloss.Left, title, body, help)
}
