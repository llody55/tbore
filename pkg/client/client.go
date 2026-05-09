package client

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"tbore/pkg/auth"
	"tbore/pkg/config"
)

type Client struct {
	cfg       *config.ClientConfig
	auth      *auth.Authenticator
	sshClient *ssh.Client
}

func NewClient(cfg *config.ClientConfig) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Client{
		cfg:  cfg,
		auth: auth.NewAuthenticator(cfg.Token),
	}, nil
}

func (c *Client) Start() {
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

		for _, t := range c.cfg.Tunnels {
			go c.createTunnel(client, t)
		}

		client.Wait()
		c.sshClient = nil
		log.Printf("Connection lost. Reconnecting...")
		time.Sleep(5 * time.Second)
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
		go ssh.DiscardRequests(reqs)

		go func(ch ssh.Channel, local net.Conn) {
			defer ch.Close()
			defer local.Close()

			if tcpConn, ok := local.(*net.TCPConn); ok {
				tcpConn.SetNoDelay(true)
			}

			bufA := make([]byte, 128*1024)
			bufB := make([]byte, 128*1024)

			var wg sync.WaitGroup
			wg.Add(2)

			go func() {
				defer wg.Done()
				copyBufferRW(local, ch, bufA)
			}()
			go func() {
				defer wg.Done()
				copyBufferRW(ch, local, bufB)
			}()
			wg.Wait()
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

func (c *Client) createTunnel(client *ssh.Client, tunnel config.TunnelConfig) {
	l, err := client.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", tunnel.RemotePort))
	if err != nil {
		log.Printf("Tunnel [%s] failed: %v", tunnel.Name, err)
		return
	}

	actualPort := uint32(l.Addr().(*net.TCPAddr).Port)
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

	bufA := make([]byte, 128*1024)
	bufB := make([]byte, 128*1024)

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
