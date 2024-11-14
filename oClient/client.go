package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os/exec"
	"time"

	"github.com/pion/rtp"
)

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
	err = sendContentRequest(conn, "stream1")
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
