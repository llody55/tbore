package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

const version = "0.3.0"

type TunnelConfig struct {
	Name       string `yaml:"name"`
	LocalIP    string `yaml:"local_ip"`
	LocalPort  int    `yaml:"local_port"`
	RemotePort uint32 `yaml:"remote_port"`
}

type Config struct {
	Client struct {
		Addr    string         `yaml:"server_addr"`
		Port    int            `yaml:"server_port"`
		Token   string         `yaml:"token"`
		Tunnels []TunnelConfig `yaml:"tunnels"`
	} `yaml:"client"`
}

func copyBidirectional(src, dst io.ReadWriteCloser) {
	defer src.Close()
	defer dst.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(dst, src) }()
	go func() { defer wg.Done(); io.Copy(src, dst) }()
	wg.Wait()
}

func generateSigner() (ssh.Signer, error) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemKey := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	return ssh.ParsePrivateKey(pem.EncodeToMemory(pemKey))
}


func runServer(port int, token string) {
	sshConfig := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if token != "" && string(pass) != token {
				return nil, fmt.Errorf("auth failed")
			}
			return nil, nil
		},
	}
	signer, _ := generateSigner()
	sshConfig.AddHostKey(signer)

	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		log.Fatalf("Server start error: %v", err)
	}
	log.Printf("tbore server v%s started on :%d", version, port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go func(nConn net.Conn) {
			sConn, chans, reqs, err := ssh.NewServerConn(nConn, sshConfig)
			if err != nil {
				return
			}
			
			var activeListeners sync.Map

			log.Printf("Session [%s] established", sConn.RemoteAddr())

			go func() {
				for req := range reqs {
					switch req.Type {
					case "tcpip-forward":
						handleForwardRequest(sConn, req, &activeListeners)
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

			sConn.Wait()
			log.Printf("Session [%s] closed, cleaning up ports...", sConn.RemoteAddr())
			
			activeListeners.Range(func(key, value interface{}) bool {
				if ln, ok := value.(net.Listener); ok {
					ln.Close()
					log.Printf("Released port :%v", key)
				}
				return true
			})
		}(conn)
	}
}

func handleForwardRequest(sConn *ssh.ServerConn, req *ssh.Request, listeners *sync.Map) {
	var msg struct {
		Addr string
		Port uint32
	}
	ssh.Unmarshal(req.Payload, &msg)

	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", msg.Port))
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

	log.Printf("[%s] Tunnel Active: :%d (requested: %d)", sConn.RemoteAddr(), actualPort, msg.Port)

	go func() {
		defer ln.Close()
		for {
			uConn, err := ln.Accept()
			if err != nil {
				return
			}
			
			go func(user net.Conn) {
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
					Addr:       msg.Addr,
					Port:       actualPort,
					OriginAddr: host,
					OriginPort: p,
				}

				ch, r, err := sConn.OpenChannel("forwarded-tcpip", ssh.Marshal(&payload))
				if err != nil {
					return
				}
				go ssh.DiscardRequests(r)
				copyBidirectional(user, ch)
			}(uConn)
		}
	}()
}


func runClient(cfg Config) {
	sshConfig := &ssh.ClientConfig{
		User:            "tbore",
		Auth:            []ssh.AuthMethod{ssh.Password(cfg.Client.Token)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	for {
		target := fmt.Sprintf("%s:%d", cfg.Client.Addr, cfg.Client.Port)
		log.Printf("Connecting to %s...", target)
		
		client, err := ssh.Dial("tcp", target, sshConfig)
		if err != nil {
			log.Printf("Dial error: %v. Retry in 5s...", err)
			time.Sleep(5 * time.Second)
			continue
		}

		go func() {
			t := time.NewTicker(20 * time.Second)
			defer t.Stop()
			for range t.C {
				if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
					client.Close()
					return
				}
			}
		}()

		for _, t := range cfg.Client.Tunnels {
			go func(tunnel TunnelConfig) {
				l, err := client.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", tunnel.RemotePort))
				if err != nil {
					log.Printf("Tunnel [%s] failed: %v", tunnel.Name, err)
					return
				}
				log.Printf("SUCCESS [%s]: :%d -> %s:%d", tunnel.Name, l.Addr().(*net.TCPAddr).Port, tunnel.LocalIP, tunnel.LocalPort)

				for {
					remote, err := l.Accept()
					if err != nil { break }
					go func(r net.Conn, tc TunnelConfig) {
						local, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", tc.LocalIP, tc.LocalPort), 5*time.Second)
						if err != nil {
							r.Close()
							return
						}
						copyBidirectional(r, local)
					}(remote, tunnel)
				}
			}(t)
		}
		client.Wait()
		log.Printf("Connection lost. Reconnecting...")
		time.Sleep(5 * time.Second)
	}
}

func main() {
	serverCmd := flag.NewFlagSet("server", flag.ExitOnError)
	sPort := serverCmd.Int("port", 7835, "Listen port")
	sToken := serverCmd.String("token", "", "Auth token")

	clientCmd := flag.NewFlagSet("client", flag.ExitOnError)
	confPath := clientCmd.String("c", "config.yaml", "Path to config file")

	if len(os.Args) < 2 {
		fmt.Printf("tbore v%s\nUsage: tbore <server|client> [options]\n", version)
		return
	}

	switch os.Args[1] {
	case "server":
		serverCmd.Parse(os.Args[2:])
		runServer(*sPort, *sToken)
	case "client":
		clientCmd.Parse(os.Args[2:])
		data, err := os.ReadFile(*confPath)
		if err != nil { log.Fatal(err) }
		var cfg Config
		yaml.Unmarshal(data, &cfg)
		runClient(cfg)
	default:
		log.Fatal("Unknown command")
	}
}