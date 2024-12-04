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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtp"
)

type Node struct {
	Name                 string `json:"name"`
	Address              string `json:"address"`
	Port                 int
	ResponseTimes        []time.Duration
	AverageTime          time.Duration
	Jitter               time.Duration
	SuccessCount         int
	TotalCount           int
	Score                float64
	ResponseAverageDelay time.Duration
	ResponseJitter       time.Duration
	ResponseSuccRate     float64
}

func loadNodesFromFile(filename string) ([]*Node, error) {
	// Read the file contents
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

	// Convert the map to a slice of Node pointers, setting both Name and Address fields.
	var nodes []*Node
	for name, address := range nodeMap {
		node := &Node{
			Name:    name,    // Set the Name field
			Address: address, // Set the Address field
			Port:    8000,    // Default port
		}
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// Function to reset the metrics of each node after testing
func resetNodeMetrics(node *Node) {
	// Reset the metrics to "clean" the node after each test cycle
	node.ResponseTimes = nil
	node.AverageTime = 0
	node.Jitter = 0
	node.SuccessCount = 0
	node.TotalCount = 0
	node.Score = 0
	node.ResponseAverageDelay = 0
	node.ResponseJitter = 0
	node.ResponseSuccRate = 0
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

	addr := fmt.Sprintf("%s:%d", node.Address, node.Port)

	// Create a UDP connection
	conn, err := net.Dial("udp", addr)
	if err != nil {
		fmt.Printf("Failed to connect to %s: %v\n", node.Address, err)
		return
	}
	defer conn.Close()

	message := []byte("PERFTEST")
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

	n, err := conn.Read(buffer)
	duration := time.Since(start)

	perfReport := string(buffer[:n])
	parts := strings.Fields(perfReport)

	if len(parts) == 4 {
		// Parse the averageDelay string to a float64 and convert to time.Duration
		avgDelay, err := strconv.ParseFloat(parts[1], 64) // parts[0] = averageDelay
		if err != nil {
			log.Printf("Error parsing average delay: %v", err)
			return
		}
		// Convert averageDelay (milliseconds) to time.Duration (nanoseconds)
		node.ResponseAverageDelay = time.Duration(avgDelay)

		// Parse the averageJitter string to a float64 and convert to time.Duration
		avgJitter, err := strconv.ParseFloat(parts[2], 64) // parts[1] = averageJitter
		if err != nil {
			log.Printf("Error parsing average jitter: %v", err)
			return
		}
		// Convert averageJitter (milliseconds) to time.Duration (nanoseconds)
		node.ResponseJitter = time.Duration(avgJitter)

		// Parse the successRate string to float64
		successRate, err := strconv.ParseFloat(parts[3], 64) // parts[2] = successRate
		if err != nil {
			log.Printf("Error parsing success rate: %v", err)
			return
		}
		// Store success rate as float64
		node.ResponseSuccRate = successRate

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

		node.Jitter = calculateJitter(node)
		//fmt.Printf("Node %s - Jitter: %v\n", node.Address, node.Jitter)
	}
}

// Method to calculate the score for the node based on the given weights
func (n *Node) calculateScore(jitterWeight, avgTimeWeight, successWeight float64, maxAvgTime, maxJitter time.Duration) {
	// Normalize Jitter: lower is better, so invert it
	normalizedJitter := (float64(n.Jitter) + float64(n.ResponseJitter)) / float64(maxJitter) // Scale jitter based on the maximum jitter
	if normalizedJitter > 1 {
		normalizedJitter = 1 // Cap jitter normalization to 1
	}

	// Normalize Average Time: lower is better, so invert it
	normalizedAvgTime := (float64(n.AverageTime) + float64(n.ResponseAverageDelay)) / float64(maxAvgTime) // Scale based on the max average time
	if normalizedAvgTime > 1 {
		normalizedAvgTime = 1 // Cap the normalization to 1
	}

	// Calculate Success Rate: successCount / totalCount
	successRate := 0.0
	if n.TotalCount > 0 {
		successRate = float64(n.SuccessCount) / float64(n.TotalCount)
		successRate = (successRate + n.ResponseSuccRate) / float64(2)
	}

	// Compute the composite score as a weighted sum of factors
	n.Score = (jitterWeight * (1 - normalizedJitter)) +
		(avgTimeWeight * (1 - normalizedAvgTime)) +
		(successWeight * successRate)

	if n.AverageTime == 0 || n.ResponseAverageDelay == 0 {
		n.Score = 0
	}

}

// Function to find the best node based on their computed scores
func findBestNode(nodes []*Node) *Node {
	// Define weights for Jitter, Average Time, and Success Rate
	jitterWeight := 0.5
	avgTimeWeight := 0.2
	successWeight := 0.3

	var maxAvgTime, maxJitter time.Duration
	for _, node := range nodes {
		if node.AverageTime+node.ResponseAverageDelay > maxAvgTime {
			maxAvgTime = node.AverageTime + node.ResponseAverageDelay
		}
		if node.Jitter+node.ResponseJitter > maxJitter {
			maxJitter = node.Jitter + node.ResponseJitter
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

	//for _, node := range nodes {
	//	fmt.Printf("POP:%s, Score:%f\n", node.Name, node.Score)
	//}

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

	// // Send initial connection request
	// _, err = conn.Write([]byte("CONNECT"))eceiveAndDisplay
	// if err != nil {
	// 	conn.Close()
	// 	return nil, fmt.Errorf("failed to send connection request: %w", err)
	// }
	fmt.Println("Sent connection request to server")
	return conn, nil
}

func startFFPlay() (io.WriteCloser, error) {
	// ffplayCmd := exec.Command("ffplay", "-f", "mjpeg", "-i", "pipe:0")
	ffplayCmd := exec.Command("ffplay", "-f", "mjpeg", "-i", "pipe:0", "-autoexit", "-window_title", "Window stream")

	ffplayIn, err := ffplayCmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create ffplay input pipe: %w", err)
	}

	if err := ffplayCmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ffplay: %w", err)
	}

	return ffplayIn, nil
}

func receiveAndDisplayRTPPackets(conn **net.UDPConn, connMutex *sync.Mutex, ffplayIn io.WriteCloser, stop chan struct{}, done chan struct{}) {
	packet := &rtp.Packet{}

	inactivityTimeout := 3 * time.Second         // Set timeout duration for inactivity
	timeoutChan := time.After(inactivityTimeout) // Channel that triggers after the inactivity timeout

	for {
		select {
		case <-stop:
			log.Println("Stopping packet reception.")
			return
		case <-timeoutChan: // Timeout after inactivity period
			log.Println("No packets received within timeout period. Assuming stream ended.")
			closeStream(ffplayIn, *conn)
			close(done)
			return
		default:
			// Lock the mutex and get the current connection
			connMutex.Lock()
			activeConn := *conn
			connMutex.Unlock()

			if activeConn == nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			buf := make([]byte, 150000)

			activeConn.SetReadDeadline(time.Now().Add(inactivityTimeout)) // Set a timeout

			n, _, err := activeConn.ReadFrom(buf)
			if err != nil {
				if isTimeoutError(err) {
					log.Println("Stream ended due to timeout.")
					continue // Timeout error, just retry reading
				}
				log.Printf("Error reading from UDP: %v", err)
				closeStream(ffplayIn, activeConn)
				close(done)
				return
			}

			// Reset timeout channel to wait for the next timeout
			timeoutChan = time.After(inactivityTimeout)

			if err := packet.Unmarshal(buf[:n]); err != nil {
				log.Printf("Failed to unmarshal RTP packet: %v", err)
				continue
			}

			_, err = ffplayIn.Write(packet.Payload)
			if err != nil {
				log.Printf("Failed to write to ffplay: %v", err)
				return
			}

			// Simulate 30 FPS playback
			time.Sleep(time.Millisecond * 33)
		}
	}
}

// Helper function to close the stream and connection
func closeStream(ffplayIn io.WriteCloser, conn *net.UDPConn) {
	log.Println("Closing stream and connection.")
	ffplayIn.Close()
	conn.Close()
}

func isTimeoutError(err error) bool {
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return true
	}
	return false
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

// sendAliveMessage sends an ALIVE message to the current best node using the provided UDP connection.
func sendAliveMessage(conn *net.UDPConn) {
	if conn == nil {
		log.Println("No active connection to send ALIVE message.")
		return
	}

	aliveMessage := "ALIVE"
	_, err := conn.Write([]byte(aliveMessage))
	if err != nil {
		log.Printf("Error sending ALIVE message: %v", err)
	}
	//else {
	//	log.Println("ALIVE message sent successfully.")
	//}
}

func main() {
	// Define the port flag and parse the command-line arguments
	stream := flag.String("stream", "stream1", "stream to connect to")
	popFile := flag.String("config", "pops.json", "Config File with pops")
	//port := flag.Int("port", 8000, "UDP port to connect to on the server")
	flag.Parse()
	log.SetFlags(log.Ltime)

	nodes, err := loadNodesFromFile(*popFile)
	if err != nil {
		fmt.Printf("Error loading nodes: %v\n", err)
		os.Exit(1)
	}

	var conn *net.UDPConn
	var connMutex sync.Mutex // Protect the connection with a mutex

	var bestNode *Node
	var previousBestNodeAddr string
	var previousBestNodeName string
	testCount := 3

	//first time
	testNodesMultipleTimes(nodes, testCount)
	bestNode = findBestNode(nodes)
	previousBestNodeAddr = bestNode.Address
	previousBestNodeName = bestNode.Name
	fmt.Printf("Best POP %s\n", previousBestNodeName)

	for _, node := range nodes {
		resetNodeMetrics(node)
	}

	// Create a ticker that ticks every minute (60 seconds)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Best node switching logic in the ticker goroutine
	go func() {
		for range ticker.C {
			// Test nodes and find the best one
			testNodesMultipleTimes(nodes, testCount)
			bestNode = findBestNode(nodes)

			// If the best node has changed, reinitialize the connection
			if bestNode.Address != previousBestNodeAddr {

				for _, node := range nodes {
					if node.Address == previousBestNodeAddr {

						if bestNode.Score-node.Score > 0.1 {

							for _, node := range nodes {
								log.Printf("POP %s, Score:%f\n", node.Name, node.Score)
							}
							log.Printf("Best POP updated to %s from %s\n", bestNode.Name, previousBestNodeName)
							log.Printf("Best POP changed, reinitializing stream request to: %s\n", bestNode.Name)

							connMutex.Lock()

							// Close the old connection if it exists
							if conn != nil {
								conn.Close()
							}

							// Set up a new connection
							newConn, err := setupUDPConnection(bestNode.Address, bestNode.Port)
							if err != nil {
								log.Printf("Error setting up new UDP connection: %v", err)
								connMutex.Unlock()
								continue
							}

							// Update the global connection and send the content request
							conn = newConn
							err = sendContentRequest(conn, *stream)
							if err != nil {
								log.Printf("Error sending content request: %v", err)
							}

							// Unlock the mutex after updating the connection
							connMutex.Unlock()

							// Update the previous best node address
							previousBestNodeAddr = bestNode.Address
							previousBestNodeName = bestNode.Name

						}
					}
				}

			}

			sendAliveMessage(conn)

			// Reset metrics for all nodes
			for _, node := range nodes {
				resetNodeMetrics(node)
			}
		}
	}()

	// Set up the UDP connection to the specified port
	conn, err = setupUDPConnection(bestNode.Address, bestNode.Port)
	if err != nil {
		log.Fatalf("Error setting up UDP connection: %v", err)
	}
	defer conn.Close()

	sendAliveMessage(conn)

	// Wait 2 seconds to request for testing purpose
	//time.Sleep(2 * time.Second)

	// Send the content name request
	err = sendContentRequest(conn, *stream)
	if err != nil {
		log.Fatalf("Error sending content request: %v", err)
	}

	ffplayIn, err := startFFPlay()
	if err != nil {
		log.Fatalf("Error starting ffplay: %v", err)
	}
	defer ffplayIn.Close()

	stop := make(chan struct{})
	done := make(chan struct{})

	// Start receiving and displaying RTP packets
	go receiveAndDisplayRTPPackets(&conn, &connMutex, ffplayIn, stop, done)

	// Wait for the stream to end or the ffplay process to exit
	<-done
	log.Println("Stream ended, shutting down.")

}
