// tbore: 基于 SSH 隧道的内网穿透工具 (Transparent Bore)
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
)

const version = "0.2.1"

// ====================== 通用工具 ======================

func copyBidirectional(src, dst io.ReadWriteCloser) {
	defer src.Close()
	defer dst.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(dst, src)
	}()
	go func() {
		defer wg.Done()
		io.Copy(src, dst)
	}()
	wg.Wait()
}

func generateSigner() (ssh.Signer, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return ssh.ParsePrivateKey(pem.EncodeToMemory(privateKeyPEM))
}

// ====================== Server 端逻辑 ======================

func runServer(port int, token string) {
	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if token != "" && string(pass) != token {
				return nil, fmt.Errorf("auth failed")
			}
			return nil, nil
		},
	}

	signer, err := generateSigner()
	if err != nil {
		log.Fatal(err)
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("tbore server v%s listening on :%d", version, port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}

		go func(nConn net.Conn) {
			sConn, chans, reqs, err := ssh.NewServerConn(nConn, config)
			if err != nil {
				return
			}
			defer sConn.Close()

			// 1. 处理全局请求 (tcpip-forward 在这里)
			go handleServerRequests(sConn, reqs)

			// 2. 修正：手动处理并拒绝通道请求，不再调用 ssh.DiscardRequests(chans)
			go func(in <-chan ssh.NewChannel) {
				for newChan := range in {
					newChan.Reject(ssh.Prohibited, "direct channel access not allowed")
				}
			}(chans)

			sConn.Wait()
		}(conn)
	}
}

func handleServerRequests(sConn *ssh.ServerConn, reqs <-chan *ssh.Request) {
	for req := range reqs {
		if req.Type == "tcpip-forward" {
			var msg struct {
				Addr string
				Port uint32
			}
			ssh.Unmarshal(req.Payload, &msg)

			ln, err := net.Listen("tcp", "0.0.0.0:0")
			if err != nil {
				req.Reply(false, nil)
				continue
			}

			_, portStr, _ := net.SplitHostPort(ln.Addr().String())
			var actualPort uint32
			fmt.Sscanf(portStr, "%d", &actualPort)

			resp := make([]byte, 4)
			binary.BigEndian.PutUint32(resp, actualPort)
			req.Reply(true, resp)

			log.Printf("Tunnel opened for %s on port %d", sConn.RemoteAddr(), actualPort)

			go func(l net.Listener, p uint32) {
				defer l.Close()
				for {
					userConn, err := l.Accept()
					if err != nil {
						return
					}

					go func(u net.Conn) {
						defer u.Close()
						addr, portStr, _ := net.SplitHostPort(u.RemoteAddr().String())
						var port uint32
						fmt.Sscanf(portStr, "%d", &port)

						payload := struct {
							Addr  string
							Port  uint32
							OAddr string
							OPort uint32
						}{
							Addr:  "0.0.0.0",
							Port:  p,
							OAddr: addr,
							OPort: port,
						}

						ch, reqs, err := sConn.OpenChannel("forwarded-tcpip", ssh.Marshal(&payload))
						if err != nil {
							return
						}
						go ssh.DiscardRequests(reqs)
						copyBidirectional(u, ch)
					}(userConn)
				}
			}(ln, actualPort)
		} else {
			req.Reply(false, nil)
		}
	}
}

// ====================== Client 端逻辑 ======================

func runClient(serverAddr string, serverPort int, localPort int, token string) {
	config := &ssh.ClientConfig{
		User:            "tbore",
		Auth:            []ssh.AuthMethod{ssh.Password(token)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	for {
		client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", serverAddr, serverPort), config)
		if err != nil {
			log.Printf("Dial error: %v, retry in 5s", err)
			time.Sleep(5 * time.Second)
			continue
		}

		l, err := client.Listen("tcp", "0.0.0.0:0")
		if err != nil {
			log.Printf("Listen error: %v", err)
			client.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		log.Printf("SUCCESS: Tunnel established!")
		log.Printf("Public Access -> %s:%d", serverAddr, l.Addr().(*net.TCPAddr).Port)

		for {
			remoteConn, err := l.Accept()
			if err != nil {
				break
			}
			go func(r net.Conn) {
				localConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
				if err != nil {
					r.Close()
					return
				}
				copyBidirectional(r, localConn)
			}(remoteConn)
		}
		client.Close()
		time.Sleep(5 * time.Second)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("tbore v%s\nUsage: tbore <server|client> [options]\n", version)
		os.Exit(1)
	}
	cmd := os.Args[1]
	switch cmd {
	case "server":
		f := flag.NewFlagSet("server", flag.ExitOnError)
		port := f.Int("port", 7835, "Listen port")
		token := f.String("token", "", "Auth token")
		f.Parse(os.Args[2:])
		runServer(*port, *token)
	case "client":
		f := flag.NewFlagSet("client", flag.ExitOnError)
		to := f.String("to", "", "Server IP")
		port := f.Int("port", 7835, "Server port")
		local := f.Int("local-port", 8080, "Local port")
		token := f.String("token", "", "Auth token")
		f.Parse(os.Args[2:])
		runClient(*to, *port, *local, *token)
	}
}
