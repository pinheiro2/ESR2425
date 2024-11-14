package main

import (
	"fmt"
	"net"
	"os"
)

// startUDPServer starts a simple UDP server that listens for ping messages and responds with pong
func startUDPServer(address string) {
	// Listen for incoming UDP packets
	ln, err := net.ListenPacket("udp", address)
	if err != nil {
		fmt.Printf("Error listening on %s: %v\n", address, err)
		os.Exit(1)
	}
	defer ln.Close()

	fmt.Printf("Listening on %s...\n", address)

	buffer := make([]byte, 1024)

	for {
		// Read incoming data from the UDP connection
		n, addr, err := ln.ReadFrom(buffer)
		if err != nil {
			fmt.Printf("Error reading from UDP connection: %v\n", err)
			continue
		}

		// Print the received message
		fmt.Printf("Received message: %s from %s\n", string(buffer[:n]), addr)

		// Send a response back
		response := []byte("pong")
		_, err = ln.WriteTo(response, addr)
		if err != nil {
			fmt.Printf("Error sending response to %s: %v\n", addr, err)
		}
	}
}

func main() {
	// Read the port from the command-line arguments
	if len(os.Args) != 2 {
		fmt.Println("Usage: go run udp_server.go <port>")
		os.Exit(1)
	}

	port := os.Args[1]

	// Construct the address with localhost and the given port
	address := "localhost:" + port

	// Start the server with the specified port
	go startUDPServer(address)

	// Prevent the main function from exiting
	select {}
}
