package server

import (
	"encoding/binary"
	"encoding/json"
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
	"tbore/pkg/version"
)

type TunnelState int

const (
	TunnelStateActive TunnelState = iota
	TunnelStateIdle
	TunnelStateError
)

func (s TunnelState) String() string {
	switch s {
	case TunnelStateActive:
		return "active"
	case TunnelStateIdle:
		return "idle"
	case TunnelStateError:
		return "error"
	default:
		return "unknown"
	}
}

type TunnelInfo struct {
	RemotePort  uint32
	LocalAddr   string
	State       TunnelState
	ActiveConns int32
	Name        string
	Listener    net.Listener
}

type ClientInfo struct {
	Project string `json:"project"`
	Region  string `json:"region"`
}

type ConnectionInfo struct {
	ID          string
	RemoteAddr  string
	ConnectTime time.Time
	Tunnels     map[uint32]*TunnelInfo
	ClientInfo  ClientInfo
}

type Server struct {
	cfg           *config.ServerConfig
	auth          *auth.Authenticator
	sem           *semaphore.Weighted
	signer        ssh.Signer
	activeConns   sync.Map
	serverStarted time.Time
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
		cfg:           cfg,
		auth:          auth.NewAuthenticator(cfg.Token),
		sem:           semaphore.NewWeighted(int64(cfg.MaxConnections)),
		signer:        signer,
		serverStarted: time.Now(),
	}, nil
}

func generateSessionID() string {
	return fmt.Sprintf("sess_%x", time.Now().UnixNano())
}

func (s *Server) Start() error {
	sshConfig := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if len(pass) < 32 {
				return nil, fmt.Errorf("invalid authentication data from %s", c.RemoteAddr())
			}
			challenge := string(pass[:32])
			response := string(pass[32:])

			if !s.auth.ValidateResponse(challenge, response) {
				return nil, fmt.Errorf("authentication failed from %s", c.RemoteAddr())
			}

			return nil, nil
		},
	}

	sshConfig.AddHostKey(s.signer)

	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.cfg.BindAddr, s.cfg.Port))
	if err != nil {
		return err
	}

	log.Printf("tbore server v%s started on %s:%d", version.Version, s.cfg.BindAddr, s.cfg.Port)
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

	remoteAddr := conn.RemoteAddr().String()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, sshConfig)
	if err != nil {
		log.Printf("SSH handshake failed from %s: %v", remoteAddr, err)
		return
	}

	sessionID := generateSessionID()

	connInfo := &ConnectionInfo{
		ID:          sessionID,
		RemoteAddr:  sshConn.RemoteAddr().String(),
		ConnectTime: time.Now(),
		Tunnels:     make(map[uint32]*TunnelInfo),
	}

	s.activeConns.Store(sessionID, connInfo)

	log.Printf("Session [%s] established from %s", sessionID, sshConn.RemoteAddr())

	go func() {
		for req := range reqs {
			switch req.Type {
			case "tcpip-forward":
				s.handleForwardRequest(sshConn, req, connInfo)
			case "tbore-client-info":
				s.handleClientInfoRequest(req, connInfo)
			case "tbore-tunnel-info":
				s.handleTunnelInfoRequest(req, connInfo)
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
	log.Printf("Session [%s] closed, cleaning up ports...", sessionID)

	for port, tunnel := range connInfo.Tunnels {
		if tunnel.Listener != nil {
			tunnel.Listener.Close()
			log.Printf("Released port :%d", port)
		}
	}

	s.activeConns.Delete(sessionID)
}

func (s *Server) handleClientInfoRequest(req *ssh.Request, connInfo *ConnectionInfo) {
	var info ClientInfo
	if err := json.Unmarshal(req.Payload, &info); err != nil {
		log.Printf("Failed to parse client info: %v", err)
		req.Reply(false, nil)
		return
	}

	connInfo.ClientInfo = info
	log.Printf("Client info received for %s: project=%s, region=%s",
		connInfo.ID, info.Project, info.Region)
	req.Reply(true, nil)
}

func (s *Server) handleTunnelInfoRequest(req *ssh.Request, connInfo *ConnectionInfo) {
	var info struct {
		Port      uint32 `json:"port"`
		Name      string `json:"name"`
		LocalIP   string `json:"local_ip"`
		LocalPort int    `json:"local_port"`
	}
	if err := json.Unmarshal(req.Payload, &info); err != nil {
		log.Printf("Failed to parse tunnel info: %v", err)
		req.Reply(false, nil)
		return
	}

	if tunnel, ok := connInfo.Tunnels[info.Port]; ok {
		tunnel.Name = info.Name
		tunnel.LocalAddr = fmt.Sprintf("%s:%d", info.LocalIP, info.LocalPort)
	}

	req.Reply(true, nil)
}

func (s *Server) handleForwardRequest(sshConn *ssh.ServerConn, req *ssh.Request, connInfo *ConnectionInfo) {
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

	if len(connInfo.Tunnels) >= s.cfg.MaxTunnels {
		req.Reply(false, nil)
		log.Printf("Tunnel limit exceeded for %s (max: %d)", connInfo.ID, s.cfg.MaxTunnels)
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

	connInfo.Tunnels[actualPort] = &TunnelInfo{
		RemotePort:  actualPort,
		LocalAddr:   msg.Addr,
		State:       TunnelStateActive,
		ActiveConns: 0,
		Listener:    ln,
	}

	resp := make([]byte, 4)
	binary.BigEndian.PutUint32(resp, actualPort)
	req.Reply(true, resp)

	log.Printf("[%s] Tunnel Active: :%d (requested: %d)", connInfo.ID, actualPort, msg.Port)

	go s.acceptClientConnections(ln, sshConn, actualPort, connInfo)
}

func (s *Server) acceptClientConnections(ln net.Listener, sshConn *ssh.ServerConn, actualPort uint32, connInfo *ConnectionInfo) {
	for {
		uConn, err := ln.Accept()
		if err != nil {
			if tunnel, ok := connInfo.Tunnels[actualPort]; ok {
				tunnel.State = TunnelStateIdle
			}
			return
		}

		if tunnel, ok := connInfo.Tunnels[actualPort]; ok {
			tunnel.State = TunnelStateActive
			tunnel.ActiveConns++
		}

		if tcpConn, ok := uConn.(*net.TCPConn); ok {
			tcpConn.SetNoDelay(true)
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(3 * time.Minute)
		}

		go s.handleClientConnection(uConn, sshConn, actualPort, connInfo)
	}
}

func (s *Server) handleClientConnection(user net.Conn, sshConn *ssh.ServerConn, actualPort uint32, connInfo *ConnectionInfo) {
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
		Addr:       connInfo.Tunnels[actualPort].LocalAddr,
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

	if tunnel, ok := connInfo.Tunnels[actualPort]; ok {
		tunnel.ActiveConns--
	}
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

func (s *Server) GetStatus() ServerStatus {
	var connections []ConnectionInfo
	s.activeConns.Range(func(key, value interface{}) bool {
		if connInfo, ok := value.(*ConnectionInfo); ok {
			connections = append(connections, *connInfo)
		}
		return true
	})

	return ServerStatus{
		Uptime:      time.Since(s.serverStarted),
		Connections: connections,
		Config:      s.cfg,
	}
}

type ServerStatus struct {
	Uptime      time.Duration
	Connections []ConnectionInfo
	Config      *config.ServerConfig
}
