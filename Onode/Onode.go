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

type Metric struct {
	Timestamp string `json:"timestamp"`
	Hops      int    `json:"hops"` // Number of hops or any other metric-related number

}

// NodeMetrics structure to hold the node name and associated metrics
type NodeMetrics struct {
	Name    string `json:"name"`    // Node name
	Metrics Metric `json:"metrics"` // Slice of Metric structs
}

// Probing structure to hold the slice of NodeMetrics
type Probing struct {
	Id         int           `json:"Id"`
	TotalCount int           `json:"TotalCount"`
	Nodes      []NodeMetrics `json:"nodes"` // Slice of NodeMetrics
}

// Define the ProbingState struct locally within the function
type ProbingState struct {
	ProbingMap map[string][]Probing
	Timer      *time.Timer
}

// Parametheres to calculate best path
type PathMetrics struct {
	AverageDelay  float64 `json:"averageDelay"`
	AverageJitter float64 `json:"averageJitter"`
	TotalCount    int     `json:"TotalCount"`
	SuccessRate   float64 `json:"successRate"`
	Score         float64 `json:"score"`
}

var (
	videos              map[string]string
	clients             map[string][]net.Addr
	clientsNode         map[string]map[string][]net.Addr
	clientsName         map[string][]net.UDPAddr
	clientsMu           sync.Mutex // Mutex to protect the client list
	streamConnectionsIn map[string]*net.UDPConn
	streamConnMu        sync.Mutex        // Mutex to protect streamConnectionsIn
	routingTable        map[string]string //Roting Table
	stopChans           map[string]chan struct{}
	stopChansMu         sync.Mutex
	verboseFlag         bool
)

// Updated getAllNames Function
func getAllNames(probing Probing) (string, error) {
	var names []string
	for _, node := range probing.Nodes {
		names = append(names, node.Name) // Collect all node names
	}
	return strings.Join(names, ","), nil // Return the names joined by commas
}

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
	log.Printf("Node %s (Type: %s) - Stored neighbors: %v\n", node.Name, node.Type, node.Neighbors)
}

func handleError(err error, errMsg string, args ...interface{}) bool {
	if err != nil {
		log.Printf(errMsg, args...)
		log.Printf("Error details: %v", err)
		return true
	}
	return false
}

func prepareFFmpegCommands(videos map[string]string) (map[string]*exec.Cmd, error) {
	ffmpegMap := make(map[string]*exec.Cmd)
	var err error

	for name, videoPath := range videos {
		// ffmpegCmd := exec.Command("ffmpeg",
		// 	// "-stream_loop", "-1", // Loop the video infinitely
		// 	"-i", videoPath, // Input file
		// 	"-f", "image2pipe", // Output format for piping images
		// 	"-vcodec", "mjpeg", // Encode as JPEG
		// 	"-q:v", "2", // Quality (lower is better)
		// 	"pipe:1") // Output to stdout

		ffmpegMap[name], err = prepareFFmpegCommand(videoPath)
	}

	return ffmpegMap, err
}

// Function to create an FFmpeg command for a single video
func prepareFFmpegCommand(videoPath string) (*exec.Cmd, error) {
	ffmpegCmd := exec.Command("ffmpeg",
		// "-stream_loop", "-1", // Loop the video infinitely
		"-i", videoPath, // Input file
		"-f", "image2pipe", // Output format for piping images
		"-vcodec", "mjpeg", // Encode as JPEG
		"-q:v", "2", // Quality (lower is better)
		"pipe:1") // Output to stdout

	return ffmpegCmd, nil
}

// Starts ffmpeg to output video frames as JPEGs for Content Server

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

// Initialize probing for a node, called by CS type node
func (n *Node) initializeProbing(protocolConn *net.UDPConn, totalcount int, id int) {
	probing := Probing{
		Id:         id,
		TotalCount: totalcount,
		Nodes: []NodeMetrics{
			{
				Name: n.Name,
				Metrics: Metric{
					Timestamp: time.Now().Format(time.RFC3339Nano), // Get the current timestamp in ISO 8601 format
					Hops:      0,                                   // Start with 0 hops
				},
			},
		},
	}
	var neighborsList []string

	// Send the probing to all neighbors totalcount times
	for i := 0; i < totalcount; i++ {
		for neighbor := range n.Neighbors {
			if i == 0 {
				neighborsList = append(neighborsList, neighbor)
			}

			err := n.sendProbing(protocolConn, neighbor, probing)
			if err != nil {
				log.Printf("Error sending probing to %s: %v", neighbor, err)
			}
		}
		log.Printf("Sending probing to neighbor:%s from node %s", neighborsList, n.Name)
	}

}

func (n *Node) sendProbing(protocolConn *net.UDPConn, neighborName string, probing Probing) error {
	// Marshal the probing structure into JSON or another suitable format

	probingData, err := json.Marshal(probing)
	if err != nil {
		return fmt.Errorf("failed to marshal probing data: %v", err)
	}

	// Send the probing to the neighbor's address (you will need the neighbor's IP from the Neighbors map)
	neighborAddr, ok := n.Neighbors[neighborName]

	if !ok {
		return fmt.Errorf("neighbor %s not found in Neighbors map", neighborName)
	}

	// Append the port to the neighbor's IP

	neighborWithPort := fmt.Sprintf("%s:%d", neighborAddr, 8000)

	// Create the UDP address for the neighbor
	addr, err := net.ResolveUDPAddr("udp", neighborWithPort)
	if err != nil {
		return fmt.Errorf("failed to resolve UDP address: %v", err)
	}

	finalMessage := append([]byte("PROBING "), probingData...)
	// Send the probing data to the neighbor
	//log.Printf("Sending probing to neighbor: %s from node: %s", neighborName, n.Name)
	// log.Printf("Sending probing to neighbor: %s from node: %s", neighborName, n.Name)
	_, err = protocolConn.WriteTo(finalMessage, addr)
	if err != nil {
		return fmt.Errorf("failed to send probing: %v", err)
	}

	return nil
}

func (node *Node) filterNeighbors(probing *Probing) []string {
	// Create a map of existing nodes in the probing (for quick lookup)
	alreadyInProbing := make(map[string]struct{})
	for _, probingNode := range probing.Nodes {
		alreadyInProbing[probingNode.Name] = struct{}{} // Empty struct to represent presence
	}

	// Filter neighbors: only those that are not in the probing path
	var filteredNeighbors []string
	for neighborName := range node.Neighbors { // Iterate over the keys (neighbor names)
		if _, exists := alreadyInProbing[neighborName]; !exists {
			filteredNeighbors = append(filteredNeighbors, neighborName)
		}
	}

	return filteredNeighbors
}

// calculateScores calculates scores for each path and updates the PathMetrics map
func calculateScores(metrics map[string]PathMetrics, maxAvgTime, maxJitter float64) map[string]PathMetrics {
	// Define weights for jitter, average delay, and success rate
	const (
		jitterWeight  = 0.5
		avgTimeWeight = 0.2
		successWeight = 0.3
	)

	for key, metric := range metrics {
		// Normalize Jitter
		normalizedJitter := metric.AverageJitter / maxJitter
		if normalizedJitter > 1 {
			normalizedJitter = 1
		}

		// Normalize Average Delay
		normalizedAvgTime := metric.AverageDelay / maxAvgTime
		if normalizedAvgTime > 1 {
			normalizedAvgTime = 1
		}

		// Compute the score
		metric.Score = (jitterWeight * (1 - normalizedJitter)) +
			(avgTimeWeight * (1 - normalizedAvgTime)) +
			(successWeight * metric.SuccessRate)

		metrics[key] = metric

		//fmt.Printf("Path: %s Score: %f\n", key, metric.Score)
	}

	return metrics
}

// Example to find the best path
func findBestPath(metrics map[string]PathMetrics) (string, PathMetrics) {
	var bestKey string
	var highestScore float64
	var bestMetrics PathMetrics

	for key, metric := range metrics {
		if metric.Score > highestScore {
			highestScore = metric.Score
			bestKey = key
			bestMetrics = metric
		}
	}

	return bestKey, bestMetrics
}

func calculateBestPath(probingState *ProbingState, oldPath string) (string, PathMetrics) {
	result := make(map[string]PathMetrics)

	var maxDelay float64 = 0  // Track the maximum delay
	var maxJitter float64 = 0 // Track the maximum jitter

	// Iterate over each key in the ProbingMap
	for key, probings := range probingState.ProbingMap {
		var delays []float64                        // Store delays for each probing
		var jitters []float64                       // Store jitter differences between consecutive delays
		var totalSuccess int                        // Total successful packets
		var totalCount int = probings[0].TotalCount // Total packets sent

		//get delay
		for _, probing := range probings {
			// Check if there are enough nodes to calculate delay
			if len(probing.Nodes) >= 2 {
				// Most recent
				firstTimestamp, err1 := time.Parse(time.RFC3339Nano, probing.Nodes[0].Metrics.Timestamp)
				// Oldest
				lastTimestamp, err2 := time.Parse(time.RFC3339Nano, probing.Nodes[len(probing.Nodes)-1].Metrics.Timestamp)

				if err1 == nil && err2 == nil {
					// Calculate delay
					delay := firstTimestamp.Sub(lastTimestamp).Nanoseconds() // Convert to milliseconds
					delays = append(delays, float64(delay))

					totalSuccess++

					// Track the maximum delay
					if float64(delay) > maxDelay {
						maxDelay = float64(delay)
					}
				}
			}
		}

		// Calculate jitter between consecutive delays
		for i := 1; i < len(delays); i++ {
			jitter := delays[i] - delays[i-1]
			if jitter < 0 {
				jitter = -jitter
			}
			jitters = append(jitters, jitter)

			// Track maximum jitter
			if jitter > maxJitter {
				maxJitter = jitter
			}
		}

		// Calculate average delay
		var totalDelay float64
		for _, delay := range delays {
			totalDelay += delay
		}
		averageDelay := totalDelay / float64(len(delays))

		// Calculate average jitter
		var totalJitter float64
		for _, jitter := range jitters {
			totalJitter += jitter
		}
		averageJitter := totalJitter / float64(len(jitters))

		// Store results in the map for this path (key)
		result[key] = PathMetrics{
			AverageDelay:  averageDelay,
			AverageJitter: averageJitter,
			TotalCount:    totalCount,
			SuccessRate:   float64(totalSuccess) / float64(totalCount),
		}
	}

	scores := calculateScores(result, maxDelay, maxJitter)

	key, metric := findBestPath(scores)

	log.Printf("NEW best:%s Score:%f\n", key, metric.Score)

	if oldPath != "" {
		if metric.Score-scores[oldPath].Score < 0.1 {
			key = oldPath
			metric = scores[oldPath]
			log.Printf("OLD best:%s Score:%f\n", key, metric.Score)

		}
	}

	return key, metric
}

func addClientAddress(contentName string, clientAddr net.Addr, clients map[string][]net.Addr, mu *sync.Mutex) {
	mu.Lock()
	defer mu.Unlock()

	// Check if the client address already exists for the given content
	for _, c := range clients[contentName] {
		if c.String() == clientAddr.String() {
			log.Printf("Existing client %s reconnected for content \"%s\"", clientAddr, contentName)
			return
		}
	}

	// Add the client address to the list
	clients[contentName] = append(clients[contentName], clientAddr)
	log.Printf("New client connected from %s for content \"%s\"", clientAddr, contentName)
}

func addClientAddressNode(contentName string, popOfRoute string, clientAddr net.Addr, clientsNode map[string]map[string][]net.Addr, mu *sync.Mutex) {
	mu.Lock()
	defer mu.Unlock()

	// Check if the client address already exists for the given content
	for _, c := range clientsNode[contentName][popOfRoute] {
		if c.String() == clientAddr.String() {
			log.Printf("Existing client %s reconnected for content from %s \"%s\"", clientAddr, popOfRoute, contentName)
			return
		}
	}

	// Add the client address to the list
	clientsNode[contentName][popOfRoute] = append(clientsNode[contentName][popOfRoute], clientAddr)
	log.Printf("New client connected from %s for content \"%s\"", popOfRoute, contentName)
}

func addClientName(contentName string, clientName string, clientsIp map[string][]net.UDPAddr, mu *sync.Mutex, node Node) {
	mu.Lock()
	defer mu.Unlock()

	clientIP, err := getNextInRouteAddr(node.Neighbors[clientName])
	if err != nil {
	}

	log.Printf("Adding client %v for content \"%s\"", *clientIP, contentName)

	// Check if the client address already exists for the given content
	for _, c := range clientsIp[contentName] {
		if c.String() == clientIP.String() {
			log.Printf("Existing client %s reconnected for content \"%s\"", clientIP, contentName)
			return
		}
	}

	// Add the client address to the list
	clientsIp[contentName] = append(clientsIp[contentName], *clientIP)
	log.Printf("New client connected from %s for content \"%s\"", clientIP, contentName)
}

func addClientNameNode(contentName string, popOfRoute string, clientIP net.UDPAddr, clientsName map[string]map[string][]net.UDPAddr, mu *sync.Mutex) {
	mu.Lock()
	defer mu.Unlock()
	log.Printf("New client %v", clientIP)

	// Check if the client address already exists for the given content
	for _, c := range clientsName[contentName][popOfRoute] {
		if c.String() == clientIP.String() {
			log.Printf("Existing client %v reconnected for content from %s \"%s\"", clientIP, popOfRoute, contentName)
			return
		}
	}

	// Add the client address to the list
	clientsName[contentName][popOfRoute] = append(clientsName[contentName][popOfRoute], clientIP)
	log.Printf("New client connected from %s for content \"%s\"", popOfRoute, contentName)
}

func (node *Node) handleConnectionsPOP(protocolConn *net.UDPConn, routingTable map[string]string, neighbors map[string]string, popOfRoute string) {
	// Initialize probingState and expectedID
	probingState := &ProbingState{
		ProbingMap: make(map[string][]Probing),
	}
	expectedID := 0

	firstProbing := true

	clientsAlive := make(map[string]time.Time) // Map to store alive timestamps
	var clientsAliveMu sync.Mutex              // Mutex for thread-safe access

	var bestPath string
	var bestMetric PathMetrics

	// Initialize the map if it's nil
	if streamConnectionsIn == nil {
		streamConnectionsIn = make(map[string]*net.UDPConn)
	}

	clients := make(map[string][]net.Addr)
	stopChans = make(map[string]chan struct{})
	// var stopChansMu sync.Mutex

	// stopChan := make(chan struct{})
	buf := make([]byte, 1024)

	for {
		n, clientAddr, err := protocolConn.ReadFrom(buf) // porta 8000
		if err != nil || n == 0 {
			log.Printf("Error reading client connection request: %v", err)
			continue
		}

		// Read the message from the buffer as a string
		clientMessage := string(buf[:n])
		//log.Printf("Received message from client %s: %s", clientAddr, clientMessage)

		// Split the message into parts by whitespace
		parts := strings.Fields(clientMessage)
		if len(parts) == 0 {
			log.Printf("Received empty message from client %s", clientAddr)
			continue
		}

		//log.Printf("Received message \"%s\" from client %s", clientMessage, clientAddr)

		// Parse the command and handle each case
		command := parts[0]
		switch command {

		case "ENDSTREAM":
			log.Printf("Received message \"%s\" from client %s", clientMessage, clientAddr)

			if len(parts) < 2 {
				log.Printf("ENDSTREAM command from client %s is missing args", clientAddr)
				continue
			}
			contentName := parts[1]
			// popOfRoute := parts[2]

			streamConnMu.Lock()                      // Lock the mutex to ensure safe access to the shared resource
			delete(streamConnectionsIn, contentName) // Remove the entry for the specified contentName
			streamConnMu.Unlock()                    // Unlock the mutex after modifying the map

			clientsMu.Lock()
			delete(clients, contentName) // Removes contentName from map and releases memory
			clientsMu.Unlock()

		case "UPDATE":
			if len(parts) < 3 {
				log.Printf("UPDATE command from client %s is missing data", clientAddr)
				continue
			}

			popOfRoute := parts[1]

			updateDataString := strings.Join(parts[2:], " ") // Extract the JSON data
			updateData := []byte(updateDataString)

			var newRoutes []string
			if handleError(json.Unmarshal(updateData, &newRoutes), "Failed to parse UPDATE data from client %s", clientAddr) {
				continue
			}

			log.Printf("Received UPDATE from %s: %v", clientAddr, newRoutes)

			// Call the ExtractFirstElement function
			first, restJSON, err := ExtractFirstElement(updateData)
			if handleError(err, "Failed to extract first element from UPDATE data from client %s", clientAddr) {
				continue
			}

			// Get the next-in-route IP address
			nextInRouteIp, err := getNextInRouteAddr(neighbors[first])
			if handleError(err, "Failed to resolve next-in-route IP for \"%s\" from client %s", first, clientAddr) {
				continue
			}

			// Check if the neighbor exists
			if _, exists := neighbors[first]; !exists {
				log.Printf("Neighbor \"%s\" does not exist in the routing table. Cannot update.", first)
				continue
			}

			// Update the routing table
			updateRoutingTable(routingTable, popOfRoute, neighbors[first])

			// Send the update packet
			if handleError(sendUpdatePacket(protocolConn, popOfRoute, restJSON, nextInRouteIp), "Failed to send update packet to %s for \"%s\"", nextInRouteIp, first) {
				continue
			}

			log.Printf("Successfully processed UPDATE for \"%s\" and forwarded to %s", popOfRoute, nextInRouteIp)

		case "REQUEST":
			if len(parts) < 2 {
				log.Printf("REQUEST command from client %s is missing a content name", clientAddr)
				continue
			}
			contentName := parts[1]

			// Check if the content name exists in the routing table
			if _, exists := routingTable[contentName]; !exists {
				log.Printf("Content \"%s\" not found in the routing table. Skipping request from client %s", contentName, clientAddr)
				continue
			}

			if _, exists := streamConnectionsIn[contentName]; !exists {
				log.Printf("Content \"%s\" not found in the Conn In.", contentName)
			}

			log.Printf("REQUEST for content \"%s\" from client %s", contentName, clientAddr)

			addClientAddress(contentName, clientAddr, clients, &clientsMu)

			streamConnMu.Lock()
			_, exists := streamConnectionsIn[contentName]
			streamConnMu.Unlock()

			log.Printf("LIST OF CLIENTS: %s", clients[contentName])

			if !exists {
				// Connection doesn't exist, create a new one
				var err error
				streamConnIn, err := setupUDPConnection(routingTable[contentName], 8000)
				if err != nil {
					log.Fatalf("Error setting up UDP connection to %s for content \"%s\": %v", routingTable[contentName], contentName, err)
				}

				// Add the new connection to the map
				streamConnMu.Lock()
				streamConnectionsIn[contentName] = streamConnIn
				streamConnMu.Unlock()

				// Send the content request to the appropriate stream connection
				err = sendContentRequest(streamConnIn, contentName, popOfRoute, node.Name) // escrever na porta 8000 do vizinho
				if err != nil {
					log.Printf("Failed to request content \"%s\" for client %s: %v", contentName, clientAddr, err)
					continue // Skip forwarding if content request fails
				}

				log.Printf("New connection established for content \"%s\"", contentName)
				stopChansMu.Lock()
				if _, exists := stopChans[contentName]; !exists {
					stopChans[contentName] = make(chan struct{})
				}
				stopChan := stopChans[contentName]
				stopChansMu.Unlock()

				// Forward the stream to the client
				go forwardToClients(protocolConn, streamConnIn, contentName, clients, stopChan)
			} else {
				log.Printf("Reusing existing connection for content \"%s\"", contentName)
			}
		case "PROBING":

			var probing Probing
			err := json.Unmarshal([]byte(parts[1]), &probing)
			if err != nil {
				log.Printf("Error unmarshalling probing message: %v", err)
				continue
			}

			//se for a primeira vez que recebe um probing o expectedId deve ser igual ao id do probing que recebe
			if firstProbing {
				expectedID = probing.Id
				firstProbing = false
			}

			// Ignore messages with unexpected IDs
			if probing.Id != expectedID {
				log.Printf("Ignoring probing with unexpected ID: %d (expected: %d)", probing.Id, expectedID)
				continue
			}

			// 10 second timeout
			if probingState.Timer == nil {
				probingState.Timer = time.AfterFunc(3*time.Second, func() {
					//ensures that it does not have problems in the first run
					if len(probingState.ProbingMap) > 0 {
						newbestPath, newbestMetric := calculateBestPath(probingState, bestPath) // Calculate the best path
						log.Printf("Received %d paths with the best path being: %s Score:%f\n", len(probingState.ProbingMap), newbestPath, newbestMetric.Score)

						//best path changed
						if newbestPath != bestPath {

							bestPath = newbestPath
							bestMetric = newbestMetric

							elements := strings.Split(newbestPath, ",")

							// Marshal the slice into JSON
							best, err := json.Marshal(elements)
							if err != nil {
								fmt.Printf("Error marshalling JSON: %v\n", err)
								return
							}

							popOfRoute, jsonUpdate, err := ExtractFirstElement(best)
							if handleError(err, "Failed to extract first element from JSON update: %s", string(jsonUpdate)) {
								return
							}

							//fmt.Printf("POP: %s\n", popOfRoute)

							first, restJSON, err := ExtractFirstElement(jsonUpdate)
							if handleError(err, "Failed to extract first element from JSON update: %s", string(jsonUpdate)) {
								return
							}

							//fmt.Printf("First: %s\n", first)
							//fmt.Printf("Rest: %s\n", restJSON)

							nextInRouteIp, err := getNextInRouteAddr(node.Neighbors[first])
							if handleError(err, "Failed to resolve next-in-route IP for neighbor: %s", first) {
								return
							}

							updateRoutingTable(routingTable, "stream1", node.Neighbors[first])
							updateRoutingTable(routingTable, "stream2", node.Neighbors[first])
							updateRoutingTable(routingTable, "stream3", node.Neighbors[first])

							err = sendUpdatePacket(protocolConn, popOfRoute, restJSON, nextInRouteIp)
							if handleError(err, "Failed to send update packet to %s", nextInRouteIp) {
								return
							}
						}

						probingState.ProbingMap = make(map[string][]Probing) // Reset map
						expectedID++                                         // Move to the next probing ID
					}
					probingState.Timer = nil
				})
			}

			//adds itself
			lastNode := probing.Nodes[0]
			newHop := lastNode.Metrics.Hops + 1

			// Add the new entry in Probing
			newNode := NodeMetrics{
				Name: node.Name,
				Metrics: Metric{
					Timestamp: time.Now().Format(time.RFC3339Nano), // Add the current timestamp
					Hops:      newHop,
				},
			}
			probing.Nodes = append([]NodeMetrics{newNode}, probing.Nodes...)

			//Create the key for the map
			pathKey, err := getAllNames(probing)
			if err != nil {
				log.Printf("Error generating path key: %v", err)
				continue
			}

			//Add the Probing to the map
			probingState.ProbingMap[pathKey] = append(probingState.ProbingMap[pathKey], probing)
			//log.Printf("Probing added for path: %s", pathKey)

		case "PERFTEST":

			response := fmt.Sprintf("PERFREPORT %f %f %f", bestMetric.AverageDelay, bestMetric.AverageJitter, bestMetric.SuccessRate)
			_, err = protocolConn.WriteTo([]byte(response), clientAddr)
			if err != nil {
				log.Printf("Error sending response to %s: %v\n", clientAddr, err)
			}

		case "ALIVE":
			clientAddrStr := clientAddr.String()

			clientsAliveMu.Lock()
			clientsAlive[clientAddrStr] = time.Now()
			clientsAliveMu.Unlock()

			//log.Printf("Updated ALIVE timestamp for client: %s", clientAddrStr)

			// Start a goroutine for cleanup specific to this client
			go func(addr string) {
				time.Sleep(8 * time.Second) // Wait 15 seconds
				now := time.Now()

				clientsAliveMu.Lock()
				lastTimestamp, exists := clientsAlive[addr]
				if exists && now.Sub(lastTimestamp) > 8*time.Second {

					delete(clientsAlive, addr)

					var contentName string
					found := false

					for key, addrs := range clients {
						for _, addr := range addrs {
							if addr.String() == clientAddr.String() {
								contentName = key
								found = true
								break
							}
						}
						if found {
							break
						}
					}

					for i, addr := range clients[contentName] {
						if addr.String() == clientAddr.String() {
							// Remove clientAddr by slicing out the element
							clients[contentName] = append(clients[contentName][:i], clients[contentName][i+1:]...)
							break
						}
					}

					NumberWatching := len(clients[contentName])

					if NumberWatching < 1 {
						// Do something else if count is 1 or less
						nextInRouteIp, _ := getNextInRouteAddr(routingTable[contentName])

						streamConnMu.Lock()                      // Lock the mutex to ensure safe access to the shared resource
						delete(streamConnectionsIn, contentName) // Remove the entry for the specified contentName
						streamConnMu.Unlock()                    // Unlock the mutex after modifying the map

						// Close the stopChan only once
						stopForwarding(contentName)

						sendEndStreamUp(protocolConn, nextInRouteIp, contentName, node.Name)

					}
					log.Printf("The client: %s is no longer alive\n", clientAddrStr)
				}

				clientsAliveMu.Unlock()
			}(clientAddrStr)

		default:
			log.Printf("Unknown message from %s: %s", clientAddr, clientMessage)
		}
	}
}

func stopForwarding(contentName string) {
	log.Printf("Stopping forwarding of %s", contentName)

	stopChansMu.Lock()

	if len(stopChans) == 0 {
		log.Println("No active stop channels.")
		return
	}

	//log.Println("Active stop channels:")
	for contentName := range stopChans {
		log.Printf("Content: %s", contentName)
	}

	if stopChan, exists := stopChans[contentName]; exists {
		log.Printf("Found %s to stop", contentName)

		close(stopChan)                // Signal the goroutine to stop
		delete(stopChans, contentName) // Clean up the map entry
	}
	stopChansMu.Unlock()
}

func (node *Node) handleConnectionsNODE(protocolConn *net.UDPConn, routingTable map[string]string, neighbors map[string]string) {
	// Initialize the map if it's nil
	if streamConnectionsIn == nil {
		streamConnectionsIn = make(map[string]*net.UDPConn)
	}

	clientsNode := make(map[string]map[string][]net.Addr)
	clientsName := make(map[string]map[string][]net.UDPAddr)
	stopChans = make(map[string]chan struct{})
	// var stopChansMu sync.Mutex

	// stopChan := make(chan struct{})
	buf := make([]byte, 1024)

	for {
		n, clientAddr, err := protocolConn.ReadFrom(buf) // porta 8000
		if err != nil || n == 0 {
			log.Printf("Error reading client connection request: %v", err)
			continue
		}

		// Read the message from the buffer as a string
		clientMessage := string(buf[:n])
		//log.Printf("Received message from client %s: %s", clientAddr, clientMessage)

		// Split the message into parts by whitespace
		parts := strings.Fields(clientMessage)
		if len(parts) == 0 {
			log.Printf("Received empty message from client %s", clientAddr)
			continue
		}
		//log.Printf("Received message \"%s\" from client %s", clientMessage, clientAddr)

		// Parse the command and handle each case
		command := parts[0]
		switch command {

		case "ENDSTREAM_UP":
			log.Printf("Received message \"%s\" from client %s", clientMessage, clientAddr)

			if len(parts) < 2 {
				log.Printf("ENDSTREAM_UP command from client %s is missing args", clientAddr)
				continue
			}

			contentName := parts[1]
			popOfRoute := parts[2]

			// Stops sending
			clientsMu.Lock()
			delete(clientsName[contentName], popOfRoute) // Removes contentName from map and releases memory
			delete(clientsNode[contentName], popOfRoute) // Removes contentName from map and releases memory
			clientsMu.Unlock()

			NumberWatching := len(clientsNode[contentName])

			log.Printf("Clients watching %s: %d ", contentName, NumberWatching)

			if NumberWatching < 1 {

				// Do something else if count is 1 or less

				streamConnMu.Lock()                      // Lock the mutex to ensure safe access to the shared resource
				delete(streamConnectionsIn, contentName) // Remove the entry for the specified contentName
				streamConnMu.Unlock()

				// clientsMu.Lock()
				// delete(clientsName, contentName) // Remove the entry for the specified contentName from map and releases memory
				// delete(clientsNode, contentName) // Removes contentName from map and releases memory
				// clientsMu.Unlock()
				// Unlock the mutex after modifying the map
				// Close the stopChan only once
				stopForwarding(contentName)

			}
			nextInRouteIp, _ := getNextInRouteAddr(routingTable[popOfRoute])

			sendEndStreamUp(protocolConn, nextInRouteIp, contentName, popOfRoute)

		case "ENDSTREAM":

			if len(parts) < 2 {
				log.Printf("ENDSTREAM command from client %s is missing args", clientAddr)
				continue
			}
			contentName := parts[1]
			popOfRoute := parts[2]
			log.Printf("Clients to send endstream: %v ", clientsName[contentName])

			sendEndStreamClientsNode(protocolConn, contentName, popOfRoute, clientsName[contentName])

			// cleans input
			streamConnMu.Lock()                      // Lock the mutex to ensure safe access to the shared resource
			delete(streamConnectionsIn, contentName) // Remove the entry for the specified contentName
			streamConnMu.Unlock()                    // Unlock the mutex after modifying the map

			// Stops sending
			clientsMu.Lock()
			delete(clientsName, contentName) // Removes contentName from map and releases memory
			delete(clientsNode, contentName) // Removes contentName from map and releases memory
			clientsMu.Unlock()

		case "UPDATE":
			if len(parts) < 3 {
				log.Printf("UPDATE command from client %s is missing data", clientAddr)
				continue
			}

			popOfRoute := parts[1]

			updateDataString := strings.Join(parts[2:], " ") // Extract the JSON data
			updateData := []byte(updateDataString)

			var newRoutes []string
			if handleError(json.Unmarshal(updateData, &newRoutes), "Failed to parse UPDATE data from client %s", clientAddr) {
				continue
			}

			log.Printf("Received UPDATE from %s: %v", clientAddr, newRoutes)

			// Call the ExtractFirstElement function
			first, restJSON, err := ExtractFirstElement(updateData)
			if handleError(err, "Failed to extract first element from UPDATE data from client %s", clientAddr) {
				continue
			}

			// Get the next-in-route IP address
			nextInRouteIp, err := getNextInRouteAddr(neighbors[first])
			if handleError(err, "Failed to resolve next-in-route IP for \"%s\" from client %s", first, clientAddr) {
				continue
			}

			// Check if the neighbor exists
			if _, exists := neighbors[first]; !exists {
				log.Printf("Neighbor \"%s\" does not exist in the routing table. Cannot update.", first)
				continue
			}

			// Update the routing table
			updateRoutingTable(routingTable, popOfRoute, neighbors[first])

			// Send the update packet
			if handleError(sendUpdatePacket(protocolConn, popOfRoute, restJSON, nextInRouteIp), "Failed to send update packet to %s for \"%s\"", nextInRouteIp, first) {
				continue
			}

			log.Printf("Successfully processed UPDATE for \"%s\" and forwarded to %s", popOfRoute, nextInRouteIp)

		case "REQUEST":
			if len(parts) < 2 {
				log.Printf("REQUEST command from client %s is missing a content name", clientAddr)
				continue
			}
			contentName := parts[1]
			popOfRoute := parts[2]
			clientName := parts[3]

			log.Printf("REQUEST for content \"%s\" from POP %s from client %s", contentName, popOfRoute, routingTable[clientName])

			clientsMu.Lock()
			if _, exists := clientsNode[contentName]; !exists {
				clientsNode[contentName] = make(map[string][]net.Addr)
			}
			if _, exists := clientsName[contentName]; !exists {
				clientsName[contentName] = make(map[string][]net.UDPAddr)
			}
			clientsMu.Unlock()

			nextInRouteIp, err := getNextInRouteAddr(neighbors[clientName])
			if err != nil {
			}

			addClientNameNode(contentName, popOfRoute, *nextInRouteIp, clientsName, &clientsMu)

			addClientAddressNode(contentName, popOfRoute, clientAddr, clientsNode, &clientsMu)

			streamConnMu.Lock()
			streamConnIn, exists := streamConnectionsIn[contentName]
			streamConnMu.Unlock()

			log.Printf("LIST OF CLIENTS: %v", clientsName[contentName])

			if !exists {
				// Connection doesn't exist, create a new one
				var err error
				streamConnIn, err = setupUDPConnection(routingTable[popOfRoute], 8000)
				if err != nil {
					log.Fatalf("Error setting up UDP connection to %s for content \"%s\": %v", routingTable[popOfRoute], popOfRoute, err)
				}

				// Add the new connection to the map
				streamConnMu.Lock()
				streamConnectionsIn[contentName] = streamConnIn
				streamConnMu.Unlock()

				// Send the content request to the appropriate stream connection
				err = sendContentRequest(streamConnIn, contentName, popOfRoute, node.Name) // escrever na porta 8000 do vizinho
				if err != nil {
					log.Printf("Failed to request content \"%s\" for client %s: %v", popOfRoute, clientAddr, err)
					continue // Skip forwarding if content request fails
				}

				log.Printf("New connection established for content \"%s\"", popOfRoute)

				stopChansMu.Lock()
				if _, exists := stopChans[contentName]; !exists {
					stopChans[contentName] = make(chan struct{})
				}
				stopChan := stopChans[contentName]
				stopChansMu.Unlock()

				// go forwardToClientsNode(conn, contentConn, clients, stopChan)

				// Forward the stream to the client
				go forwardToClientsNode(protocolConn, streamConnIn, clientsNode[contentName], stopChan)
			} else {

				sendContentRequest(streamConnIn, contentName, popOfRoute, node.Name) // escrever na porta 8000 do vizinho

				log.Printf("Reusing existing connection for POP \"%s\"", popOfRoute)
			}

		case "PROBING":
			var probing Probing
			err := json.Unmarshal([]byte(parts[1]), &probing)
			if err != nil {
				log.Printf("Error unmarshalling probing message: %v", err)
				continue
			}

			//adds itself
			lastNode := probing.Nodes[0]
			newHop := lastNode.Metrics.Hops + 1

			// Create the new node (O3)
			newNode := NodeMetrics{
				Name: node.Name,
				Metrics: Metric{
					Timestamp: time.Now().Format(time.RFC3339Nano), // Add the current timestamp
					Hops:      newHop,
				},
			}
			probing.Nodes = append([]NodeMetrics{newNode}, probing.Nodes...)

			filteredNeighbors := node.filterNeighbors(&probing)

			var neighborsList []string
			for _, neighbor := range filteredNeighbors {
				neighborsList = append(neighborsList, neighbor)

				// Assuming you want to increment hop value by 1 for each new neighbor
				node.sendProbing(protocolConn, neighbor, probing)
			}
			var names []string
			for _, node := range probing.Nodes {
				names = append(names, node.Name) // Collect all node names
			}
			if verboseFlag {
				log.Printf("Sending probing to neighbor:%s coming from:%s", neighborsList, names)
			}

		default:
			log.Printf("Unknown message from %s: %s", clientAddr, clientMessage)
		}
	}
}

func sendEndStreamUp(conn *net.UDPConn, udpAddr *net.UDPAddr, contentName string, popOfRoute string) error {
	if udpAddr == nil {
		return fmt.Errorf("client address is nil; cannot send ENDSTREAM_UP for content: %s", contentName)
	}

	// Create a new UDP address with the same IP but port 8000
	targetAddr := &net.UDPAddr{
		IP:   udpAddr.IP,   // Keep the same IP address
		Port: 8000,         // Set the port to 8000
		Zone: udpAddr.Zone, // Keep the same zone (if any)
	}

	// Prefix the content name with "ENDSTREAM:"
	message := "ENDSTREAM_UP " + contentName + " " + popOfRoute

	// Send the request message to the modified target address (port 8000)
	_, err := conn.WriteToUDP([]byte(message), targetAddr)
	if err != nil {
		return fmt.Errorf("failed to send content name: %w", err)
	}

	log.Printf("ENDSTREAM_UP content: %s sent to %v\n", contentName, targetAddr)
	return nil
}

func sendContentRequest(conn *net.UDPConn, contentName string, popOfRoute string, nodeName string) error {

	if conn == nil {
		return fmt.Errorf("connection is nil; cannot send request for content: %s", contentName)
	}
	// Prefix the content name with "Request:"
	message := "REQUEST " + contentName + " " + popOfRoute + " " + nodeName

	// Send the request message
	_, err := conn.Write([]byte(message))
	if err != nil {
		return fmt.Errorf("failed to send content name: %w", err)
	}
	log.Printf("Requested content: %s\n", contentName)
	return nil
}

// sendEndStreamClients sends the "ENDSTREAM" message to all clients for the given content name.
func sendEndStreamClientsNode(conn *net.UDPConn, contentName string, popOfRoute string, clientsName map[string][]net.UDPAddr) {
	// Check if there are any clients for the given contentName

	clientAddrs, exists := clientsName[popOfRoute]
	if !exists || len(clientAddrs) == 0 {
		log.Printf("No clients to send ENDSTREAM for content: %s", contentName)
		return
	}

	// Send the ENDSTREAM message to each client
	for _, client := range clientAddrs {
		err := sendEndStream(conn, &client, contentName, popOfRoute)
		if err != nil {
			log.Printf("Error sending ENDSTREAM to client %v for content \"%s\": %v", client, contentName, err)
		} else {
			log.Printf("Sent ENDSTREAM to client %v for content \"%s\"", client, contentName)
		}
	}
}
func sendEndStreamClientsCS(conn *net.UDPConn, contentName string, popOfRoute string, clientsName map[string][]net.UDPAddr) {
	// Check if there are any clients for the given contentName

	clientAddrs, exists := clientsName[contentName]
	if !exists || len(clientAddrs) == 0 {
		log.Printf("No clients to send ENDSTREAM for content: %s", contentName)
		return
	}

	// Send the ENDSTREAM message to each client
	for _, client := range clientAddrs {
		err := sendEndStream(conn, &client, contentName, popOfRoute)
		if err != nil {
			log.Printf("Error sending ENDSTREAM to client %v for content \"%s\": %v", client, contentName, err)
		} else {
		}
	}
}

func sendEndStream(conn *net.UDPConn, udpAddr *net.UDPAddr, contentName string, popOfRoute string) error {
	if udpAddr == nil {
		return fmt.Errorf("client address is nil; cannot send ENDSTREAM for content: %s", contentName)
	}

	// Create a new UDP address with the same IP but port 8000
	targetAddr := &net.UDPAddr{
		IP:   udpAddr.IP,   // Keep the same IP address
		Port: 8000,         // Set the port to 8000
		Zone: udpAddr.Zone, // Keep the same zone (if any)
	}

	// Prefix the content name with "ENDSTREAM:"
	message := "ENDSTREAM " + contentName + " " + popOfRoute

	// Send the request message to the modified target address (port 8000)
	_, err := conn.WriteToUDP([]byte(message), targetAddr)
	if err != nil {
		return fmt.Errorf("failed to send content name: %w", err)
	}

	log.Printf("ENDSTREAM content: %s sent to %v\n", contentName, targetAddr)
	return nil
}
func resetTicker(stopCh chan struct{}) {
	stopCh <- struct{}{} // Signal goroutine to reset and start with the new ticker
}

func (node *Node) handleConnectionsCS(protocolConn *net.UDPConn, streams map[string]*bufio.Reader, ffmpegCommands map[string]*exec.Cmd) {

	// Cria um ticker para executar initializeProbing a cada 30 segundos
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Variável para rastrear o probeID
	probeID := 0

	// Executa a primeira chamada imediatamente
	node.initializeProbing(protocolConn, 3, probeID)
	probeID++ // Incrementa o ID após a primeira chamada

	// Create a stop channel to signal the goroutine to reset the ticker
	stopCh := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				// Periodic call to initializeProbing
				node.initializeProbing(protocolConn, 3, probeID)
				probeID++ // Increment probeID after each call
			case <-stopCh:
				// Wait for the signal to restart with the new ticker
				log.Println("Goroutine received signal to reset the ticker")
				ticker.Stop()
				log.Println("Old ticker stopped")

				// Create a new ticker with a shorter interval, e.g., 1 second
				ticker = time.NewTicker(30 * time.Second)
				log.Println("New ticker started with 1 second interval")

				continue
			}
		}
	}()

	clientsNode = make(map[string]map[string][]net.Addr)
	clientsName = make(map[string][]net.UDPAddr)
	stopChans = make(map[string]chan struct{})

	buf := make([]byte, 1024)
	for {
		n, clientAddr, err := protocolConn.ReadFrom(buf)
		if err != nil || n == 0 {
			log.Printf("Error reading client connection request: %v", err)
			continue
		}

		// Read the message from the buffer as a string
		clientMessage := string(buf[:n])
		//log.Printf("Received message from client %s: %s", clientAddr, clientMessage)

		// Split the message into parts by whitespace
		parts := strings.Fields(clientMessage)
		if len(parts) == 0 {
			log.Printf("Received empty message from client %s", clientAddr)
			continue
		}

		// var stopChansMu sync.Mutex

		// stopChan := make(chan struct{})
		// Parse the command and handle each case
		command := parts[0]

		switch command {

		case "PROBING":
			// log.Printf("Received message \"%s\" from client %s", clientMessage, clientAddr)

			var probing Probing
			err := json.Unmarshal([]byte(parts[1]), &probing)
			if err != nil {
				log.Printf("Error unmarshalling probing message: %v", err)
				continue
			}
			log.Printf("Probing with ID: %d  mine is: %d", probing.Id, probeID)

			//se for a primeira vez que recebe um probing o expectedId deve ser igual ao id do probing que recebe
			if probeID <= probing.Id {
				// Reset the ticker when probeID < probing.Id
				log.Printf("probeID (%d) <= probing.Id (%d), resetting ticker...", probeID, probing.Id)
				// Reset probeID to match the probing ID and set expectedID to the same
				probeID = probing.Id
				// expectedID = probing.Id

				// Call resetTicker to stop and start a new ticker
				resetTicker(stopCh)

				// Initialize probing immediately
				node.initializeProbing(protocolConn, 3, probing.Id)
				probeID++
			}

			// Ignore messages with unexpected IDs

		case "ENDSTREAM_UP":
			log.Printf("Received message \"%s\" from client %s", clientMessage, clientAddr)

			if len(parts) < 3 {
				log.Printf("ENDSTREAM_UP command from client %s is missing args", clientAddr)
				continue
			}

			contentName := parts[1]
			popOfRoute := parts[2]

			clientsMu.Lock()
			delete(clientsNode[contentName], popOfRoute) // Removes contentName from map and releases memory
			clientsMu.Unlock()

			NumberWatching := len(clientsNode[contentName])

			log.Printf("Clients watching %s: %d ", contentName, NumberWatching)

			if NumberWatching < 1 {
				// Do something else if count is 1 or less

				ffmpegCommands[contentName], err = prepareFFmpegCommand(videos[contentName])
				if err != nil {
					log.Fatalf("Error creating ffmpeg for content \"%s\": %v", contentName, err)
				}
				stopChansMu.Lock()
				if stopChan, exists := stopChans[contentName]; exists {
					log.Printf("Found %s to stop", contentName)

					delete(stopChans, contentName) // Clean up the map entry
					close(stopChan)                // Signal the goroutine to stop
				}
				stopChansMu.Unlock()

				clientsMu.Lock()
				delete(clients, contentName) // Removes contentName from map and releases memory
				clientsMu.Unlock()

				delete(streams, contentName)

				log.Printf("Stream reader is: %v", streams[contentName])

			}

		case "UPDATE":
			if len(parts) < 3 {
				log.Printf("UPDATE command from client %s is missing data", clientAddr)
				continue
			}

			popOfRoute := parts[1]

			updateDataString := strings.Join(parts[2:], " ") // Extract the JSON data
			updateData := []byte(updateDataString)

			var newRoutes []string
			if handleError(json.Unmarshal(updateData, &newRoutes), "Failed to parse UPDATE data from client %s", clientAddr) {
				continue
			}
			log.Printf("Received UPDATE from %s for POP %s", clientAddr, popOfRoute)

		case "REQUEST":
			if len(parts) < 2 {
				log.Printf("REQUEST command from client %s is missing a video name", clientAddr)
				continue
			}

			contentName := parts[1]
			popOfRoute := parts[2]
			clientName := parts[3]

			if _, exists := clientsNode[contentName]; !exists {
				clientsNode[contentName] = make(map[string][]net.Addr)
			}

			log.Printf("REQUEST for content \"%s\" from POP %s from client %s", contentName, popOfRoute, node.Neighbors[clientName])

			addClientName(contentName, clientName, clientsName, &clientsMu, *node)

			addClientAddressNode(contentName, popOfRoute, clientAddr, clientsNode, &clientsMu)

			if streams[contentName] == nil {
				reader, cleanup, err := startFFmpeg(ffmpegCommands, contentName)
				if err != nil {
					log.Fatalf("Error initializing ffmpeg: %v", err)
				}
				streams[contentName] = reader

				defer cleanup()

				stopChansMu.Lock()
				if _, exists := stopChans[contentName]; !exists {
					stopChans[contentName] = make(chan struct{})
					//log.Printf("Created new stop channel for content: %s", contentName)
				}
				// stopChan := stopChans[contentName]

				if len(stopChans) == 0 {
					log.Println("No active stop channels.")
					return
				}

				log.Println("Active stop channels:")
				for contentName := range stopChans {
					log.Printf("Content: %s", contentName)
				}

				stopChansMu.Unlock()

				// Forward the stream to the client

				// Send RTP packets and handle errors
				go func() {
					err := sendRTPPackets(protocolConn, reader, contentName, clientsNode, stopChans[contentName])
					if err != nil {
						log.Printf("Error sending RTP packets to %v for content \"%s\": %v", clientAddr, contentName, err)

						// Catch if stream has ended
						if err.Error() == "end of stream reached" {

							log.Printf("LIST OF CLIENTS: %v", clientsName[contentName])
							sendEndStreamClientsCS(protocolConn, contentName, popOfRoute, clientsName)

							ffmpegCommands[contentName], err = prepareFFmpegCommand(videos[contentName])
							if err != nil {
								log.Fatalf("Error creating ffmpeg for content \"%s\": %v", contentName, err)
							}

							clientsMu.Lock()
							delete(clients, contentName) // Removes contentName from map and releases memory
							clientsMu.Unlock()

							delete(streams, contentName)
							defer cleanup()

						}
					}
				}()
			}

		default:
			log.Printf("Unknown message from %s: %s", clientAddr, clientMessage)
		}
	}
}

func sendRTPPackets(conn *net.UDPConn, reader *bufio.Reader, contentName string, clients map[string]map[string][]net.Addr, stopChan chan struct{}) error {
	seqNumber := uint16(0)
	ssrc := uint32(1234)
	payloadType := uint8(96) // Dynamic payload type for video
	maxBufferSize := 65535   // Maximum allowable RTP payload size

	for {

		select {
		case <-stopChan:
			// Stop signal received, terminate the goroutine
			log.Printf("Stopping Sending From ENDSTREAM_UP")
			return nil
		default:
			// Read one frame from the reader
			var buf bytes.Buffer
			for {

				b, err := reader.ReadByte()
				if err != nil {
					if err == io.EOF {
						log.Println("End of stream reached.")
						return fmt.Errorf("end of stream reached")
					}
					log.Printf("Error reading byte: %v", err)
					return fmt.Errorf("error reading byte: %w", err)
				}

				buf.WriteByte(b)

				// Detect end of JPEG frame (FF D9 marks the end of a JPEG frame)
				if buf.Len() > 2 && buf.Bytes()[buf.Len()-2] == 0xFF && buf.Bytes()[buf.Len()-1] == 0xD9 {
					break
				}

				// Check buffer size to prevent overflows
				if buf.Len() > maxBufferSize {
					log.Printf("Frame exceeds max buffer size (%d bytes). Discarding.", maxBufferSize)
					return fmt.Errorf("frame exceeds max buffer size (%d bytes)", maxBufferSize)
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
				return fmt.Errorf("failed to marshal RTP packet: %w", err)
			}

			clientsMu.Lock() // Lock the client list for safe access

			sentClients := make(map[string]struct{}) // Set to track already sent clients

			for _, popClients := range clients[contentName] { // Iterate over all POPs
				for _, client := range popClients {
					clientAddr := client.String() // Get a unique string identifier for the client (e.g., IP address or identifier)

					// Skip sending to this client if it has already been sent to
					if _, alreadySent := sentClients[clientAddr]; alreadySent {
						continue
					}

					// Send the packet to the client
					_, err := conn.WriteTo(packetData, client)
					if err != nil {
						log.Printf("Failed to send packet to %v: %v", client, err)
					} else {
						// Log packet details after successful send
						// log.Printf("Sent RTP packet to %v - Seq=%d, Timestamp=%d, Size=%d bytes",
						// client, packet.SequenceNumber, packet.Timestamp, len(packet.Payload))
					}

					// Mark the client as having been sent to
					sentClients[clientAddr] = struct{}{}
				}
			}

			clientsMu.Unlock() // Unlock the client list

			// Increment sequence number for the next packet
			seqNumber++

			// Wait for the next frame (approximately 30 FPS)
			time.Sleep(time.Millisecond * 33)
		}
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

	log.Println("Setup UDP connection Successful")
	return conn, nil
}
func forwardToClientsNode(conn *net.UDPConn, contentConn *net.UDPConn, clients map[string][]net.Addr, stopChan chan struct{}) {
	buf := make([]byte, 150000)
	for {
		select {
		case <-stopChan:
			// Stop signal received, terminate the goroutine
			log.Printf("Stopping forwardToClientsNode")
			return
		default:
			// Read data from the content connection
			n, _, err := contentConn.ReadFromUDP(buf)
			if err != nil {
				log.Printf("Error reading from Content Server: %v", err)
				return
			}

			// Forward data to all clients
			clientsMu.Lock()
			for _, clientAddrs := range clients {
				for _, clientAddr := range clientAddrs {
					_, err := conn.WriteTo(buf[:n], clientAddr)
					if err != nil {
						log.Printf("Failed to forward packet to %v: %v", clientAddr, err)
					}
				}
			}
			clientsMu.Unlock()
		}
	}
}

// Forwards data from POP to connected clients
func forwardToClients(conn *net.UDPConn, contentConn *net.UDPConn, popOfRoute string, clients map[string][]net.Addr, stopChan chan struct{}) {
	buf := make([]byte, 150000)
	for {

		select {
		case <-stopChan:
			// Stop signal received, terminate the goroutine
			log.Printf("Stopping forwardToClientsNode")
			return
		default:

			n, _, err := contentConn.ReadFromUDP(buf)
			if err != nil {
				log.Printf("Error reading from Content Server: %v", err)
				return
			}

			//log.Printf("POP received packet from Content Server - Size=%d bytes", n)

			clientsMu.Lock()
			for _, clientAddr := range clients[popOfRoute] {
				_, err := conn.WriteTo(buf[:n], clientAddr)
				if err != nil {
					log.Printf("Failed to forward packet to %v: %v", clientAddr, err)
				} else {
					//log.Printf("POP forwarded packet to %v - Size=%d bytes", clientAddr, n)
				}
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

// Function to extract the first element and return the rest as JSON
func ExtractFirstElement(jsonData []byte) (string, []byte, error) {
	var data []string
	err := json.Unmarshal(jsonData, &data)
	if err != nil {
		return "", nil, err
	}

	// Check if the slice is empty
	if len(data) == 0 {
		return "", nil, fmt.Errorf("empty JSON array")
	}

	first := data[0]
	rest := data[1:]

	restJSON, err := json.Marshal(rest)
	if err != nil {
		return "", nil, err
	}

	return first, restJSON, nil
}
func updateRoutingTable(routingTable map[string]string, valueToUpdate string, ipNextHop string) {
	routingTable[valueToUpdate] = ipNextHop
	log.Printf("Update Routing Table for: %s\n", valueToUpdate)
}

func getNextInRouteAddr(nextInRouteIp string) (*net.UDPAddr, error) {
	// Parse the IP address
	parsedIP := net.ParseIP(nextInRouteIp)
	if parsedIP == nil {
		return nil, fmt.Errorf("invalid IP address: %s", nextInRouteIp)
	}

	// Create a UDPAddr
	return &net.UDPAddr{
		IP:   parsedIP,
		Port: 8000,
	}, nil
}

func sendUpdatePacket(conn *net.UDPConn, popOfRoute string, jsonData []byte, nextInRoute net.Addr) error {

	if conn == nil {
		return fmt.Errorf("connection is nil; cannot send update to %s", nextInRoute)
	}

	message := fmt.Sprintf("UPDATE %s ", popOfRoute)

	msgBytes := []byte(message)

	finalMessage := append(msgBytes, jsonData...)

	// Send the request message
	_, err := conn.WriteTo(finalMessage, nextInRoute)

	if err != nil {
		return fmt.Errorf("failed to send update: %w", err)
	}
	//log.Printf("Sent update to %s\n", nextInRoute)
	return nil
}

func main() {
	nodeName := flag.String("name", "", "Node name")
	ip := flag.String("ip", "0.0.0.0", "IP to open on for testing")
	port := flag.Int("port", 8000, "UDP port to listen on")
	nodeType := flag.String("type", "Node", "Node type (POP, Node, CS)")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	log.SetFlags(log.Ltime)

	verboseFlag = *verbose

	fmt.Printf("VERBOSE: %v", verboseFlag)

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

	node.initialize("10.0.2.2:8080")

	log.Printf("Node %s initialized with neighbors: %v", node.Name, node.Neighbors)

	switch node.Type {
	case "POP":
		protocolConn, err := setupUDPListener(*ip, node.Port)
		if handleError(err, "Error setting up UDP listener on port %d", node.Port) {
			return
		}
		defer protocolConn.Close()

		routingTable = make(map[string]string)

		go node.handleConnectionsPOP(protocolConn, routingTable, node.Neighbors, node.Name)

		// Block indefinitely to keep the node running
		select {}

	case "NODE":

		fmt.Printf("Node %s initialized as a regular node with neighbors: %v\n", node.Name, node.Neighbors)

		// routingTable [ "Nome da Stream" ] = "IP Do Nodo onde ir buscar a stream"
		routingTable = make(map[string]string)

		// abrir porta udp para escuta de pedidos
		protocolConn, err := setupUDPListener(*ip, node.Port)
		if err != nil {
			log.Fatalf("Error setting up UDP listener: %v", err)
		}
		defer protocolConn.Close()

		// esperar por conexao
		go node.handleConnectionsNODE(protocolConn, routingTable, node.Neighbors)

		select {}

	case "CS":

		videos = make(map[string]string)
		streams := make(map[string]*bufio.Reader)
		LoadJSONToMap("streams.json", videos)

		routingTable = make(map[string]string)

		ffmpegCommands, err := prepareFFmpegCommands(videos)
		if err != nil {
			log.Fatalf("Error creating ffmpeg commands for streams: %v", err)
		}

		conn, err := setupUDPListener(*ip, node.Port)
		if err != nil {
			log.Fatalf("Error setting up UDP listener: %v", err)
		}
		defer conn.Close()

		go node.handleConnectionsCS(conn, streams, ffmpegCommands)

		select {}

	default:
		log.Fatalf("Unknown node type: %s", node.Type)
	}
}
