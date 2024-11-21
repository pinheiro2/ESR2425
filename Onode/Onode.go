package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
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
	clients             []net.Addr
	clientsMu           sync.Mutex // Mutex to protect the client list
	streamConnectionsIn map[string]*net.UDPConn
	streamConnMu        sync.Mutex // Mutex to protect streamConnectionsIn

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

func prepareFFmpegCommands(videos map[string]string) (map[string]*exec.Cmd, error) {
	ffmpegMap := make(map[string]*exec.Cmd)

	for name, videoPath := range videos {
		ffmpegCmd := exec.Command("ffmpeg",
			"-i", videoPath, // Input file
			"-f", "image2pipe", // Output format for piping images
			"-vcodec", "mjpeg", // Encode as JPEG
			"-q:v", "2", // Quality (lower is better)
			"pipe:1") // Output to stdout

		ffmpegMap[name] = ffmpegCmd
	}

	return ffmpegMap, nil
}

func startFFmpeg(ffmpegMap map[string]*exec.Cmd, stream string) (*bufio.Reader, func(), error) {
	ffmpegCmd, exists := ffmpegMap[stream]
	if !exists {
		return nil, nil, fmt.Errorf("stream %s not found in ffmpeg map", stream)
	}

	ffmpegOut, err := ffmpegCmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get ffmpeg stdout for stream %s: %w", stream, err)
	}

	if err := ffmpegCmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start ffmpeg for stream %s: %w", stream, err)
	}

	log.Printf("Started streaming %s", stream)

	cleanup := func() {
		ffmpegOut.Close()
		ffmpegCmd.Wait()
	}

	return bufio.NewReader(ffmpegOut), cleanup, nil
}

// Starts ffmpeg to output video frames as JPEGs for Content Server
func startFFmpeg_old(video string) (*bufio.Reader, func(), error) {
	ffmpegCmd := exec.Command("ffmpeg",
		"-i", video, // Input file
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
func setupUDPListener(ip string, port int) (*net.UDPConn, error) {
	addr := net.UDPAddr{
		Port: port,
		IP:   net.ParseIP(ip),
	}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		return nil, fmt.Errorf("failed to set up UDP listener: %w", err)
	}
	log.Printf("Listening for UDP connections on port %d\n", port)
	return conn, nil
}

func handleClientConnectionsPOP(protocolConn *net.UDPConn, streamFrom map[string]string) {
	// Initialize the map if it's nil
	if streamConnectionsIn == nil {
		streamConnectionsIn = make(map[string]*net.UDPConn)
	}

	buf := make([]byte, 1024)
	for {
		n, clientAddr, err := protocolConn.ReadFrom(buf)
		if err != nil || n == 0 {
			log.Printf("Error reading client connection request: %v", err)
			continue
		}

		// Read the message from the buffer as a string
		clientMessage := string(buf[:n])
		log.Printf("Received message from client %s: %s", clientAddr, clientMessage)

		// Split the message into parts by whitespace
		parts := strings.Fields(clientMessage)
		if len(parts) == 0 {
			log.Printf("Received empty message from client %s", clientAddr)
			continue
		}

		// Parse the command and handle each case
		command := parts[0]
		switch command {
		case "CONNECT":
			log.Printf("CONNECT request from client %s", clientAddr)

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

		case "REQUEST":
			if len(parts) < 2 {
				log.Printf("REQUEST command from client %s is missing a video name", clientAddr)
				continue
			}
			contentName := parts[1]
			log.Printf("REQUEST for content \"%s\" from client %s", contentName, clientAddr)

			/*********************/

			streamConnIn, err := setupUDPConnection(streamFrom[contentName], 8000)

			if err != nil {
				log.Fatalf("Error setting up UDP connection to %s (%s): %v", err)
			}

			// Protect access to streamConnectionsIn
			streamConnMu.Lock()
			streamConnectionsIn[contentName] = streamConnIn
			streamConnMu.Unlock()

			defer streamConnIn.Close()

			/*********************/

			// Send the content request to the appropriate stream connection
			err = sendContentRequest(streamConnectionsIn[contentName], contentName)
			if err != nil {
				log.Printf("Failed to request content \"%s\" for client %s: %v", contentName, clientAddr, err)
				continue // Skip forwarding if content request fails
			}

			go forwardToClient(protocolConn, streamConnectionsIn[contentName], clientAddr)

		default:
			log.Printf("Unknown command from client %s: %s", clientAddr, clientMessage)
			// Optionally handle unknown messages, send error responses, etc.
		}
	}
}

func sendContentRequest(conn *net.UDPConn, contentName string) error {

	if conn == nil {
		return fmt.Errorf("connection is nil; cannot send request for content: %s", contentName)
	}
	// Prefix the content name with "Request:"
	message := "REQUEST " + contentName

	// Send the request message
	_, err := conn.Write([]byte(message))
	if err != nil {
		return fmt.Errorf("failed to send content name: %w", err)
	}
	log.Printf("Requested content: %s\n", contentName)
	return nil
}

func handleClientConnectionsCS(conn *net.UDPConn, streams map[string]*bufio.Reader, ffmpegCommands map[string]*exec.Cmd) {
	buf := make([]byte, 1024)
	for {
		n, clientAddr, err := conn.ReadFrom(buf)
		if err != nil || n == 0 {
			log.Printf("Error reading client connection request: %v", err)
			continue
		}

		// Read the message from the buffer as a string
		clientMessage := string(buf[:n])
		log.Printf("Received message from client %s: %s", clientAddr, clientMessage)

		// Split the message into parts by whitespace
		parts := strings.Fields(clientMessage)
		if len(parts) == 0 {
			log.Printf("Received empty message from client %s", clientAddr)
			continue
		}

		// Parse the command and handle each case
		command := parts[0]
		switch command {
		case "CONNECT":
			log.Printf("CONNECT request from client %s", clientAddr)

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

		case "REQUEST":
			if len(parts) < 2 {
				log.Printf("REQUEST command from client %s is missing a video name", clientAddr)
				continue
			}
			contentName := parts[1]
			log.Printf("REQUEST for content \"%s\" from client %s", contentName, clientAddr)
			// Handle content request here (e.g., start sending content or fetch the requested item)
			if streams[contentName] == nil {
				reader, cleanup, err := startFFmpeg(ffmpegCommands, contentName)
				if err != nil {
					log.Fatalf("Error initializing ffmpeg: %v", err)
				}
				streams[contentName] = reader

				defer cleanup()
			}

			go sendRTPPackets(conn, streams[contentName], clientAddr)
		default:
			log.Printf("Unknown command from client %s: %s", clientAddr, clientMessage)
		}
	}
}

// Sends RTP packets to a specific client with logging
func sendRTPPackets(conn *net.UDPConn, reader *bufio.Reader, targetClient net.Addr) {
	seqNumber := uint16(0)
	ssrc := uint32(1234)
	payloadType := uint8(96) // Dynamic payload type for video
	maxBufferSize := 65535   // Maximum allowable RTP payload size

	for {
		// Read one frame from the reader
		var buf bytes.Buffer
		for {
			b, err := reader.ReadByte()
			if err != nil {
				if err == io.EOF {
					log.Println("End of stream reached.")
				} else {
					log.Printf("Error reading byte: %v", err)
				}
				return
			}

			buf.WriteByte(b)

			// Detect end of JPEG frame (FF D9 marks the end of a JPEG frame)
			if buf.Len() > 2 && buf.Bytes()[buf.Len()-2] == 0xFF && buf.Bytes()[buf.Len()-1] == 0xD9 {
				break
			}

			// Check buffer size to prevent overflows
			if buf.Len() > maxBufferSize {
				log.Printf("Frame exceeds max buffer size (%d bytes). Discarding.", maxBufferSize)
				return
			}
		}

		// Construct the RTP packet
		packet := &rtp.Packet{
			Header: rtp.Header{
				Marker:         true, // Indicates the end of a frame
				PayloadType:    payloadType,
				SequenceNumber: seqNumber,
				Timestamp:      uint32(time.Now().UnixNano() / 1e6), // Current timestamp in milliseconds
				SSRC:           ssrc,                                // Synchronization source identifier
			},
			Payload: buf.Bytes(),
		}

		// Marshal the RTP packet into bytes
		packetData, err := packet.Marshal()
		if err != nil {
			log.Printf("Failed to marshal RTP packet: %v", err)
			continue // Skip this frame if marshaling fails
		}

		// Send the packet to the specific target client
		_, err = conn.WriteTo(packetData, targetClient)
		if err != nil {
			log.Printf("Failed to send packet to %v: %v", targetClient, err)
		} else {
			// Log packet details after successful send
			log.Printf("Sent RTP packet to %v - Seq=%d, Timestamp=%d, Size=%d bytes",
				targetClient, packet.SequenceNumber, packet.Timestamp, len(packet.Payload))
		}

		// Increment sequence number for the next packet
		seqNumber++

		// Wait for the next frame (approximately 30 FPS)
		time.Sleep(time.Millisecond * 33)
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
	// _, err = conn.Write([]byte("CONNECT"))
	// if err != nil {
	// 	conn.Close()
	// 	return nil, fmt.Errorf("failed to send connection request: %w", err)
	// }
	fmt.Println("Sent connection request to server")
	return conn, nil
}

// Forwards data from content server to a specific client (used by POP)
func forwardToClient(conn *net.UDPConn, contentConn *net.UDPConn, targetClient net.Addr) {
	buf := make([]byte, 150000)
	for {
		n, _, err := contentConn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("Error reading from Content Server: %v", err)
			return
		}

		// Forward packet only to the specified target client
		_, err = conn.WriteTo(buf[:n], targetClient)
		if err != nil {
			log.Printf("Failed to forward packet to %v: %v", targetClient, err)
		} else {
			// Optional: log the forwarding event
			//log.Printf("POP forwarded packet to %v - Size=%d bytes", targetClient, n)
		}
	}
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

		//log.Printf("POP received packet from Content Server - Size=%d bytes", n)

		clientsMu.Lock()
		for _, clientAddr := range clients {
			_, err := conn.WriteTo(buf[:n], clientAddr)
			if err != nil {
				log.Printf("Failed to forward packet to %v: %v", clientAddr, err)
			} else {
				//log.Printf("POP forwarded packet to %v - Size=%d bytes", clientAddr, n)
			}
		}
		clientsMu.Unlock()
	}
}

func LoadJSONToMap(filename string, data map[string]string) error {
	// Lê o conteúdo do ficheiro diretamente para byte slice
	byteValue, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("erro ao abrir o ficheiro: %w", err)
	}

	// Decodifica o JSON para o map fornecido
	if err := json.Unmarshal(byteValue, &data); err != nil {
		return fmt.Errorf("erro ao decodificar o JSON: %w", err)
	}

	return nil
}

func main() {
	// Define flags for node name, UDP port, and node type
	nodeName := flag.String("name", "", "Node name")
	ip := flag.String("ip", "0.0.0.0", "IP to open on for testing")
	port := flag.Int("port", 8000, "UDP port to listen on")
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

	// Print all neighbors
	log.Printf("Node %s initialized with neighbors: %v", node.Name, node.Neighbors)

	switch node.Type {
	case "POP":

		// connections := make(map[string]*net.UDPConn)

		// for neighborName, neighborIP := range node.Neighbors {
		// 	infoConn, err := setupUDPConnection(neighborIP, 8000)
		// 	if err != nil {
		// 		log.Fatalf("Error setting up UDP connection to %s (%s): %v", neighborName, neighborIP, err)
		// 	}
		// 	log.Printf("POP connected to %s at %s", neighborName, infoConn.RemoteAddr())
		// 	connections[neighborName] = infoConn
		// 	defer infoConn.Close() // Remember to close these later
		// }

		streamFrom := make(map[string]string)

		// Add entries to the map
		// TODO: arvore de distribuição aqui
		streamFrom["stream1"] = node.Neighbors["S1"]
		streamFrom["stream2"] = node.Neighbors["S1"]
		streamFrom["stream3"] = node.Neighbors["S1"]

		// // streamConnectionsOut := make(map[string]*net.UDPConn)

		// for stream, neighborIP := range streamFrom {
		// 	streamConnIn, err := setupUDPConnection(neighborIP, 8000)
		// 	// streamConnOut, err := setupUDPConnection(neighbor, 8000)
		// 	if err != nil {
		// 		log.Fatalf("Error setting up UDP connection to %s (%s): %v", err)
		// 	}
		// 	// log.Printf("POP connected to %s at %s", neighborName, infoConn.RemoteAddr())
		// 	streamConnectionsIn[stream] = streamConnIn
		// 	// streamConnectionsOut[stream] = streamConnOut

		// 	cleanup := func() {
		// 		streamConnIn.Close() // Remember to close these later
		// 		// streamConnOut.Close() // Remember to close these later
		// 	}
		// 	defer cleanup()
		// }

		// abrir porta udp para escuta de pedidos
		protocolConn, err := setupUDPListener(*ip, node.Port)
		if err != nil {
			log.Fatalf("Error setting up UDP listener: %v", err)
		}
		defer protocolConn.Close()

		// esperar por conexao
		go handleClientConnectionsPOP(protocolConn, streamFrom)

		select {}
	case "Node":

		fmt.Printf("Node %s initialized as a regular node with neighbors: %v\n", node.Name, node.Neighbors)

	case "CS":
		// Content Server: Stream video frames to any connecting POP nodes

		videos := make(map[string]string)
		streams := make(map[string]*bufio.Reader)
		err := LoadJSONToMap("streams.json", videos)
		ffmpegCommands, err := prepareFFmpegCommands(videos)
		if err != nil {
			log.Fatalf("Error creating ffmpeg commands for streams: %v", err)
		}

		conn, err := setupUDPListener(*ip, node.Port)
		if err != nil {
			log.Fatalf("Error setting up UDP listener: %v", err)
		}
		defer conn.Close()

		go handleClientConnectionsCS(conn, streams, ffmpegCommands)

		select {}

	default:
		log.Fatalf("Unknown node type: %s", node.Type)
	}
}
