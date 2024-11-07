package main

import (
	"fmt"
	"log"
	"net"
	"os/exec"

	"github.com/pion/rtp"
)

func main() {
	// Set up UDP listener for RTP packets on port 5004
	addr := net.UDPAddr{
		Port: 5004,
		IP:   net.ParseIP("localhost"),
	}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		log.Fatalf("Failed to listen on UDP port: %v", err)
	}
	defer conn.Close()

	// Start ffplay to display the video frames
	ffplayCmd := exec.Command("ffplay", "-f", "mjpeg", "-i", "pipe:0")
	ffplayIn, err := ffplayCmd.StdinPipe()
	if err != nil {
		log.Fatalf("Failed to create ffplay input pipe: %v", err)
	}
	defer ffplayIn.Close()

	// Start ffplay
	if err := ffplayCmd.Start(); err != nil {
		log.Fatalf("Failed to start ffplay: %v", err)
	}

	// Buffer to hold incoming RTP packets
	packet := &rtp.Packet{}

	for {
		// Buffer for receiving packet data
		buf := make([]byte, 150000) // typical MTU size

		// Read packet from UDP connection
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			log.Printf("Error reading from UDP: %v", err)
			continue
		}

		// Unmarshal the RTP packet
		if err := packet.Unmarshal(buf[:n]); err != nil {
			log.Printf("Failed to unmarshal RTP packet: %v", err)
			continue
		}

		// Write the payload (JPEG frame) to ffplay
		_, err = ffplayIn.Write(packet.Payload)
		if err != nil {
			log.Printf("Failed to write to ffplay: %v", err)
			break
		}

		fmt.Printf("Received RTP packet: Seq=%d, Timestamp=%d\n", packet.SequenceNumber, packet.Timestamp)
	}

	// Wait for ffplay to finish
	if err := ffplayCmd.Wait(); err != nil {
		log.Fatalf("ffplay exited with error: %v", err)
	}
}
