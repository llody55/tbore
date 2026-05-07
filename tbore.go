package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"tbore/pkg/client"
	"tbore/pkg/config"
	"tbore/pkg/server"
)

const version = "0.4.0"

func main() {
	serverCmd := flag.NewFlagSet("server", flag.ExitOnError)
	sConfig := serverCmd.String("c", "server.yaml", "Path to server config file")

	clientCmd := flag.NewFlagSet("client", flag.ExitOnError)
	cConfig := clientCmd.String("c", "client.yaml", "Path to client config file")

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

	log.Fatal(srv.Start())
}

func runClient(configPath string) {
	cfg, err := config.LoadClientConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load client config: %v", err)
	}

	cli := client.NewClient(cfg)
	cli.Start()
}

func printUsage() {
	fmt.Printf("tbore v%s\n", version)
	fmt.Println("Usage:")
	fmt.Println("  tbore server -c <config.yaml>")
	fmt.Println("  tbore client -c <config.yaml>")
}
