package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"

	"tbore/pkg/client"
	"tbore/pkg/config"
	"tbore/pkg/server"
	"tbore/pkg/version"
)

const defaultSocketPath = "/var/run/tbore.sock"

func main() {
	serverCmd := flag.NewFlagSet("server", flag.ExitOnError)
	sConfig := serverCmd.String("c", "server.yaml", "Path to server config file")

	clientCmd := flag.NewFlagSet("client", flag.ExitOnError)
	cConfig := clientCmd.String("c", "client.yaml", "Path to client config file")

	statusCmd := flag.NewFlagSet("status", flag.ExitOnError)
	statusSocket := statusCmd.String("s", defaultSocketPath, "Path to tbore socket")

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
	case "status":
		statusCmd.Parse(os.Args[2:])
		runStatus(*statusSocket)
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

	cli, err := client.NewClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	cli.Start()
}

func printUsage() {
	fmt.Printf("tbore v%s\n", version.Version)
	fmt.Println("Usage:")
	fmt.Println("  tbore server -c <config.yaml>")
	fmt.Println("  tbore client -c <config.yaml>")
	fmt.Println("  tbore status [-s <socket_path>]")
}
