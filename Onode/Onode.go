package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

func main() {
	// Define uma flag para receber a porta como argumento
	port := flag.String("port", "9000", "Porta na qual o nó vai escutar")
	flag.Parse()

	// Inicia o servidor na porta recebida como argumento
	go startUDPServer(*port)

	// Dá tempo para o servidor iniciar
	time.Sleep(1 * time.Second)

	// Pergunta ao usuário o endereço do nó ao qual ele quer se conectar
	fmt.Print("Digite o endereço do nó para se conectar (ex: localhost:9001) ou pressione Enter para escutar apenas: ")
	reader := bufio.NewReader(os.Stdin)
	targetNode, _ := reader.ReadString('\n')
	targetNode = strings.TrimSpace(targetNode)

	// Se o usuário forneceu um endereço, tenta enviar dados para o nó
	if targetNode != "" {
		startUDPClient(targetNode)
	}

	// Mantém o nó ativo para aceitar mensagens
	select {}
}

// Função que inicia o servidor UDP
func startUDPServer(port string) {
	address := ":" + port
	// Cria um listener UDP
	conn, err := net.ListenPacket("udp", address)
	if err != nil {
		fmt.Println("Erro ao iniciar o servidor na porta", port, ":", err)
		os.Exit(1)
	}
	defer conn.Close()
	fmt.Println("Servidor UDP iniciado e escutando na porta", port)

	buf := make([]byte, 1024)
	for {
		// Lê mensagens recebidas no servidor
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			fmt.Println("Erro ao ler mensagem:", err)
			continue
		}

		message := strings.TrimSpace(string(buf[:n]))
		fmt.Printf("Servidor recebeu de %s: %s\n", addr.String(), message)

		// Responde com "HELLO" se a mensagem for "HELLO"
		var response string
		if message == "HELLO" {
			response = "HELLO\n"
		} else {
			response = "Mensagem recebida!\n"
		}

		// Envia resposta ao cliente
		conn.WriteTo([]byte(response), addr)
	}
}

// Função que inicia o cliente UDP e envia mensagens a outro nó
func startUDPClient(targetNode string) {
	// Resolve o endereço do nó alvo
	addr, err := net.ResolveUDPAddr("udp", targetNode)
	if err != nil {
		fmt.Println("Erro ao resolver endereço do nó:", err)
		return
	}

	// Cria uma conexão UDP para enviar/receber pacotes
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		fmt.Println("Erro ao conectar ao nó:", err)
		return
	}
	defer conn.Close()

	// Usa uma Goroutine para escutar respostas do nó (modo full-duplex)
	go func() {
		buf := make([]byte, 1024)
		for {
			// Lê a resposta do nó
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				fmt.Println("Erro ao ler do nó:", err)
				return
			}
			fmt.Println("Cliente recebeu:", strings.TrimSpace(string(buf[:n])))
		}
	}()

	// Envia a mensagem "HELLO" ao nó conectado
	fmt.Println("Cliente enviando: HELLO")
	conn.Write([]byte("HELLO\n"))

	// Mantém a conexão aberta e permite que o cliente envie mais mensagens
	inputReader := bufio.NewReader(os.Stdin)
	for {
		// Lê o que o usuário digitar e envia ao nó
		fmt.Print("Digite mensagem para enviar: ")

		userInput, _ := inputReader.ReadString('\n')
		userInput = strings.TrimSpace(userInput)
		if userInput == "exit" {
			fmt.Println("Cliente fechando conexão...")
			return
		}
		conn.Write([]byte(userInput + "\n"))
	}
}
