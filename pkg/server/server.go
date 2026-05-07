package server

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sync/semaphore"

	"tbore/pkg/auth"
	"tbore/pkg/config"
)

type Server struct {
	cfg         *config.ServerConfig
	auth        *auth.Authenticator
	sem         *semaphore.Weighted
	signer      ssh.Signer
	activeConns sync.Map
}

func NewServer(cfg *config.ServerConfig) (*Server, error) {
	err := cfg.Validate()
	if err != nil {
		return nil, err
	}

	signer, err := auth.LoadOrGenerateSigner(cfg.HostKeyPath)
	if err != nil {
		return nil, err
	}

	return &Server{
		cfg:    cfg,
		auth:   auth.NewAuthenticator(cfg.Token),
		sem:    semaphore.NewWeighted(int64(cfg.MaxConnections)),
		signer: signer,
	}, nil
}

func (s *Server) Start() error {
	sshConfig := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			challenge := string(pass[:32])
			response := string(pass[32:])

			if !s.auth.ValidateResponse(challenge, response) {
				return nil, fmt.Errorf("authentication failed")
			}

			return nil, nil
		},
	}

	sshConfig.AddHostKey(s.signer)

	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.cfg.BindAddr, s.cfg.Port))
	if err != nil {
		return err
	}

	log.Printf("tbore server v0.4.0 started on %s:%d", s.cfg.BindAddr, s.cfg.Port)
	log.Printf("Host key fingerprint: %s", auth.GetHostKeyFingerprint(s.signer))

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}

		if !s.sem.TryAcquire(1) {
			log.Printf("Connection rejected: too many connections")
			conn.Close()
			continue
		}

		go s.handleConnection(conn, sshConfig)
	}
}

func (s *Server) handleConnection(conn net.Conn, sshConfig *ssh.ServerConfig) {
	defer s.sem.Release(1)

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, sshConfig)
	if err != nil {
		log.Printf("SSH handshake failed: %v", err)
		return
	}

	var activeListeners sync.Map

	log.Printf("Session [%s] established", sshConn.RemoteAddr())

	go func() {
		for req := range reqs {
			switch req.Type {
			case "tcpip-forward":
				s.handleForwardRequest(sshConn, req, &activeListeners)
			case "keepalive@openssh.com":
				req.Reply(true, nil)
			default:
				req.Reply(false, nil)
			}
		}
	}()

	go func() {
		for newChan := range chans {
			newChan.Reject(ssh.Prohibited, "denied")
		}
	}()

	sshConn.Wait()
	log.Printf("Session [%s] closed, cleaning up ports...", sshConn.RemoteAddr())

	activeListeners.Range(func(key, value interface{}) bool {
		if ln, ok := value.(net.Listener); ok {
			ln.Close()
			log.Printf("Released port :%v", key)
		}
		return true
	})
}

func (s *Server) handleForwardRequest(sshConn *ssh.ServerConn, req *ssh.Request, listeners *sync.Map) {
	var msg struct {
		Addr string
		Port uint32
	}
	ssh.Unmarshal(req.Payload, &msg)

	if msg.Port != 0 && !s.cfg.IsValidPort(msg.Port) {
		req.Reply(false, nil)
		log.Printf("Port %d is not in allowed range [%d-%d]", msg.Port, s.cfg.MinPort, s.cfg.MaxPort)
		return
	}

	tunnelCount := 0
	listeners.Range(func(key, value interface{}) bool {
		tunnelCount++
		return true
	})
	if tunnelCount >= s.cfg.MaxTunnels {
		req.Reply(false, nil)
		log.Printf("Tunnel limit exceeded for %s (max: %d)", sshConn.RemoteAddr(), s.cfg.MaxTunnels)
		return
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.cfg.BindAddr, msg.Port))
	if err != nil {
		log.Printf("Bind failed for %d: %v", msg.Port, err)
		req.Reply(false, nil)
		return
	}

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var actualPort uint32
	fmt.Sscanf(portStr, "%d", &actualPort)

	listeners.Store(actualPort, ln)

	resp := make([]byte, 4)
	binary.BigEndian.PutUint32(resp, actualPort)
	req.Reply(true, resp)

	log.Printf("[%s] Tunnel Active: :%d (requested: %d)", sshConn.RemoteAddr(), actualPort, msg.Port)

	go s.acceptClientConnections(ln, sshConn, actualPort, msg.Addr)
}

func (s *Server) acceptClientConnections(ln net.Listener, sshConn *ssh.ServerConn, actualPort uint32, addr string) {
	defer ln.Close()
	for {
		uConn, err := ln.Accept()
		if err != nil {
			return
		}

		if tcpConn, ok := uConn.(*net.TCPConn); ok {
			tcpConn.SetNoDelay(true)
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(3 * time.Minute)
		}

		go s.handleClientConnection(uConn, sshConn, actualPort, addr)
	}
}

func (s *Server) handleClientConnection(user net.Conn, sshConn *ssh.ServerConn, actualPort uint32, addr string) {
	defer user.Close()

	host, pStr, _ := net.SplitHostPort(user.RemoteAddr().String())
	var p uint32
	fmt.Sscanf(pStr, "%d", &p)

	payload := struct {
		Addr       string
		Port       uint32
		OriginAddr string
		OriginPort uint32
	}{
		Addr:       addr,
		Port:       actualPort,
		OriginAddr: host,
		OriginPort: p,
	}

	ch, r, err := sshConn.OpenChannel("forwarded-tcpip", ssh.Marshal(&payload))
	if err != nil {
		return
	}
	go ssh.DiscardRequests(r)

	bufA := make([]byte, 128*1024)
	bufB := make([]byte, 128*1024)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		copyBuffer(user, ch, bufA)
	}()
	go func() {
		defer wg.Done()
		copyBuffer(ch, user, bufB)
	}()
	wg.Wait()
}

func copyBuffer(dst, src io.ReadWriter, buf []byte) {
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