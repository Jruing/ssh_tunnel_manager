package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type TunnelType string

const (
	LocalTunnel   TunnelType = "local"
	RemoteTunnel  TunnelType = "remote"
	DynamicTunnel TunnelType = "dynamic"
)

type TunnelStatus string

const (
	StatusStopped TunnelStatus = "stopped"
	StatusRunning TunnelStatus = "running"
	StatusError   TunnelStatus = "error"
)

type TunnelConfig struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Type        TunnelType `json:"type"`
	LocalPort   int        `json:"local_port"`
	RemoteHost  string     `json:"remote_host"`
	RemotePort  int        `json:"remote_port"`
	SSHHost     string     `json:"ssh_host"`
	SSHPort     int        `json:"ssh_port"`
	SSHUser     string     `json:"ssh_user"`
	SSHKeyPath  string     `json:"ssh_key_path,omitempty"`
	Description string     `json:"description,omitempty"`

	// SSH连接参数
	Background          bool `json:"background"`            // -f 后台运行
	NoCommand           bool `json:"no_command"`            // -N 不执行远程命令
	GatewayPorts        bool `json:"gateway_ports"`         // -g 允许远程主机连接到本地转发端口
	Compression         bool `json:"compression"`           // -C 启用压缩
	Quiet               bool `json:"quiet"`                 // -q 安静模式
	KeepAlive           bool `json:"keep_alive"`            // 保持连接活跃
	ServerAliveInterval int  `json:"server_alive_interval"` // ServerAliveInterval参数

	// 自动重连配置
	AutoReconnect        bool `json:"auto_reconnect"`         // 启用自动重连
	ReconnectInterval    int  `json:"reconnect_interval"`     // 重连间隔(秒)
	MaxReconnectAttempts int  `json:"max_reconnect_attempts"` // 最大重连尝试次数，0表示无限重连
	ReconnectBackoff     bool `json:"reconnect_backoff"`      // 启用指数退避重连
}

type Tunnel struct {
	Config    TunnelConfig `json:"config"`
	Status    TunnelStatus `json:"status"`
	Error     string       `json:"error,omitempty"`
	StartTime *time.Time   `json:"start_time,omitempty"`

	// 重连状态
	ReconnectCount    int        `json:"reconnect_count"`
	LastReconnectTime *time.Time `json:"last_reconnect_time,omitempty"`

	// 内部字段（不序列化）
	listener        net.Listener
	sshClient       *ssh.Client
	cancel          chan struct{}
	reconnectCancel chan struct{}
}

type TunnelManager struct {
	tunnels    map[string]*Tunnel
	configDir  string
	configFile string
	mu         sync.RWMutex
}

func NewTunnelManager() (*TunnelManager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".ssh-tunnel-manager")
	configFile := filepath.Join(configDir, "tunnels.json")

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	tm := &TunnelManager{
		tunnels:    make(map[string]*Tunnel),
		configDir:  configDir,
		configFile: configFile,
	}

	if err := tm.loadConfig(); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return tm, nil
}

func (tm *TunnelManager) AddTunnel(config TunnelConfig) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if config.ID == "" {
		config.ID = fmt.Sprintf("tunnel_%d", time.Now().Unix())
	}

	if _, exists := tm.tunnels[config.ID]; exists {
		return fmt.Errorf("tunnel with ID %s already exists", config.ID)
	}

	tunnel := &Tunnel{
		Config:          config,
		Status:          StatusStopped,
		cancel:          make(chan struct{}),
		reconnectCancel: make(chan struct{}),
	}

	tm.tunnels[config.ID] = tunnel
	return tm.saveConfig()
}

func (tm *TunnelManager) RemoveTunnel(id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tunnel, exists := tm.tunnels[id]
	if !exists {
		return fmt.Errorf("tunnel with ID %s not found", id)
	}

	if tunnel.Status == StatusRunning {
		tm.stopTunnel(tunnel)
	}

	delete(tm.tunnels, id)
	return tm.saveConfig()
}

func (tm *TunnelManager) StartTunnel(id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tunnel, exists := tm.tunnels[id]
	if !exists {
		return fmt.Errorf("tunnel with ID %s not found", id)
	}

	if tunnel.Status == StatusRunning {
		return fmt.Errorf("tunnel %s is already running", id)
	}

	return tm.startTunnel(tunnel)
}

func (tm *TunnelManager) StopTunnel(id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tunnel, exists := tm.tunnels[id]
	if !exists {
		return fmt.Errorf("tunnel with ID %s not found", id)
	}

	if tunnel.Status != StatusRunning {
		return fmt.Errorf("tunnel %s is not running", id)
	}

	tm.stopTunnel(tunnel)
	return nil
}

func (tm *TunnelManager) GetTunnels() map[string]*Tunnel {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	result := make(map[string]*Tunnel)
	for id, tunnel := range tm.tunnels {
		result[id] = tunnel
	}
	return result
}

func (tm *TunnelManager) GetTunnel(id string) (*Tunnel, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tunnel, exists := tm.tunnels[id]
	return tunnel, exists
}

func (tm *TunnelManager) startTunnel(tunnel *Tunnel) error {
	config := tunnel.Config

	sshConfig := &ssh.ClientConfig{
		User:            config.SSHUser,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	// 应用SSH参数配置
	if config.Compression {
		// Go SSH库默认不支持压缩，这里只记录配置
		log.Printf("Compression enabled for tunnel %s", config.Name)
	}

	if config.SSHKeyPath != "" {
		key, err := os.ReadFile(config.SSHKeyPath)
		if err != nil {
			tunnel.Status = StatusError
			tunnel.Error = fmt.Sprintf("failed to read SSH key: %v", err)
			return err
		}

		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			tunnel.Status = StatusError
			tunnel.Error = fmt.Sprintf("failed to parse SSH key: %v", err)
			return err
		}

		sshConfig.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	}

	// 设置KeepAlive
	if config.KeepAlive && config.ServerAliveInterval > 0 {
		sshConfig.Timeout = time.Duration(config.ServerAliveInterval) * time.Second
	}

	sshAddr := net.JoinHostPort(config.SSHHost, strconv.Itoa(config.SSHPort))
	sshClient, err := ssh.Dial("tcp", sshAddr, sshConfig)
	if err != nil {
		tunnel.Status = StatusError
		tunnel.Error = fmt.Sprintf("failed to connect to SSH server: %v", err)
		return err
	}

	var listener net.Listener
	var listenAddr string

	// 根据GatewayPorts参数决定监听地址
	if config.GatewayPorts {
		listenAddr = net.JoinHostPort("0.0.0.0", strconv.Itoa(config.LocalPort))
	} else {
		listenAddr = net.JoinHostPort("localhost", strconv.Itoa(config.LocalPort))
	}

	switch config.Type {
	case LocalTunnel:
		listener, err = net.Listen("tcp", listenAddr)
		if err != nil {
			sshClient.Close()
			tunnel.Status = StatusError
			tunnel.Error = fmt.Sprintf("failed to listen on local port: %v", err)
			return err
		}
	case RemoteTunnel:
		remoteAddr := net.JoinHostPort("localhost", strconv.Itoa(config.LocalPort))
		listener, err = sshClient.Listen("tcp", remoteAddr)
		if err != nil {
			sshClient.Close()
			tunnel.Status = StatusError
			tunnel.Error = fmt.Sprintf("failed to create remote listener: %v", err)
			return err
		}
	case DynamicTunnel:
		// 动态端口转发 (SOCKS5代理)
		listener, err = net.Listen("tcp", listenAddr)
		if err != nil {
			sshClient.Close()
			tunnel.Status = StatusError
			tunnel.Error = fmt.Sprintf("failed to listen on local port for SOCKS5: %v", err)
			return err
		}
	}

	tunnel.sshClient = sshClient
	tunnel.listener = listener
	tunnel.Status = StatusRunning
	tunnel.Error = ""
	now := time.Now()
	tunnel.StartTime = &now
	tunnel.cancel = make(chan struct{})
	tunnel.reconnectCancel = make(chan struct{})

	// 启动隧道处理和自动重连监控
	go tm.handleTunnel(tunnel)
	if tunnel.Config.AutoReconnect {
		go tm.monitorConnection(tunnel)
	}

	// 如果不是安静模式，记录启动信息
	if !config.Quiet {
		log.Printf("Tunnel %s started on %s", config.Name, listenAddr)
	}

	return nil
}

func (tm *TunnelManager) stopTunnel(tunnel *Tunnel) {
	if tunnel.cancel != nil {
		close(tunnel.cancel)
	}

	if tunnel.reconnectCancel != nil {
		close(tunnel.reconnectCancel)
	}

	if tunnel.listener != nil {
		tunnel.listener.Close()
	}

	if tunnel.sshClient != nil {
		tunnel.sshClient.Close()
	}

	tunnel.Status = StatusStopped
	tunnel.Error = ""
	tunnel.StartTime = nil
	tunnel.ReconnectCount = 0
	tunnel.LastReconnectTime = nil
}

func (tm *TunnelManager) handleTunnel(tunnel *Tunnel) {
	for {
		select {
		case <-tunnel.cancel:
			return
		default:
			conn, err := tunnel.listener.Accept()
			if err != nil {
				select {
				case <-tunnel.cancel:
					return
				default:
					tunnel.Status = StatusError
					tunnel.Error = fmt.Sprintf("failed to accept connection: %v", err)
					return
				}
			}

			go tm.handleConnection(tunnel, conn)
		}
	}
}

func (tm *TunnelManager) handleConnection(tunnel *Tunnel, localConn net.Conn) {
	defer localConn.Close()

	config := tunnel.Config

	switch config.Type {
	case LocalTunnel, RemoteTunnel:
		tm.handlePortForwarding(tunnel, localConn)
	case DynamicTunnel:
		tm.handleSOCKS5(tunnel, localConn)
	}
}

func (tm *TunnelManager) handlePortForwarding(tunnel *Tunnel, localConn net.Conn) {
	config := tunnel.Config
	remoteAddr := net.JoinHostPort(config.RemoteHost, strconv.Itoa(config.RemotePort))

	var remoteConn net.Conn
	var err error

	switch config.Type {
	case LocalTunnel:
		remoteConn, err = tunnel.sshClient.Dial("tcp", remoteAddr)
	case RemoteTunnel:
		remoteConn, err = net.Dial("tcp", remoteAddr)
	}

	if err != nil {
		log.Printf("Failed to connect to remote %s: %v", remoteAddr, err)
		return
	}
	defer remoteConn.Close()

	done := make(chan struct{}, 2)

	go func() {
		defer func() { done <- struct{}{} }()
		buffer := make([]byte, 32*1024)
		for {
			n, err := localConn.Read(buffer)
			if err != nil {
				break
			}
			if _, err := remoteConn.Write(buffer[:n]); err != nil {
				break
			}
		}
	}()

	go func() {
		defer func() { done <- struct{}{} }()
		buffer := make([]byte, 32*1024)
		for {
			n, err := remoteConn.Read(buffer)
			if err != nil {
				break
			}
			if _, err := localConn.Write(buffer[:n]); err != nil {
				break
			}
		}
	}()

	<-done
}

func (tm *TunnelManager) handleSOCKS5(tunnel *Tunnel, localConn net.Conn) {
	// 简化的SOCKS5实现
	// 1. 读取客户端的认证方法选择
	buffer := make([]byte, 256)
	n, err := localConn.Read(buffer)
	if err != nil || n < 3 {
		return
	}

	// 检查SOCKS版本
	if buffer[0] != 0x05 {
		return // 不是SOCKS5
	}

	// 回复：选择无认证方法
	if _, err := localConn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// 2. 读取连接请求
	n, err = localConn.Read(buffer)
	if err != nil || n < 10 {
		return
	}

	// 检查请求格式
	if buffer[0] != 0x05 || buffer[1] != 0x01 || buffer[2] != 0x00 {
		// 发送错误响应
		localConn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		return
	}

	var targetHost string
	var targetPort int

	// 解析目标地址
	if buffer[3] == 0x01 { // IPv4
		if n < 10 {
			return
		}
		targetHost = fmt.Sprintf("%d.%d.%d.%d", buffer[4], buffer[5], buffer[6], buffer[7])
		targetPort = int(buffer[8])<<8 + int(buffer[9])
	} else if buffer[3] == 0x03 { // 域名
		if n < 7 {
			return
		}
		domainLen := int(buffer[4])
		if n < 5+domainLen+2 {
			return
		}
		targetHost = string(buffer[5 : 5+domainLen])
		targetPort = int(buffer[5+domainLen])<<8 + int(buffer[5+domainLen+1])
	} else {
		// 不支持的地址类型
		localConn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		return
	}

	// 通过SSH连接到目标
	targetAddr := net.JoinHostPort(targetHost, strconv.Itoa(targetPort))
	remoteConn, err := tunnel.sshClient.Dial("tcp", targetAddr)
	if err != nil {
		// 连接失败
		localConn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		return
	}
	defer remoteConn.Close()

	// 发送成功响应
	response := []byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if _, err := localConn.Write(response); err != nil {
		return
	}

	// 开始数据转发
	done := make(chan struct{}, 2)

	go func() {
		defer func() { done <- struct{}{} }()
		buffer := make([]byte, 32*1024)
		for {
			n, err := localConn.Read(buffer)
			if err != nil {
				break
			}
			if _, err := remoteConn.Write(buffer[:n]); err != nil {
				break
			}
		}
	}()

	go func() {
		defer func() { done <- struct{}{} }()
		buffer := make([]byte, 32*1024)
		for {
			n, err := remoteConn.Read(buffer)
			if err != nil {
				break
			}
			if _, err := localConn.Write(buffer[:n]); err != nil {
				break
			}
		}
	}()

	<-done
}

func (tm *TunnelManager) loadConfig() error {
	data, err := os.ReadFile(tm.configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var configs []TunnelConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return err
	}

	for _, config := range configs {
		tunnel := &Tunnel{
			Config:          config,
			Status:          StatusStopped,
			cancel:          make(chan struct{}),
			reconnectCancel: make(chan struct{}),
		}
		tm.tunnels[config.ID] = tunnel
	}

	return nil
}

func (tm *TunnelManager) saveConfig() error {
	var configs []TunnelConfig
	for _, tunnel := range tm.tunnels {
		configs = append(configs, tunnel.Config)
	}

	data, err := json.Marshal(configs)
	if err != nil {
		return err
	}

	return os.WriteFile(tm.configFile, data, fs.FileMode(0644))
}

func (tm *TunnelManager) Close() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for _, tunnel := range tm.tunnels {
		if tunnel.Status == StatusRunning {
			tm.stopTunnel(tunnel)
		}
	}
}

// monitorConnection 监控SSH连接状态并在断开时自动重连
func (tm *TunnelManager) monitorConnection(tunnel *Tunnel) {
	config := tunnel.Config
	ticker := time.NewTicker(time.Duration(config.ServerAliveInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-tunnel.reconnectCancel:
			return
		case <-ticker.C:
			// 检查SSH连接是否仍然活跃
			if tunnel.sshClient != nil {
				// 发送保活请求
				_, _, err := tunnel.sshClient.SendRequest("keepalive@openssh.com", true, nil)
				if err != nil {
					if !config.Quiet {
						log.Printf("SSH connection lost for tunnel %s: %v", config.Name, err)
					}
					tm.attemptReconnect(tunnel)
				}
			}
		}
	}
}

// attemptReconnect 尝试重新连接隧道
func (tm *TunnelManager) attemptReconnect(tunnel *Tunnel) {
	config := tunnel.Config

	// 检查是否超过最大重连次数
	if config.MaxReconnectAttempts > 0 && tunnel.ReconnectCount >= config.MaxReconnectAttempts {
		tunnel.Status = StatusError
		tunnel.Error = fmt.Sprintf("达到最大重连次数 %d", config.MaxReconnectAttempts)
		if !config.Quiet {
			log.Printf("Tunnel %s reached max reconnect attempts", config.Name)
		}
		return
	}

	// 停止当前连接
	if tunnel.listener != nil {
		tunnel.listener.Close()
	}
	if tunnel.sshClient != nil {
		tunnel.sshClient.Close()
	}

	tunnel.ReconnectCount++
	now := time.Now()
	tunnel.LastReconnectTime = &now

	// 计算重连延迟
	delay := time.Duration(config.ReconnectInterval) * time.Second
	if config.ReconnectBackoff && tunnel.ReconnectCount > 1 {
		// 指数退避：每次重连延迟翻倍，最大60秒
		backoffDelay := time.Duration(config.ReconnectInterval*(1<<(tunnel.ReconnectCount-1))) * time.Second
		if backoffDelay > 60*time.Second {
			backoffDelay = 60 * time.Second
		}
		delay = backoffDelay
	}

	if !config.Quiet {
		log.Printf("Attempting to reconnect tunnel %s (attempt %d) in %v",
			config.Name, tunnel.ReconnectCount, delay)
	}

	// 等待重连延迟
	select {
	case <-tunnel.reconnectCancel:
		return
	case <-time.After(delay):
		// 继续重连
	}

	// 尝试重新建立连接
	err := tm.reconnectTunnel(tunnel)
	if err != nil {
		tunnel.Status = StatusError
		tunnel.Error = fmt.Sprintf("重连失败: %v", err)
		if !config.Quiet {
			log.Printf("Failed to reconnect tunnel %s: %v", config.Name, err)
		}

		// 递归尝试下一次重连
		go func() {
			time.Sleep(time.Second) // 短暂延迟防止快速循环
			tm.attemptReconnect(tunnel)
		}()
	} else {
		tunnel.Status = StatusRunning
		tunnel.Error = ""
		if !config.Quiet {
			log.Printf("Successfully reconnected tunnel %s", config.Name)
		}
	}
}

// reconnectTunnel 重新建立隧道连接
func (tm *TunnelManager) reconnectTunnel(tunnel *Tunnel) error {
	config := tunnel.Config

	// 重新建立SSH连接
	sshConfig := &ssh.ClientConfig{
		User:            config.SSHUser,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	if config.SSHKeyPath != "" {
		key, err := os.ReadFile(config.SSHKeyPath)
		if err != nil {
			return fmt.Errorf("failed to read SSH key: %v", err)
		}

		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return fmt.Errorf("failed to parse SSH key: %v", err)
		}

		sshConfig.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	}

	if config.KeepAlive && config.ServerAliveInterval > 0 {
		sshConfig.Timeout = time.Duration(config.ServerAliveInterval) * time.Second
	}

	sshAddr := net.JoinHostPort(config.SSHHost, strconv.Itoa(config.SSHPort))
	sshClient, err := ssh.Dial("tcp", sshAddr, sshConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to SSH server: %v", err)
	}

	var listener net.Listener
	var listenAddr string

	if config.GatewayPorts {
		listenAddr = net.JoinHostPort("0.0.0.0", strconv.Itoa(config.LocalPort))
	} else {
		listenAddr = net.JoinHostPort("localhost", strconv.Itoa(config.LocalPort))
	}

	switch config.Type {
	case LocalTunnel:
		listener, err = net.Listen("tcp", listenAddr)
		if err != nil {
			sshClient.Close()
			return fmt.Errorf("failed to listen on local port: %v", err)
		}
	case RemoteTunnel:
		remoteAddr := net.JoinHostPort("localhost", strconv.Itoa(config.LocalPort))
		listener, err = sshClient.Listen("tcp", remoteAddr)
		if err != nil {
			sshClient.Close()
			return fmt.Errorf("failed to create remote listener: %v", err)
		}
	case DynamicTunnel:
		listener, err = net.Listen("tcp", listenAddr)
		if err != nil {
			sshClient.Close()
			return fmt.Errorf("failed to listen on local port for SOCKS5: %v", err)
		}
	}

	// 更新隧道连接
	tunnel.sshClient = sshClient
	tunnel.listener = listener

	// 重新启动隧道处理
	go tm.handleTunnel(tunnel)

	return nil
}
