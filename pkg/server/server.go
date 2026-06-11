package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
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
	ConnTimeout time.Duration
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

const bufferSize = 128 * 1024
const defaultConnTimeout = 300 * time.Second

var (
	bufferPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, bufferSize)
		},
	}
	poolGetCount   int64
	poolPutCount   int64
	poolAllocCount int64
	poolMu         sync.Mutex
)

func getBuffer() []byte {
	buf := bufferPool.Get().([]byte)
	if buf == nil {
		poolMu.Lock()
		poolAllocCount++
		poolMu.Unlock()
		return make([]byte, bufferSize)
	}
	poolMu.Lock()
	poolGetCount++
	poolMu.Unlock()
	return buf
}

func putBuffer(buf []byte) {
	poolMu.Lock()
	poolPutCount++
	poolMu.Unlock()
	bufferPool.Put(buf)
}

func getPoolStats() (get, put, alloc int64) {
	poolMu.Lock()
	defer poolMu.Unlock()
	return poolGetCount, poolPutCount, poolAllocCount
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
	defer conn.Close()

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
			case "cancel-tcpip-forward":
				s.handleCancelForwardRequest(req, connInfo)
			case "tbore-client-info":
				s.handleClientInfoRequest(req, connInfo)
			case "tbore-tunnel-info":
				s.handleTunnelInfoRequest(req, connInfo)
			case "tbore-health-check":
				s.handleHealthCheckRequest(req, connInfo)
			case "tbore-health-report":
				s.handleHealthReportRequest(req, connInfo)
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
		Timeout   int    `json:"timeout"`
	}
	if err := json.Unmarshal(req.Payload, &info); err != nil {
		log.Printf("Failed to parse tunnel info: %v", err)
		req.Reply(false, nil)
		return
	}

	if tunnel, ok := connInfo.Tunnels[info.Port]; ok {
		tunnel.Name = info.Name
		tunnel.LocalAddr = fmt.Sprintf("%s:%d", info.LocalIP, info.LocalPort)
		tunnel.ConnTimeout = time.Duration(info.Timeout) * time.Second
	}

	req.Reply(true, nil)
}

func (s *Server) handleHealthCheckRequest(req *ssh.Request, connInfo *ConnectionInfo) {
	var healthInfo []struct {
		Port   uint32      `json:"port"`
		Status TunnelState `json:"status"`
	}

	for port, tunnel := range connInfo.Tunnels {
		healthInfo = append(healthInfo, struct {
			Port   uint32      `json:"port"`
			Status TunnelState `json:"status"`
		}{
			Port:   port,
			Status: tunnel.State,
		})
	}

	data, err := json.Marshal(healthInfo)
	if err != nil {
		log.Printf("Failed to marshal health info: %v", err)
		req.Reply(false, nil)
		return
	}

	req.Reply(true, data)
}

func (s *Server) handleHealthReportRequest(req *ssh.Request, connInfo *ConnectionInfo) {
	var info struct {
		Port   uint32 `json:"port"`
		Status int    `json:"status"`
	}

	if err := json.Unmarshal(req.Payload, &info); err != nil {
		log.Printf("Failed to parse health report: %v", err)
		req.Reply(false, nil)
		return
	}

	if tunnel, ok := connInfo.Tunnels[info.Port]; ok {
		newState := TunnelState(info.Status)
		if tunnel.State != newState {
			tunnel.State = newState
			log.Printf("[%s] Tunnel :%d health status changed: %s", connInfo.ID, info.Port, tunnel.State)
		}
	}

	req.Reply(true, nil)
}

func (s *Server) handleCancelForwardRequest(req *ssh.Request, connInfo *ConnectionInfo) {
	var msg struct {
		Addr string
		Port uint32
	}
	ssh.Unmarshal(req.Payload, &msg)

	tunnel, ok := connInfo.Tunnels[msg.Port]
	if !ok {
		log.Printf("[%s] Cancel forward failed: tunnel :%d not found", connInfo.ID, msg.Port)
		req.Reply(false, nil)
		return
	}

	if tunnel.Listener != nil {
		tunnel.Listener.Close()
	}

	delete(connInfo.Tunnels, msg.Port)
	log.Printf("[%s] Tunnel canceled: :%d", connInfo.ID, msg.Port)
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
		State:       TunnelStateIdle,
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

	tunnel := connInfo.Tunnels[actualPort]
	if tunnel == nil || tunnel.LocalAddr == "" {
		if t, ok := connInfo.Tunnels[actualPort]; ok {
			t.ActiveConns--
		}
		return
	}

	payload := struct {
		Addr       string
		Port       uint32
		OriginAddr string
		OriginPort uint32
	}{
		Addr:       tunnel.LocalAddr,
		Port:       actualPort,
		OriginAddr: host,
		OriginPort: p,
	}

	ch, r, err := sshConn.OpenChannel("forwarded-tcpip", ssh.Marshal(&payload))
	if err != nil {
		tunnel.State = TunnelStateError
		tunnel.ActiveConns--
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer ch.Close()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				req, ok := <-r
				if !ok {
					return
				}
				req.Reply(false, nil)
			}
		}
	}()

	bufA := getBuffer()
	bufB := getBuffer()

	var wg sync.WaitGroup
	wg.Add(2)

	timeout := tunnel.ConnTimeout
	if timeout < 0 {
		timeout = defaultConnTimeout
	}

	var lastActivityTime int64 = time.Now().Unix()

	go func() {
		defer wg.Done()
		defer putBuffer(bufA)
		copyBufferWithTimestamp(user, ch, bufA, &lastActivityTime)
		ch.CloseWrite()
	}()
	go func() {
		defer wg.Done()
		defer putBuffer(bufB)
		copyBufferWithTimestamp(ch, user, bufB, &lastActivityTime)
	}()

	if timeout > 0 {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if time.Now().Unix()-atomic.LoadInt64(&lastActivityTime) >= int64(timeout.Seconds()) {
						log.Printf("[%s] Connection timeout after %d seconds of inactivity", connInfo.ID, timeout.Seconds())
						cancel()
						ch.Close()
						user.Close()
						return
					}
				}
			}
		}()
	}

	wg.Wait()

	tunnel.ActiveConns--
	log.Printf("[%s] Connection closed, active conns: %d", connInfo.ID, tunnel.ActiveConns)
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

func copyBufferWithTimestamp(dst, src io.ReadWriter, buf []byte, lastActivityTime *int64) {
	for {
		n, err := src.Read(buf)
		if err != nil {
			return
		}
		atomic.StoreInt64(lastActivityTime, time.Now().Unix())
		if _, err := dst.Write(buf[:n]); err != nil {
			return
		}
	}
}

func bidirectionalCopy(a, b io.ReadWriter) {
	bufA := getBuffer()
	bufB := getBuffer()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer putBuffer(bufA)
		copyBuffer(a, b, bufA)
	}()
	go func() {
		defer wg.Done()
		defer putBuffer(bufB)
		copyBuffer(b, a, bufB)
	}()
	wg.Wait()
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
