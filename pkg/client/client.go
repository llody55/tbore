package client

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"tbore/pkg/auth"
	"tbore/pkg/config"
)

type Client struct {
	cfg  *config.ClientConfig
	auth *auth.Authenticator
}

func NewClient(cfg *config.ClientConfig) *Client {
	return &Client{
		cfg:  cfg,
		auth: auth.NewAuthenticator(cfg.Token),
	}
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
		}

		client, err := ssh.Dial("tcp", target, sshConfig)
		if err != nil {
			log.Printf("Dial error: %v. Retry in 5s...", err)
			time.Sleep(5 * time.Second)
			continue
		}

		go c.sendKeepAlive(client)

		for _, t := range c.cfg.Tunnels {
			go c.createTunnel(client, t)
		}

		client.Wait()
		log.Printf("Connection lost. Reconnecting...")
		time.Sleep(5 * time.Second)
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

func (c *Client) createTunnel(client *ssh.Client, tunnel config.TunnelConfig) {
	l, err := client.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", tunnel.RemotePort))
	if err != nil {
		log.Printf("Tunnel [%s] failed: %v", tunnel.Name, err)
		return
	}

	log.Printf("SUCCESS [%s]: :%d -> %s:%d", tunnel.Name, l.Addr().(*net.TCPAddr).Port, tunnel.LocalIP, tunnel.LocalPort)

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
		dst.Write(buf[:n])
	}
}