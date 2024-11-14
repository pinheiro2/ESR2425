package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/pion/rtp"
)

type Node struct {
	Address       string `json:"address"`
	ResponseTimes []time.Duration
	AverageTime   time.Duration
	Jitter        time.Duration
	SuccessCount  int
	TotalCount    int
	Score         float64
}

func loadNodesFromFile(filename string) ([]*Node, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %v", err)
	}

	// Unmarshal into a map to match the structure of the input JSON.
	var nodeMap map[string]string
	err = json.Unmarshal(data, &nodeMap)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %v", err)
	}

	// Convert the map to a slice of Node pointers, only setting the Address field.
	var nodes []*Node
	for _, address := range nodeMap {
		node := &Node{
			Address: address,
		}
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// calculateJitter calculates the jitter for a node's response times
func calculateJitter(node *Node) time.Duration {
	if len(node.ResponseTimes) <= 1 {
		return 0
	}

	var totalJitter time.Duration
	for i := 1; i < len(node.ResponseTimes); i++ {
		// Calculate the difference between consecutive response times
		difference := node.ResponseTimes[i] - node.ResponseTimes[i-1]
		if difference < 0 {
			difference = -difference
		}
		totalJitter += difference
	}

	// Return the average jitter (difference) between consecutive response times
	return totalJitter / time.Duration(len(node.ResponseTimes)-1)
}

// measureNodeResponse sends a UDP message to the node and measures the response time.
func measureNodeResponse(node *Node, wg *sync.WaitGroup) {
	defer wg.Done()

	// Create a UDP connection
	conn, err := net.Dial("udp", node.Address)
	if err != nil {
		fmt.Printf("Failed to connect to %s: %v\n", node.Address, err)
		return
	}
	defer conn.Close()

	message := []byte("ping")
	start := time.Now()

	// Send a UDP message
	_, err = conn.Write(message)
	if err != nil {
		fmt.Printf("Failed to send message to %s: %v\n", node.Address, err)
		return
	}

	// Set a read deadline for receiving a response
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buffer := make([]byte, 1024)

	// Wait for a response using Read()
	_, err = conn.Read(buffer)
	duration := time.Since(start)

	node.TotalCount++
	if err == nil {
		node.SuccessCount++
		node.ResponseTimes = append(node.ResponseTimes, duration)

		// Calculate the average response time
		totalDuration := time.Duration(0)
		for _, d := range node.ResponseTimes {
			totalDuration += d
		}
		node.AverageTime = totalDuration / time.Duration(len(node.ResponseTimes))
	}
}

// testNodesMultipleTimes runs tests on nodes a given number of times.
func testNodesMultipleTimes(nodes []*Node, testCount int) {
	var wg sync.WaitGroup

	// Run tests multiple times
	for i := 0; i < testCount; i++ {
		for _, node := range nodes {
			wg.Add(1)
			go measureNodeResponse(node, &wg)
		}
	}

	// Wait for all goroutines to finish
	wg.Wait()

	// Process and print results for each node
	for _, node := range nodes {

		//var totalDuration time.Duration
		//
		//for _, responseTime := range node.ResponseTimes {
		//	totalDuration += responseTime
		//}
		//fmt.Printf("Address: %s, Average Time: %v, TotalTime:%v, Success Count: %d, Total Count: %d\n",
		//	node.Address, node.AverageTime, totalDuration, node.SuccessCount, node.TotalCount)

		// Calculate jitter for each node if needed
		node.Jitter = calculateJitter(node)
		//fmt.Printf("Node %s - Jitter: %v\n", node.Address, node.Jitter)
	}
}

// Method to calculate the score for the node based on the given weights
func (n *Node) calculateScore(jitterWeight, avgTimeWeight, successWeight float64, maxAvgTime, maxJitter time.Duration) {
	// Normalize Jitter: lower is better, so invert it
	normalizedJitter := float64(n.Jitter) / float64(maxJitter) // Scale jitter based on the maximum jitter
	if normalizedJitter > 1 {
		normalizedJitter = 1 // Cap jitter normalization to 1
	}

	// Normalize Average Time: lower is better, so invert it
	normalizedAvgTime := float64(n.AverageTime) / float64(maxAvgTime) // Scale based on the max average time
	if normalizedAvgTime > 1 {
		normalizedAvgTime = 1 // Cap the normalization to 1
	}

	// Calculate Success Rate: successCount / totalCount
	successRate := 0.0
	if n.TotalCount > 0 {
		successRate = float64(n.SuccessCount) / float64(n.TotalCount)
	}

	// Compute the composite score as a weighted sum of factors
	n.Score = (jitterWeight * (1 - normalizedJitter)) +
		(avgTimeWeight * (1 - normalizedAvgTime)) +
		(successWeight * successRate)

	fmt.Printf("Score: %f\n", n.Score)
}

// Function to find the best node based on their computed scores
func findBestNode(nodes []*Node) *Node {
	// Define weights for Jitter, Average Time, and Success Rate
	jitterWeight := 0.4
	avgTimeWeight := 0.3
	successWeight := 0.3

	var maxAvgTime, maxJitter time.Duration
	for _, node := range nodes {
		if node.AverageTime > maxAvgTime {
			maxAvgTime = node.AverageTime
		}
		if node.Jitter > maxJitter {
			maxJitter = node.Jitter
		}
	}

	// Calculate the score for each node
	for _, node := range nodes {
		node.calculateScore(jitterWeight, avgTimeWeight, successWeight, maxAvgTime, maxJitter)
	}

	// Sort nodes by their score (descending order)
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Score > nodes[j].Score // Higher score is better
	})

	for _, node := range nodes {
		fmt.Printf("Adress:%s, Succes:%d, AverageTime:%v, Jitter:%v, Score:%f\n", node.Address, node.SuccessCount, node.AverageTime, node.Jitter, node.Score)
	}

	// Return the node with the highest score

	return nodes[0]
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

func startFFPlay() (*exec.Cmd, io.WriteCloser, error) {
	ffplayCmd := exec.Command("ffplay", "-f", "mjpeg", "-i", "pipe:0")
	ffplayIn, err := ffplayCmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create ffplay input pipe: %w", err)
	}

	if err := ffplayCmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start ffplay: %w", err)
	}

	return ffplayCmd, ffplayIn, nil
}

func receiveAndDisplayRTPPackets(conn *net.UDPConn, ffplayIn io.WriteCloser) {
	packet := &rtp.Packet{}

	for {
		buf := make([]byte, 150000)

		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			log.Printf("Error reading from UDP: %v", err)
			continue
		}

		if err := packet.Unmarshal(buf[:n]); err != nil {
			log.Printf("Failed to unmarshal RTP packet: %v", err)
			continue
		}

		_, err = ffplayIn.Write(packet.Payload)
		if err != nil {
			log.Printf("Failed to write to ffplay: %v", err)
			break
		}

		fmt.Printf("Received RTP packet: Seq=%d, Timestamp=%d\n", packet.SequenceNumber, packet.Timestamp)
		time.Sleep(time.Millisecond * 33)
	}
}
func sendContentRequest(conn *net.UDPConn, contentName string) error {
	// Prefix the content name with "Request:"
	message := "REQUEST " + contentName

	// Send the request message
	_, err := conn.Write([]byte(message))
	if err != nil {
		return fmt.Errorf("failed to send content name: %w", err)
	}
	fmt.Printf("Requested content: %s\n", contentName)
	return nil
}

func main() {
	nodes, err := loadNodesFromFile("test.json")
	if err != nil {
		fmt.Printf("Error loading nodes: %v\n", err)
		os.Exit(1)
	}

	testCount := 3

	testNodesMultipleTimes(nodes, testCount)

	bestNode := findBestNode(nodes)
	fmt.Printf("Eu sou o melhor node: %s", bestNode.Address)

	conn, err := setupUDPConnection("localhost", 5004)
	// Define the port flag and parse the command-line arguments
	popIp := flag.String("pop-ip", "0.0.0.0", "IP to connect to POP for testing")
	port := flag.Int("port", 8000, "UDP port to connect to on the server")
	flag.Parse()

	// Set up the UDP connection to the specified port
	conn, err := setupUDPConnection(*popIp, *port)
	if err != nil {
		log.Fatalf("Error setting up UDP connection: %v", err)
	}
	defer conn.Close()

	// wait 2 seconds to request for testing purpose
	time.Sleep(2 * time.Second)

	// Send the content name request
	err = sendContentRequest(conn, "video_min_360.mp4")
	if err != nil {
		log.Fatalf("Error sending content request: %v", err)
	}

	ffplayCmd, ffplayIn, err := startFFPlay()
	if err != nil {
		log.Fatalf("Error starting ffplay: %v", err)
	}
	defer ffplayIn.Close()

	receiveAndDisplayRTPPackets(conn, ffplayIn)

	if err := ffplayCmd.Wait(); err != nil {
		log.Fatalf("ffplay exited with error: %v", err)
	}
}
