package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os/exec"
	"sync"
	"time"

	"github.com/pion/rtp"
)

// Node structure to store node information and its neighbors
type Node struct {
	Name      string            // Node name
	Type      string            // Node type (POP, Node, Content Server)
	Neighbors map[string]string // Neighbors map: key is the node name, value is the IP
	Port      int               // UDP port for the node
}

var (
	clients   []net.Addr
	clientsMu sync.Mutex // Mutex to protect the client list
)

// Initializes the node and retrieves the neighbor list from the bootstrap server
func (node *Node) initialize(bootstrapAddress string) {
	conn, err := net.Dial("tcp", bootstrapAddress)
	if err != nil {
		log.Fatal("Error connecting to bootstrapper:", err)
	}
	defer conn.Close()

	// Send the node name to the bootstrap server
	_, err = conn.Write([]byte(node.Name))
	if err != nil {
		log.Fatal("Error sending node name:", err)
	}

	// Receive the list of neighbors (map of name -> IP) in JSON
	buffer := make([]byte, 4096)
	n, err := conn.Read(buffer)
	if err != nil {
		log.Fatal("Error reading response from bootstrapper:", err)
	}

	// Deserialize the JSON response to obtain the neighbor map
	var neighbors map[string]string
	if err := json.Unmarshal(buffer[:n], &neighbors); err != nil {
		log.Fatal("Error deserializing response:", err)
	}

	node.Neighbors = neighbors
	fmt.Printf("Node %s (Type: %s) - Stored neighbors: %v\n", node.Name, node.Type, node.Neighbors)
}

// Starts ffmpeg to output video frames as JPEGs for Content Server
func startFFmpeg() (*bufio.Reader, func(), error) {
	ffmpegCmd := exec.Command("ffmpeg",
		"-i", "video_min_360.mp4", // Input file
		"-f", "image2pipe", // Output format for piping images
		"-vcodec", "mjpeg", // Encode as JPEG
		"-q:v", "2", // Quality (lower is better)
		"pipe:1") // Output to stdout

	ffmpegOut, err := ffmpegCmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get ffmpeg stdout: %w", err)
	}

	if err := ffmpegCmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	cleanup := func() {
		ffmpegOut.Close()
		ffmpegCmd.Wait()
	}

	return bufio.NewReader(ffmpegOut), cleanup, nil
}

// Sets up the UDP listener on the specified port
func setupUDPListener(port int) (*net.UDPConn, error) {
	addr := net.UDPAddr{
		Port: port,
		IP:   net.ParseIP("localhost"),
	}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		return nil, fmt.Errorf("failed to set up UDP listener: %w", err)
	}
	log.Printf("Listening for UDP connections on port %d\n", port)
	return conn, nil
}

// Handles client connections by listening for connection requests
func handleClientConnections(conn *net.UDPConn) {
	buf := make([]byte, 1024)
	for {
		n, clientAddr, err := conn.ReadFrom(buf)
		if err != nil || n == 0 {
			log.Printf("Error reading client connection request: %v", err)
			continue
		}

		// Log every client connection attempt
		log.Printf("Connection attempt from client at %s", clientAddr)

		// Add the client address to the list if it's new
		clientsMu.Lock()
		found := false
		for _, c := range clients {
			if c.String() == clientAddr.String() {
				found = true
				break
			}
		}
		if !found {
			clients = append(clients, clientAddr)
			log.Printf("New client connected from %s", clientAddr)
		} else {
			log.Printf("Existing client %s reconnected", clientAddr)
		}
		clientsMu.Unlock()
	}
}

// Sends RTP packets to connected clients with logging
func sendRTPPackets(conn *net.UDPConn, reader *bufio.Reader) {
	seqNumber := uint16(0)
	ssrc := uint32(1234)
	payloadType := uint8(96) // Dynamic payload type for video

	for {
		var buf bytes.Buffer
		for {
			b, err := reader.ReadByte()
			if err != nil {
				log.Println("End of video or error reading frame:", err)
				return
			}
			buf.WriteByte(b)
			if buf.Len() > 2 && buf.Bytes()[buf.Len()-2] == 0xFF && buf.Bytes()[buf.Len()-1] == 0xD9 {
				break
			}
		}

		packet := &rtp.Packet{
			Header: rtp.Header{
				Marker:         true,
				PayloadType:    payloadType,
				SequenceNumber: seqNumber,
				Timestamp:      uint32(time.Now().UnixNano() / 1e6),
				SSRC:           ssrc,
			},
			Payload: buf.Bytes(),
		}

		packetData, err := packet.Marshal()
		if err != nil {
			log.Fatalf("Failed to marshal RTP packet: %v", err)
		}

		clientsMu.Lock()
		for _, clientAddr := range clients {
			_, err := conn.WriteTo(packetData, clientAddr)
			if err != nil {
				log.Printf("Failed to send packet to %v: %v", clientAddr, err)
			} else {
				// Log packet details after successful send
				log.Printf("Sent RTP packet to %v - Seq=%d, Timestamp=%d, Size=%d bytes",
					clientAddr, packet.SequenceNumber, packet.Timestamp, len(packet.Payload))
			}
		}
		clientsMu.Unlock()

		seqNumber++
		time.Sleep(time.Millisecond * 33) // Approx. 30 FPS
	}
}

func setupUDPConnection(serverIP string, port int) (*net.UDPConn, error) {
	serverAddr := net.UDPAddr{
		Port: port,
		IP:   net.ParseIP(serverIP),
	}
	conn, err := net.DialUDP("udp", nil, &serverAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}

	// Send initial connection request
	_, err = conn.Write([]byte("CONNECT"))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send connection request: %w", err)
	}
	fmt.Println("Sent connection request to server")
	return conn, nil
}

// Forwards data from content server to connected clients (used by POP)
func forwardToClients(conn *net.UDPConn, contentConn *net.UDPConn) {
	buf := make([]byte, 150000)
	for {
		n, _, err := contentConn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("Error reading from Content Server: %v", err)
			return
		}

		log.Printf("POP received packet from Content Server - Size=%d bytes", n)

		clientsMu.Lock()
		for _, clientAddr := range clients {
			_, err := conn.WriteTo(buf[:n], clientAddr)
			if err != nil {
				log.Printf("Failed to forward packet to %v: %v", clientAddr, err)
			} else {
				log.Printf("POP forwarded packet to %v - Size=%d bytes", clientAddr, n)
			}
		}
		clientsMu.Unlock()
	}
}

func main() {
	// Define flags for node name, UDP port, and node type
	nodeName := flag.String("name", "", "Node name")
	port := flag.Int("port", 30000, "UDP port to listen on")
	nodeType := flag.String("type", "Node", "Node type (POP, Node, CS)")

	flag.Parse()

	if *nodeName == "" {
		log.Fatal("Node name not provided. Usage: go run node.go -name <NodeName> -port <Port> -type <Type>")
	}

	node := Node{
		Name:      *nodeName,
		Type:      *nodeType,
		Neighbors: make(map[string]string),
		Port:      *port,
	}

	// Initialize node and retrieve neighbors
	node.initialize("localhost:8080") // Replace "localhost" with the bootstrap server IP if needed

	switch node.Type {
	case "POP":
		// POP node: Connect to Content Server and forward stream to clients
		contentConn, err := setupUDPConnection("localhost", 4000)
		if err != nil {
			log.Fatalf("Error setting up UDP connection: %v", err)
		}
		log.Printf("POP connected to Content Server at %s", contentConn.RemoteAddr())
		defer contentConn.Close()

		conn, err := setupUDPListener(node.Port)
		if err != nil {
			log.Fatalf("Error setting up UDP listener: %v", err)
		}
		defer conn.Close()

		go handleClientConnections(conn)
		forwardToClients(conn, contentConn)

	case "Node":
		// Regular node: Just print neighbors
		fmt.Printf("Node %s initialized as a regular node with neighbors: %v\n", node.Name, node.Neighbors)

	case "CS":
		// Content Server: Stream video frames to any connecting POP nodes
		reader, cleanup, err := startFFmpeg()
		if err != nil {
			log.Fatalf("Error initializing ffmpeg: %v", err)
		}
		defer cleanup()

		conn, err := setupUDPListener(node.Port)
		if err != nil {
			log.Fatalf("Error setting up UDP listener: %v", err)
		}
		defer conn.Close()

		// Log each new client connection
		go handleClientConnections(conn)

		sendRTPPackets(conn, reader)

	default:
		log.Fatalf("Unknown node type: %s", node.Type)
	}
}
