"""
SSH 端口转发配置管理工具
基于 pywebview 的图形化界面
"""

import atexit
import json
import subprocess
from pathlib import Path

import webview


class API:
    """JavaScript API 类，用于 Python 和前端通信"""

    def __init__(self):
        self.config_file = Path("ssh_tunnels.json")
        self.active_processes = {}
        self.load_configs()

    def load_configs(self):
        """加载配置文件"""
        if self.config_file.exists():
            try:
                with open(self.config_file, "r", encoding="utf-8") as f:
                    return json.load(f)
            except Exception:
                return []
        return []

    def save_configs(self, configs):
        """保存配置文件"""
        try:
            with open(self.config_file, "w", encoding="utf-8") as f:
                json.dump(configs, f, ensure_ascii=False, indent=2)
            return {"success": True}
        except Exception as e:
            return {"success": False, "error": str(e)}

    def get_configs(self):
        """获取所有配置"""
        return self.load_configs()

    def add_config(self, config):
        """添加新配置"""
        configs = self.load_configs()
        config["id"] = len(configs) + 1
        config["status"] = "stopped"
        configs.append(config)
        result = self.save_configs(configs)
        if result.get("success"):
            return {"success": True, "config": config}
        return result

    def update_config(self, config):
        """更新配置"""
        configs = self.load_configs()
        for i, c in enumerate(configs):
            if c.get("id") == config.get("id"):
                # 保留状态
                config["status"] = c.get("status", "stopped")
                configs[i] = config
                result = self.save_configs(configs)
                if result.get("success"):
                    return {"success": True, "config": config}
                return result
        return {"success": False, "error": "配置不存在"}

    def get_config(self, config_id):
        """获取单个配置"""
        configs = self.load_configs()
        config = next((c for c in configs if c.get("id") == config_id), None)
        if config:
            return {"success": True, "config": config}
        return {"success": False, "error": "配置不存在"}

    def delete_config(self, config_id):
        """删除配置"""
        configs = self.load_configs()
        configs = [c for c in configs if c.get("id") != config_id]
        return self.save_configs(configs)

    def start_tunnel(self, config_id):
        """启动 SSH 隧道"""
        configs = self.load_configs()
        config = next((c for c in configs if c.get("id") == config_id), None)

        if not config:
            return {"success": False, "error": "配置不存在"}

        try:
            # 构建 SSH 命令 - 修复参数分割问题
            tunnel_type = config.get("tunnel_type", "local")
            ssh_cmd = ["ssh", "-N", "-f"]

            # 添加端口转发参数
            if tunnel_type == "local":
                ssh_cmd.extend([
                    "-L",
                    f"{config['local_port']}:localhost:{config['remote_port']}",
                ])
            elif tunnel_type == "remote":
                ssh_cmd.extend([
                    "-R",
                    f"{config['remote_port']}:localhost:{config['local_port']}",
                ])
            else:  # dynamic
                ssh_cmd.extend(["-D", str(config["local_port"])])

            # 添加主机和端口
            ssh_cmd.extend([
                "-p",
                str(config.get("port", 22)),
                f"{config['username']}@{config['host']}",
            ])

            # 添加密钥文件或密码认证
            auth_type = config.get("auth_type", "password")
            if auth_type == "key" and config.get("key_path"):
                ssh_cmd.extend(["-i", config["key_path"]])

            cmd_str = " ".join(ssh_cmd)
            print(f"执行命令: {cmd_str}")

            # 启动 SSH 进程
            process = subprocess.Popen(ssh_cmd)
            self.active_processes[config_id] = process

            # 更新配置状态
            for c in configs:
                if c.get("id") == config_id:
                    c["status"] = "running"
            self.save_configs(configs)

            return {
                "success": True,
                "message": f"SSH 隧道已启动\n命令: {cmd_str}",
            }
        except Exception as e:
            return {"success": False, "error": str(e)}

    def stop_tunnel(self, config_id):
        """停止 SSH 隧道"""
        configs = self.load_configs()

        # 终止进程
        if config_id in self.active_processes:
            try:
                self.active_processes[config_id].terminate()
                del self.active_processes[config_id]
            except Exception:
                pass

        # 更新状态
        for c in configs:
            if c.get("id") == config_id:
                c["status"] = "stopped"
        self.save_configs(configs)

        return {"success": True, "message": "SSH 隧道已停止"}

    def stop_all_tunnels(self):
        """停止所有 SSH 隧道"""
        print("正在停止所有 SSH 隧道...")

        # 终止所有活动进程
        for config_id, process in list(self.active_processes.items()):
            try:
                process.terminate()
                print(f"已停止隧道 ID: {config_id}")
            except Exception as e:
                print(f"停止隧道 {config_id} 时出错: {e}")

        self.active_processes.clear()

        # 更新所有配置状态为停止
        configs = self.load_configs()
        for c in configs:
            c["status"] = "stopped"
        self.save_configs(configs)

        print("所有隧道已停止")


# HTML 内容
HTML_CONTENT = """
<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>SSH 端口转发配置</title>
    <style>
        * {
            box-sizing: border-box;
        }

        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI',
                         'Microsoft YaHei', Arial, sans-serif;
            margin: 0;
            padding: 0;
            background: linear-gradient(135deg, #1e3c72 0%, #2a5298 100%);
            min-height: 100vh;
        }

        .container {
            max-width: 1200px;
            margin: 20px auto;
            background: white;
            border-radius: 12px;
            box-shadow: 0 10px 30px rgba(0, 0, 0, 0.3);
            overflow: hidden;
        }

        .header {
            background: linear-gradient(135deg, #1e3c72 0%, #2a5298 100%);
            color: white;
            padding: 25px;
            text-align: center;
        }

        .header h1 {
            margin: 0;
            font-size: 28px;
            font-weight: 600;
        }

        .header p {
            margin: 8px 0 0 0;
            opacity: 0.9;
            font-size: 14px;
        }

        .content {
            display: grid;
            grid-template-columns: 1fr 1.5fr;
            gap: 20px;
            padding: 20px;
        }

        .form-section, .list-section {
            background: #f8f9fa;
            padding: 20px;
            border-radius: 8px;
        }

        .section-title {
            font-size: 18px;
            font-weight: 600;
            margin-bottom: 15px;
            color: #1e3c72;
            border-bottom: 2px solid #2a5298;
            padding-bottom: 8px;
        }

        .form-group {
            margin-bottom: 15px;
        }

        .form-group label {
            display: block;
            margin-bottom: 5px;
            font-weight: 500;
            color: #495057;
            font-size: 14px;
        }

        .form-group input, .form-group select {
            width: 100%;
            padding: 10px;
            border: 2px solid #dee2e6;
            border-radius: 6px;
            font-size: 14px;
            transition: border-color 0.3s;
        }

        .form-group input:focus, .form-group select:focus {
            outline: none;
            border-color: #2a5298;
        }

        .btn {
            padding: 10px 20px;
            border: none;
            border-radius: 6px;
            cursor: pointer;
            font-size: 14px;
            font-weight: 600;
            transition: all 0.3s;
        }

        .btn-primary {
            background: linear-gradient(135deg, #1e3c72 0%, #2a5298 100%);
            color: white;
            width: 100%;
        }

        .btn-primary:hover {
            transform: translateY(-2px);
            box-shadow: 0 4px 12px rgba(30, 60, 114, 0.4);
        }

        .btn-success {
            background: #28a745;
            color: white;
        }

        .btn-success:hover {
            background: #218838;
        }

        .btn-danger {
            background: #dc3545;
            color: white;
        }

        .btn-danger:hover {
            background: #c82333;
        }

        .btn-warning {
            background: #ffc107;
            color: #212529;
        }

        .btn-warning:hover {
            background: #e0a800;
        }

        .btn-info {
            background: #17a2b8;
            color: white;
        }

        .btn-info:hover {
            background: #138496;
        }

        .btn-secondary {
            background: #6c757d;
            color: white;
        }

        .btn-secondary:hover {
            background: #5a6268;
        }

        .btn:disabled {
            opacity: 0.5;
            cursor: not-allowed;
        }

        .btn:disabled:hover {
            transform: none;
            box-shadow: none;
        }

        .config-list {
            max-height: 500px;
            overflow-y: auto;
        }

        /* 模态框样式 */
        .modal {
            display: none;
            position: fixed;
            z-index: 2000;
            left: 0;
            top: 0;
            width: 100%;
            height: 100%;
            background-color: rgba(0, 0, 0, 0.5);
            animation: fadeIn 0.3s;
        }

        .modal.show {
            display: flex;
            align-items: center;
            justify-content: center;
        }

        .modal-content {
            background: white;
            border-radius: 12px;
            padding: 25px;
            max-width: 600px;
            width: 90%;
            max-height: 80vh;
            overflow-y: auto;
            animation: slideUp 0.3s;
            box-shadow: 0 10px 40px rgba(0, 0, 0, 0.3);
        }

        .modal-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 20px;
            padding-bottom: 15px;
            border-bottom: 2px solid #dee2e6;
        }

        .modal-title {
            font-size: 20px;
            font-weight: 600;
            color: #1e3c72;
        }

        .modal-close {
            background: none;
            border: none;
            font-size: 28px;
            cursor: pointer;
            color: #6c757d;
            line-height: 1;
            padding: 0;
            width: 32px;
            height: 32px;
        }

        .modal-close:hover {
            color: #dc3545;
        }

        .modal-body {
            margin-bottom: 20px;
        }

        .detail-group {
            margin-bottom: 15px;
            padding: 12px;
            background: #f8f9fa;
            border-radius: 6px;
        }

        .detail-label {
            font-weight: 600;
            color: #495057;
            margin-bottom: 5px;
            font-size: 13px;
        }

        .detail-value {
            color: #212529;
            font-size: 14px;
            word-break: break-all;
        }

        .button-group {
            display: flex;
            gap: 10px;
            margin-top: 15px;
        }

        .button-group .btn {
            flex: 1;
        }

        @keyframes fadeIn {
            from {
                opacity: 0;
            }
            to {
                opacity: 1;
            }
        }

        @keyframes slideUp {
            from {
                transform: translateY(50px);
                opacity: 0;
            }
            to {
                transform: translateY(0);
                opacity: 1;
            }
        }

        .config-item {
            background: white;
            padding: 15px;
            margin-bottom: 10px;
            border-radius: 8px;
            border: 2px solid #dee2e6;
            transition: all 0.3s;
        }

        .config-item:hover {
            border-color: #2a5298;
            box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
        }

        .config-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 10px;
        }

        .config-name {
            font-weight: 600;
            font-size: 16px;
            color: #1e3c72;
        }

        .config-status {
            padding: 4px 12px;
            border-radius: 12px;
            font-size: 12px;
            font-weight: 600;
        }

        .status-running {
            background: #d4edda;
            color: #155724;
        }

        .status-stopped {
            background: #f8d7da;
            color: #721c24;
        }

        .config-details {
            font-size: 13px;
            color: #6c757d;
            margin-bottom: 10px;
        }

        .config-details div {
            margin: 3px 0;
        }

        .config-actions {
            display: flex;
            gap: 8px;
        }

        .config-actions .btn {
            flex: 1;
            padding: 6px 12px;
            font-size: 12px;
        }

        .empty-state {
            text-align: center;
            padding: 40px;
            color: #6c757d;
        }

        .empty-state svg {
            width: 64px;
            height: 64px;
            margin-bottom: 10px;
            opacity: 0.5;
        }

        .toast {
            position: fixed;
            top: 20px;
            right: 20px;
            padding: 15px 20px;
            border-radius: 8px;
            color: white;
            font-weight: 500;
            z-index: 1000;
            animation: slideIn 0.3s ease-out;
        }

        .toast-success {
            background: #28a745;
        }

        .toast-error {
            background: #dc3545;
        }

        @keyframes slideIn {
            from {
                transform: translateX(400px);
                opacity: 0;
            }
            to {
                transform: translateX(0);
                opacity: 1;
            }
        }

        @keyframes slideOut {
            to {
                transform: translateX(400px);
                opacity: 0;
            }
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🔐 SSH 端口转发配置管理</h1>
            <p>轻松管理你的 SSH 隧道和端口转发</p>
        </div>

        <div class="content">
            <div class="form-section">
                <div class="section-title" id="formTitle">➕ 添加新配置</div>
                <form id="configForm">
                    <input type="hidden" id="config_id">
                    
                    <div class="form-group">
                        <label for="tunnel_name">隧道名称</label>
                        <input type="text" id="tunnel_name" placeholder="例如: MySQL-Tunnel" required>
                    </div>
                    
                    <div class="form-group">
                        <label for="name">配置名称</label>
                        <input type="text" id="name" placeholder="例如: 生产服务器" required>
                    </div>
                    
                    <div class="form-group">
                        <label for="host">服务器地址</label>
                        <input type="text" id="host" placeholder="例如: 192.168.1.100" required>
                    </div>
                    
                    <div class="form-group">
                        <label for="port">SSH 端口</label>
                        <input type="number" id="port" value="22" required>
                    </div>
                    
                    <div class="form-group">
                        <label for="username">用户名</label>
                        <input type="text" id="username" placeholder="SSH 用户名" required>
                    </div>
                    
                    <div class="form-group">
                        <label for="auth_type">认证方式</label>
                        <select id="auth_type">
                            <option value="password">密码认证</option>
                            <option value="key">密钥认证</option>
                        </select>
                    </div>
                    
                    <div class="form-group" id="password_group">
                        <label for="password">密码</label>
                        <input type="password" id="password" placeholder="SSH 密码">
                    </div>
                    
                    <div class="form-group" id="key_path_group" style="display: none;">
                        <label for="key_path">密钥文件路径</label>
                        <input type="text" id="key_path" placeholder="例如: ~/.ssh/id_rsa">
                    </div>
                    
                    <div class="form-group">
                        <label for="tunnel_type">转发类型</label>
                        <select id="tunnel_type">
                            <option value="local">本地转发 (-L)</option>
                            <option value="remote">远程转发 (-R)</option>
                            <option value="dynamic">动态转发 (-D)</option>
                        </select>
                    </div>
                    
                    <div class="form-group">
                        <label for="local_port">本地端口</label>
                        <input type="number" id="local_port" placeholder="例如: 8080" required>
                    </div>
                    
                    <div class="form-group" id="remote_port_group">
                        <label for="remote_port">远程端口</label>
                        <input type="number" id="remote_port" placeholder="例如: 3306">
                    </div>
                    
                    <div class="button-group">
                        <button type="submit" class="btn btn-primary" id="submitBtn">💾 保存配置</button>
                        <button type="button" class="btn btn-secondary" id="cancelBtn" style="display: none;" onclick="cancelEdit()">✖️ 取消</button>
                    </div>
                </form>
            </div>

            <div class="list-section">
                <div class="section-title">📋 配置列表</div>
                <div class="config-list" id="configList">
                    <div class="empty-state">
                        <svg viewBox="0 0 24 24" fill="currentColor">
                            <path d="M19 3H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm0 16H5V5h14v14z"/>
                        </svg>
                        <p>暂无配置，请添加新的 SSH 隧道配置</p>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <!-- 详情模态框 -->
    <div class="modal" id="detailModal">
        <div class="modal-content">
            <div class="modal-header">
                <div class="modal-title">👁️ 配置详情</div>
                <button class="modal-close" onclick="closeDetailModal()">&times;</button>
            </div>
            <div class="modal-body" id="detailContent">
            </div>
        </div>
    </div>

    <script>
        // 初始化
        let configs = [];
        let editingConfigId = null;

        // 页面加载时获取配置
        window.addEventListener('pywebviewready', function() {
            loadConfigs();
        });

        // 监听认证方式变化
        document.getElementById('auth_type').addEventListener('change', function() {
            const passwordGroup = document.getElementById('password_group');
            const keyPathGroup = document.getElementById('key_path_group');
            
            if (this.value === 'key') {
                passwordGroup.style.display = 'none';
                keyPathGroup.style.display = 'block';
                document.getElementById('password').required = false;
                document.getElementById('key_path').required = true;
            } else {
                passwordGroup.style.display = 'block';
                keyPathGroup.style.display = 'none';
                document.getElementById('password').required = true;
                document.getElementById('key_path').required = false;
            }
        });

        // 监听转发类型变化
        document.getElementById('tunnel_type').addEventListener('change', function() {
            const remotePortGroup = document.getElementById('remote_port_group');
            if (this.value === 'dynamic') {
                remotePortGroup.style.display = 'none';
                document.getElementById('remote_port').required = false;
            } else {
                remotePortGroup.style.display = 'block';
                document.getElementById('remote_port').required = true;
            }
        });

        // 加载配置列表
        async function loadConfigs() {
            try {
                configs = await pywebview.api.get_configs();
                renderConfigs();
            } catch (error) {
                console.error('加载配置失败:', error);
                showToast('加载配置失败', 'error');
            }
        }

        // 渲染配置列表
        function renderConfigs() {
            const listEl = document.getElementById('configList');
            
            if (configs.length === 0) {
                listEl.innerHTML = `
                    <div class="empty-state">
                        <svg viewBox="0 0 24 24" fill="currentColor">
                            <path d="M19 3H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm0 16H5V5h14v14z"/>
                        </svg>
                        <p>暂无配置，请添加新的 SSH 隧道配置</p>
                    </div>
                `;
                return;
            }

            listEl.innerHTML = configs.map(config => {
                const isRunning = config.status === 'running';
                const statusClass = isRunning ? 'status-running' : 'status-stopped';
                const statusText = isRunning ? '运行中' : '已停止';
                
                const tunnelTypeText = {
                    'local': '本地转发',
                    'remote': '远程转发',
                    'dynamic': '动态转发'
                }[config.tunnel_type];

                const authInfo = config.auth_type === 'key' ? '🔑 密钥' : '🔒 密码';
                
                return `
                    <div class="config-item">
                        <div class="config-header">
                            <div class="config-name">
                                ${config.tunnel_name || config.name}
                            </div>
                            <div class="config-status ${statusClass}">${statusText}</div>
                        </div>
                        <div class="config-details">
                            <div>📝 ${config.name}</div>
                            <div>🖥️ ${config.username}@${config.host}:${config.port} (${authInfo})</div>
                            <div>🔄 ${tunnelTypeText}: ${config.local_port}${config.remote_port ? ' → ' + config.remote_port : ''}</div>
                        </div>
                        <div class="config-actions">
                            ${isRunning
                                ? `<button class="btn btn-warning" onclick="stopTunnel(${config.id})">⏸️ 停止</button>`
                                : `<button class="btn btn-success" onclick="startTunnel(${config.id})">▶️ 启动</button>`
                            }
                            <button class="btn btn-info" onclick="showDetail(${config.id})">👁️ 详情</button>
                            <button class="btn btn-primary" onclick="editConfig(${config.id})" ${isRunning ? 'disabled' : ''}>✏️ 编辑</button>
                            <button class="btn btn-danger" onclick="deleteConfig(${config.id})" ${isRunning ? 'disabled' : ''}>🗑️ 删除</button>
                        </div>
                    </div>
                `;
            }).join('');
        }

        // 表单提交
        document.getElementById('configForm').addEventListener('submit', async function(e) {
            e.preventDefault();
            
            const authType = document.getElementById('auth_type').value;
            const config = {
                tunnel_name: document.getElementById('tunnel_name').value,
                name: document.getElementById('name').value,
                host: document.getElementById('host').value,
                port: parseInt(document.getElementById('port').value),
                username: document.getElementById('username').value,
                auth_type: authType,
                password: authType === 'password' ? document.getElementById('password').value : null,
                key_path: authType === 'key' ? document.getElementById('key_path').value : null,
                tunnel_type: document.getElementById('tunnel_type').value,
                local_port: parseInt(document.getElementById('local_port').value),
                remote_port: document.getElementById('tunnel_type').value !== 'dynamic'
                    ? parseInt(document.getElementById('remote_port').value)
                    : null
            };

            try {
                let result;
                if (editingConfigId) {
                    // 编辑模式
                    config.id = editingConfigId;
                    result = await pywebview.api.update_config(config);
                    if (result.success) {
                        showToast('配置更新成功', 'success');
                        cancelEdit();
                        await loadConfigs();
                    } else {
                        showToast('更新失败: ' + result.error, 'error');
                    }
                } else {
                    // 添加模式
                    result = await pywebview.api.add_config(config);
                    if (result.success) {
                        showToast('配置添加成功', 'success');
                        this.reset();
                        await loadConfigs();
                    } else {
                        showToast('添加失败: ' + result.error, 'error');
                    }
                }
            } catch (error) {
                showToast('操作失败', 'error');
                console.error(error);
            }
        });

        // 编辑配置
        async function editConfig(id) {
            try {
                const result = await pywebview.api.get_config(id);
                if (result.success) {
                    const config = result.config;
                    editingConfigId = id;
                    
                    // 更新表单标题
                    document.getElementById('formTitle').textContent = '✏️ 编辑配置';
                    document.getElementById('submitBtn').textContent = '💾 更新配置';
                    document.getElementById('cancelBtn').style.display = 'block';
                    
                    // 填充表单数据
                    document.getElementById('config_id').value = config.id;
                    document.getElementById('tunnel_name').value = config.tunnel_name || '';
                    document.getElementById('name').value = config.name || '';
                    document.getElementById('host').value = config.host || '';
                    document.getElementById('port').value = config.port || 22;
                    document.getElementById('username').value = config.username || '';
                    document.getElementById('auth_type').value = config.auth_type || 'password';
                    document.getElementById('tunnel_type').value = config.tunnel_type || 'local';
                    document.getElementById('local_port').value = config.local_port || '';
                    document.getElementById('remote_port').value = config.remote_port || '';
                    
                    // 根据认证方式显示对应字段
                    if (config.auth_type === 'key') {
                        document.getElementById('key_path').value = config.key_path || '';
                        document.getElementById('password_group').style.display = 'none';
                        document.getElementById('key_path_group').style.display = 'block';
                    } else {
                        document.getElementById('password').value = config.password || '';
                        document.getElementById('password_group').style.display = 'block';
                        document.getElementById('key_path_group').style.display = 'none';
                    }
                    
                    // 根据转发类型显示远程端口字段
                    if (config.tunnel_type === 'dynamic') {
                        document.getElementById('remote_port_group').style.display = 'none';
                    } else {
                        document.getElementById('remote_port_group').style.display = 'block';
                    }
                    
                    // 滚动到表单顶部
                    document.querySelector('.form-section').scrollIntoView({ behavior: 'smooth' });
                } else {
                    showToast('获取配置失败: ' + result.error, 'error');
                }
            } catch (error) {
                showToast('获取配置失败', 'error');
                console.error(error);
            }
        }

        // 取消编辑
        function cancelEdit() {
            editingConfigId = null;
            document.getElementById('formTitle').textContent = '➕ 添加新配置';
            document.getElementById('submitBtn').textContent = '💾 保存配置';
            document.getElementById('cancelBtn').style.display = 'none';
            document.getElementById('configForm').reset();
            document.getElementById('config_id').value = '';
            
            // 重置认证方式和转发类型显示
            document.getElementById('password_group').style.display = 'block';
            document.getElementById('key_path_group').style.display = 'none';
            document.getElementById('remote_port_group').style.display = 'block';
        }

        // 显示配置详情
        async function showDetail(id) {
            try {
                const result = await pywebview.api.get_config(id);
                if (result.success) {
                    const config = result.config;
                    const isRunning = config.status === 'running';
                    const statusClass = isRunning ? 'status-running' : 'status-stopped';
                    const statusText = isRunning ? '运行中' : '已停止';
                    
                    const tunnelTypeText = {
                        'local': '本地转发 (-L)',
                        'remote': '远程转发 (-R)',
                        'dynamic': '动态转发 (-D)'
                    }[config.tunnel_type];
                    
                    const authTypeText = config.auth_type === 'key' ? '密钥认证' : '密码认证';
                    const authValue = config.auth_type === 'key'
                        ? (config.key_path || '未设置')
                        : '••••••••';
                    
                    const portMapping = config.tunnel_type === 'dynamic'
                        ? `本地端口: ${config.local_port} (SOCKS代理)`
                        : `${config.local_port} → ${config.remote_port}`;
                    
                    document.getElementById('detailContent').innerHTML = `
                        <div class="detail-group">
                            <div class="detail-label">隧道名称</div>
                            <div class="detail-value">${config.tunnel_name || '未设置'}</div>
                        </div>
                        <div class="detail-group">
                            <div class="detail-label">配置名称</div>
                            <div class="detail-value">${config.name}</div>
                        </div>
                        <div class="detail-group">
                            <div class="detail-label">运行状态</div>
                            <div class="detail-value">
                                <span class="config-status ${statusClass}">${statusText}</span>
                            </div>
                        </div>
                        <div class="detail-group">
                            <div class="detail-label">服务器地址</div>
                            <div class="detail-value">${config.host}</div>
                        </div>
                        <div class="detail-group">
                            <div class="detail-label">SSH 端口</div>
                            <div class="detail-value">${config.port}</div>
                        </div>
                        <div class="detail-group">
                            <div class="detail-label">用户名</div>
                            <div class="detail-value">${config.username}</div>
                        </div>
                        <div class="detail-group">
                            <div class="detail-label">认证方式</div>
                            <div class="detail-value">${authTypeText}</div>
                        </div>
                        <div class="detail-group">
                            <div class="detail-label">${config.auth_type === 'key' ? '密钥路径' : '密码'}</div>
                            <div class="detail-value">${authValue}</div>
                        </div>
                        <div class="detail-group">
                            <div class="detail-label">转发类型</div>
                            <div class="detail-value">${tunnelTypeText}</div>
                        </div>
                        <div class="detail-group">
                            <div class="detail-label">端口映射</div>
                            <div class="detail-value">${portMapping}</div>
                        </div>
                        <div class="detail-group">
                            <div class="detail-label">配置 ID</div>
                            <div class="detail-value">${config.id}</div>
                        </div>
                    `;
                    
                    document.getElementById('detailModal').classList.add('show');
                } else {
                    showToast('获取配置失败: ' + result.error, 'error');
                }
            } catch (error) {
                showToast('获取配置失败', 'error');
                console.error(error);
            }
        }

        // 关闭详情模态框
        function closeDetailModal() {
            document.getElementById('detailModal').classList.remove('show');
        }

        // 点击模态框背景关闭
        document.getElementById('detailModal').addEventListener('click', function(e) {
            if (e.target === this) {
                closeDetailModal();
            }
        });

        // 启动隧道
        async function startTunnel(id) {
            try {
                const result = await pywebview.api.start_tunnel(id);
                if (result.success) {
                    showToast(result.message, 'success');
                    await loadConfigs();
                } else {
                    showToast('启动失败: ' + result.error, 'error');
                }
            } catch (error) {
                showToast('启动隧道失败', 'error');
                console.error(error);
            }
        }

        // 停止隧道
        async function stopTunnel(id) {
            try {
                const result = await pywebview.api.stop_tunnel(id);
                if (result.success) {
                    showToast(result.message, 'success');
                    await loadConfigs();
                } else {
                    showToast('停止失败: ' + result.error, 'error');
                }
            } catch (error) {
                showToast('停止隧道失败', 'error');
                console.error(error);
            }
        }

        // 删除配置
        async function deleteConfig(id) {
            if (!confirm('确定要删除这个配置吗？')) {
                return;
            }

            try {
                const result = await pywebview.api.delete_config(id);
                if (result.success) {
                    showToast('配置已删除', 'success');
                    await loadConfigs();
                } else {
                    showToast('删除失败: ' + result.error, 'error');
                }
            } catch (error) {
                showToast('删除配置失败', 'error');
                console.error(error);
            }
        }

        // 显示提示信息
        function showToast(message, type = 'success') {
            const toast = document.createElement('div');
            toast.className = `toast toast-${type}`;
            toast.textContent = message;
            document.body.appendChild(toast);

            setTimeout(() => {
                toast.style.animation = 'slideOut 0.3s ease-out';
                setTimeout(() => toast.remove(), 300);
            }, 3000);
        }
    </script>
</body>
</html>
"""


def main():
    """主函数"""
    api = API()

    # 注册退出时的清理函数
    def cleanup():
        """程序退出时的清理"""
        print("程序即将退出，正在清理资源...")
        api.stop_all_tunnels()
        print("资源清理完成")

    atexit.register(cleanup)

    webview.create_window(
        title="SSH 端口转发配置管理",
        html=HTML_CONTENT,
        js_api=api,
        width=1200,
        height=700,
        resizable=True,
    )

    webview.start()

    # webview.start() 阻塞直到所有窗口关闭
    # 窗口关闭后会自动触发 atexit 注册的清理函数


if __name__ == "__main__":
    main()
