package client

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"

	"tbore/pkg/auth"
	"tbore/pkg/config"
)

const bufferSize = 128 * 1024

var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, bufferSize)
	},
}

type TunnelHandle struct {
	Tunnel     config.TunnelConfig
	Listener   net.Listener
	ActualPort uint32
	Active     bool
}

type Client struct {
	cfg          *config.ClientConfig
	auth         *auth.Authenticator
	sshClient    *ssh.Client
	tunnels      sync.Map
	tunnelsMutex sync.RWMutex
	configPath   string
	reloadChan   chan struct{}
}

func NewClient(cfg *config.ClientConfig, configPath string) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Client{
		cfg:        cfg,
		auth:       auth.NewAuthenticator(cfg.Token),
		configPath: configPath,
		reloadChan: make(chan struct{}, 1),
	}, nil
}

func (c *Client) Start() {
	go c.handleSignals()

	for {
		target := fmt.Sprintf("%s:%d", c.cfg.ServerAddr, c.cfg.ServerPort)
		log.Printf("Connecting to %s...", target)

		challenge := auth.GenerateChallenge()
		response := c.auth.ComputeResponse(challenge)
		authString := challenge + response

		sshConfig := &ssh.ClientConfig{
			User: "tbore",
			Auth: []ssh.AuthMethod{ssh.Password(authString)},
			HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
				if c.cfg.HostKey != "" {
					fingerprint := ssh.FingerprintSHA256(key)
					if fingerprint != c.cfg.HostKey {
						return fmt.Errorf("host key mismatch: expected %s, got %s", c.cfg.HostKey, fingerprint)
					}
				}
				return nil
			},
			Timeout: 15 * time.Second,
			HostKeyAlgorithms: []string{
				ssh.KeyAlgoRSA,
				ssh.KeyAlgoDSA,
				ssh.KeyAlgoECDSA256,
				ssh.KeyAlgoECDSA384,
				ssh.KeyAlgoECDSA521,
				ssh.KeyAlgoED25519,
			},
		}

		client, err := ssh.Dial("tcp", target, sshConfig)
		if err != nil {
			log.Printf("Dial error: %v. Retry in 5s...", err)
			time.Sleep(5 * time.Second)
			continue
		}

		c.sshClient = client

		go c.handleServerChannels(client)

		if err := c.sendClientInfo(client); err != nil {
			log.Printf("Failed to send client info: %v", err)
			client.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		go c.sendKeepAlive(client)
		go c.healthCheckLoop(client)
		go c.reloadLoop(client)

		for _, t := range c.cfg.Tunnels {
			go c.createTunnel(client, t)
		}

		client.Wait()
		c.sshClient = nil
		log.Printf("Connection lost. Reconnecting...")
		time.Sleep(5 * time.Second)
	}
}

func (c *Client) handleSignals() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP)

	for range sigChan {
		log.Println("Received SIGHUP, triggering config reload...")
		c.TriggerReload()
	}
}

func (c *Client) TriggerReload() {
	select {
	case c.reloadChan <- struct{}{}:
	default:
		log.Println("Reload already in progress")
	}
}

func (c *Client) reloadLoop(client *ssh.Client) {
	for range c.reloadChan {
		c.ReloadTunnels(client)
	}
}

func (c *Client) sendClientInfo(client *ssh.Client) error {
	info := struct {
		Project string `json:"project"`
		Region  string `json:"region"`
	}{
		Project: c.cfg.Project,
		Region:  c.cfg.Region,
	}

	data, err := json.Marshal(info)
	if err != nil {
		return err
	}

	_, _, err = client.SendRequest("tbore-client-info", true, data)
	return err
}

func (c *Client) sendTunnelInfo(client *ssh.Client, tunnel config.TunnelConfig, actualPort uint32) {
	info := struct {
		Port      uint32 `json:"port"`
		Name      string `json:"name"`
		LocalIP   string `json:"local_ip"`
		LocalPort int    `json:"local_port"`
	}{
		Port:      actualPort,
		Name:      tunnel.Name,
		LocalIP:   tunnel.LocalIP,
		LocalPort: tunnel.LocalPort,
	}

	data, err := json.Marshal(info)
	if err != nil {
		log.Printf("Failed to send tunnel info: %v", err)
		return
	}

	_, _, err = client.SendRequest("tbore-tunnel-info", true, data)
	if err != nil {
		log.Printf("Failed to send tunnel info: %v", err)
	}
}

func (c *Client) sendKeepAlive(client *ssh.Client) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()

	for range t.C {
		if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
			client.Close()
			return
		}
	}
}

func (c *Client) healthCheckLoop(client *ssh.Client) {
	t := time.NewTicker(time.Duration(c.cfg.HealthCheckInterval) * time.Second)
	defer t.Stop()

	log.Printf("Health check started with interval %ds", c.cfg.HealthCheckInterval)

	for range t.C {
		for _, tunnel := range c.cfg.Tunnels {
			go c.checkTunnelHealth(client, tunnel)
		}
	}
}

func (c *Client) checkTunnelHealth(client *ssh.Client, tunnel config.TunnelConfig) {
	addr := fmt.Sprintf("%s:%d", tunnel.LocalIP, tunnel.LocalPort)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		log.Printf("Health check failed for %s (%s): %v", tunnel.Name, addr, err)
		c.reportTunnelHealth(client, tunnel.RemotePort, 2)
		return
	}
	conn.Close()
	c.reportTunnelHealth(client, tunnel.RemotePort, 0)
}

func (c *Client) reportTunnelHealth(client *ssh.Client, remotePort uint32, status int) {
	info := struct {
		Port   uint32 `json:"port"`
		Status int    `json:"status"`
	}{
		Port:   remotePort,
		Status: status,
	}

	data, err := json.Marshal(info)
	if err != nil {
		log.Printf("Failed to marshal health info: %v", err)
		return
	}

	if _, _, err := client.SendRequest("tbore-health-report", true, data); err != nil {
		log.Printf("Failed to send health report: %v", err)
	}
}

func (c *Client) handleServerChannels(client *ssh.Client) {
	for newCh := range client.HandleChannelOpen("forwarded-tcpip") {
		var msg struct {
			Addr       string
			Port       uint32
			OriginAddr string
			OriginPort uint32
		}
		ssh.Unmarshal(newCh.ExtraData(), &msg)

		local, err := net.DialTimeout("tcp", msg.Addr, 5*time.Second)
		if err != nil {
			log.Printf("Failed to connect to local %s: %v", msg.Addr, err)
			newCh.Reject(ssh.ConnectionFailed, "failed to connect to local service")
			continue
		}

		ch, reqs, err := newCh.Accept()
		if err != nil {
			log.Printf("Failed to accept channel: %v", err)
			local.Close()
			continue
		}
		defer ch.Close()
		go ssh.DiscardRequests(reqs)

		go func(ch ssh.Channel, local net.Conn) {
			if tcpConn, ok := local.(*net.TCPConn); ok {
				tcpConn.SetNoDelay(true)
			}

			bufA := bufferPool.Get().([]byte)
			bufB := bufferPool.Get().([]byte)
			defer bufferPool.Put(bufA)
			defer bufferPool.Put(bufB)

			var wg sync.WaitGroup
			wg.Add(2)

			go func() {
				defer wg.Done()
				copyBufferRW(local, ch, bufA)
				ch.CloseWrite()
			}()
			go func() {
				defer wg.Done()
				copyBufferRW(ch, local, bufB)
				local.Close()
			}()
			wg.Wait()

			ch.Close()
			local.Close()
		}(ch, local)
	}
}

func copyBufferRW(dst io.ReadWriter, src io.ReadWriter, buf []byte) {
	for {
		n, err := src.Read(buf)
		if err != nil {
			return
		}
		if _, err := dst.Write(buf[:n]); err != nil {
			return
		}
	}
}

func (c *Client) cancelPortForward(client *ssh.Client, port uint32) {
	req := struct {
		Addr string
		Port uint32
	}{
		Addr: "0.0.0.0",
		Port: port,
	}

	_, _, err := client.SendRequest("cancel-tcpip-forward", true, ssh.Marshal(req))
	if err != nil {
		log.Printf("Failed to cancel port forward for port %d: %v", port, err)
	} else {
		log.Printf("Canceled port forward for port %d", port)
	}
}

func (c *Client) createTunnel(client *ssh.Client, tunnel config.TunnelConfig) {
	l, err := client.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", tunnel.RemotePort))
	if err != nil {
		log.Printf("Tunnel [%s] failed: %v", tunnel.Name, err)
		return
	}

	actualPort := uint32(l.Addr().(*net.TCPAddr).Port)

	handle := &TunnelHandle{
		Tunnel:     tunnel,
		Listener:   l,
		ActualPort: actualPort,
		Active:     true,
	}
	c.tunnels.Store(tunnel.Name, handle)

	c.sendTunnelInfo(client, tunnel, actualPort)

	log.Printf("SUCCESS [%s]: :%d -> %s:%d", tunnel.Name, actualPort, tunnel.LocalIP, tunnel.LocalPort)

	for {
		remote, err := l.Accept()
		if err != nil {
			break
		}

		go c.handleRemoteConnection(remote, tunnel)
	}
}

func (c *Client) ReloadTunnels(client *ssh.Client) {
	log.Println("Starting tunnel reload...")

	newCfg, err := config.LoadClientConfig(c.configPath)
	if err != nil {
		log.Printf("Failed to load config: %v", err)
		return
	}

	if err := newCfg.Validate(); err != nil {
		log.Printf("Invalid config: %v", err)
		return
	}

	newTunnels := make(map[string]config.TunnelConfig)
	for _, t := range newCfg.Tunnels {
		newTunnels[t.Name] = t
	}

	c.tunnelsMutex.Lock()

	c.tunnels.Range(func(key, value interface{}) bool {
		name := key.(string)
		handle := value.(*TunnelHandle)

		newTunnel, exists := newTunnels[name]
		if !exists {
			log.Printf("Removing tunnel [%s]", name)
			handle.Active = false
			c.cancelPortForward(client, handle.ActualPort)
			handle.Listener.Close()
			c.tunnels.Delete(name)
		} else {
			if handle.Tunnel.LocalIP != newTunnel.LocalIP ||
				handle.Tunnel.LocalPort != newTunnel.LocalPort ||
				handle.Tunnel.RemotePort != newTunnel.RemotePort {
				log.Printf("Updating tunnel [%s]: %s:%d -> :%d", name, newTunnel.LocalIP, newTunnel.LocalPort, newTunnel.RemotePort)
				handle.Active = false
				c.cancelPortForward(client, handle.ActualPort)
				handle.Listener.Close()
				c.tunnels.Delete(name)
			} else {
				delete(newTunnels, name)
			}
		}
		return true
	})

	for name, tunnel := range newTunnels {
		log.Printf("Adding tunnel [%s]: %s:%d -> :%d", name, tunnel.LocalIP, tunnel.LocalPort, tunnel.RemotePort)
		go c.createTunnel(client, tunnel)
	}

	c.cfg = newCfg

	c.tunnelsMutex.Unlock()

	log.Println("Tunnel reload completed")
}

func (c *Client) GetTunnelStatus() []struct {
	Name       string
	LocalIP    string
	LocalPort  int
	RemotePort uint32
	Active     bool
} {
	var status []struct {
		Name       string
		LocalIP    string
		LocalPort  int
		RemotePort uint32
		Active     bool
	}

	c.tunnels.Range(func(key, value interface{}) bool {
		name := key.(string)
		handle := value.(*TunnelHandle)
		status = append(status, struct {
			Name       string
			LocalIP    string
			LocalPort  int
			RemotePort uint32
			Active     bool
		}{
			Name:       name,
			LocalIP:    handle.Tunnel.LocalIP,
			LocalPort:  handle.Tunnel.LocalPort,
			RemotePort: handle.ActualPort,
			Active:     handle.Active,
		})
		return true
	})

	return status
}

func (c *Client) handleRemoteConnection(remote net.Conn, tunnel config.TunnelConfig) {
	defer remote.Close()

	local, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", tunnel.LocalIP, tunnel.LocalPort), 5*time.Second)
	if err != nil {
		log.Printf("Failed to connect to local %s:%d: %v", tunnel.LocalIP, tunnel.LocalPort, err)
		return
	}
	defer local.Close()

	if tcpConn, ok := local.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
	}

	bufA := bufferPool.Get().([]byte)
	bufB := bufferPool.Get().([]byte)
	defer bufferPool.Put(bufA)
	defer bufferPool.Put(bufB)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		copyBuffer(local, remote, bufA)
	}()
	go func() {
		defer wg.Done()
		copyBuffer(remote, local, bufB)
	}()
	wg.Wait()
}

func copyBuffer(dst, src net.Conn, buf []byte) {
	for {
		n, err := src.Read(buf)
		if err != nil {
			return
		}
		if _, err := dst.Write(buf[:n]); err != nil {
			return
		}
	}
}
