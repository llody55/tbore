package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"tbore/pkg/client"
	"tbore/pkg/config"
	"tbore/pkg/server"
	"tbore/pkg/version"
)

const defaultSocketPath = "/var/run/tbore.sock"
const defaultClientSocketPath = "/var/run/tbore-client.sock"

func main() {
	serverCmd := flag.NewFlagSet("server", flag.ExitOnError)
	sConfig := serverCmd.String("c", "server.yaml", "Path to server config file")

	clientCmd := flag.NewFlagSet("client", flag.ExitOnError)
	cConfig := clientCmd.String("c", "client.yaml", "Path to client config file")

	reloadCmd := flag.NewFlagSet("reload", flag.ExitOnError)
	reloadSocket := reloadCmd.String("s", defaultClientSocketPath, "Path to client control socket")

	statusCmd := flag.NewFlagSet("status", flag.ExitOnError)
	statusSocket := statusCmd.String("s", defaultSocketPath, "Path to tbore socket")

	clientStatusCmd := flag.NewFlagSet("client-status", flag.ExitOnError)
	clientStatusSocket := clientStatusCmd.String("s", defaultClientSocketPath, "Path to client control socket")

	if len(os.Args) < 2 {
		printUsage()
		return
	}

	switch os.Args[1] {
	case "server":
		serverCmd.Parse(os.Args[2:])
		runServer(*sConfig)
	case "client":
		clientCmd.Parse(os.Args[2:])
		runClient(*cConfig)
	case "reload":
		reloadCmd.Parse(os.Args[2:])
		runReload(*reloadSocket)
	case "status":
		statusCmd.Parse(os.Args[2:])
		runStatus(*statusSocket)
	case "client-status":
		clientStatusCmd.Parse(os.Args[2:])
		runClientStatus(*clientStatusSocket)
	default:
		printUsage()
	}
}

func runServer(configPath string) {
	cfg, err := config.LoadServerConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load server config: %v", err)
	}

	srv, err := server.NewServer(cfg)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	go startStatusServer(srv)

	log.Fatal(srv.Start())
}

func startStatusServer(srv *server.Server) {
	socketPath := defaultSocketPath
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Printf("Failed to create status socket: %v", err)
		return
	}

	log.Printf("Status server listening on %s", socketPath)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}

		go func(c net.Conn) {
			defer c.Close()
			status := srv.GetStatus()
			c.Write([]byte(status.String()))
		}(conn)
	}
}

func runStatus(socketPath string) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		log.Fatalf("Failed to connect to tbore server: %v\nIs the server running?", err)
	}
	defer conn.Close()

	buf := make([]byte, 65536)
	n, err := conn.Read(buf)
	if err != nil {
		log.Fatalf("Failed to read status: %v", err)
	}

	fmt.Print(string(buf[:n]))
}

func runClient(configPath string) {
	cfg, err := config.LoadClientConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load client config: %v", err)
	}

	cli, err := client.NewClient(cfg, configPath)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	go startClientControlServer(cli)

	cli.Start()
}

func startClientControlServer(cli *client.Client) {
	socketPath := defaultClientSocketPath
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Printf("Failed to create client control socket: %v", err)
		return
	}

	log.Printf("Client control server listening on %s", socketPath)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}

		go func(c net.Conn) {
			defer c.Close()

			buf := make([]byte, 1024)
			n, err := c.Read(buf)
			if err != nil {
				return
			}

			command := strings.TrimSpace(string(buf[:n]))

			switch command {
			case "reload":
				cli.TriggerReload()
				c.Write([]byte("Reload triggered\n"))
			case "status":
				status := cli.GetTunnelStatus()
				var sb strings.Builder
				for _, t := range status {
					active := "INACTIVE"
					if t.Active {
						active = "ACTIVE"
					}
					sb.WriteString(fmt.Sprintf("%s: :%d -> %s:%d [%s]\n",
						t.Name, t.RemotePort, t.LocalIP, t.LocalPort, active))
				}
				c.Write([]byte(sb.String()))
			default:
				c.Write([]byte("Unknown command\n"))
			}
		}(conn)
	}
}

func runReload(socketPath string) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		log.Fatalf("Failed to connect to client: %v\nIs the client running?", err)
	}
	defer conn.Close()

	conn.Write([]byte("reload"))

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	fmt.Print(string(buf[:n]))
}

func runClientStatus(socketPath string) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		log.Fatalf("Failed to connect to client: %v\nIs the client running?", err)
	}
	defer conn.Close()

	conn.Write([]byte("status"))

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	fmt.Print(string(buf[:n]))
}

func printUsage() {
	fmt.Printf("tbore v%s\n", version.Version)
	fmt.Println("Usage:")
	fmt.Println("  tbore server -c <config.yaml>")
	fmt.Println("  tbore client -c <config.yaml>")
	fmt.Println("  tbore reload [-s <socket_path>]")
	fmt.Println("  tbore status [-s <socket_path>]")
	fmt.Println("  tbore client-status [-s <socket_path>]")
}
