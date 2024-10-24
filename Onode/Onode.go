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
	// Define a flag para receber a porta como argumento
	port := flag.String("port", "9000", "Porta na qual o nó vai escutar")
	flag.Parse()

	// Inicia o servidor na porta recebida como argumento
	go startServer(*port)

	// Dá tempo para o servidor iniciar
	time.Sleep(1 * time.Second)

	// Pergunta ao usuário o endereço do nó ao qual ele quer se conectar
	fmt.Print("Digite o endereço do nó para se conectar (ex: localhost:9001) ou pressione Enter para escutar apenas: ")
	reader := bufio.NewReader(os.Stdin)
	targetNode, _ := reader.ReadString('\n')
	targetNode = strings.TrimSpace(targetNode)

	// Se o usuário forneceu um endereço, tenta se conectar ao nó
	if targetNode != "" {
		startClient(targetNode)
	}

	// Mantém o nó ativo para aceitar conexões
	select {}
}

// Função que inicia o servidor TCP
func startServer(port string) {
	address := ":" + port
	// Escuta na porta especificada
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Println("Erro ao iniciar o servidor na porta", port, ":", err)
		os.Exit(1)
	}
	defer listener.Close()
	fmt.Println("Servidor iniciado e escutando na porta", port)

	// Aceitar conexões
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Erro ao aceitar conexão:", err)
			continue
		}
		// Inicia uma Goroutine para lidar com cada conexão
		go handleConnection(conn)
	}
}

// Função para tratar a conexão recebida no servidor
func handleConnection(conn net.Conn) {
	defer conn.Close()

	// Cria um leitor para a conexão
	reader := bufio.NewReader(conn)
	for {
		// Lê a mensagem recebida do cliente
		message, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Erro ao ler mensagem:", err)
			break
		}
		message = strings.TrimSpace(message)
		fmt.Println("Servidor recebeu:", message)

		// Responde com "HELLO" se a mensagem foi recebida
		if message == "HELLO" {
			conn.Write([]byte("HELLO\n"))
		} else {
			conn.Write([]byte("Mensagem recebida!\n"))
		}
	}
}

// Função que inicia o cliente TCP e se conecta a outro nó
func startClient(targetNode string) {
	// Conecta ao nó especificado
	conn, err := net.Dial("tcp", targetNode)
	if err != nil {
		fmt.Println("Erro ao conectar ao nó:", err)
		return
	}
	defer conn.Close()

	// Usa uma Goroutine para escutar respostas do nó (modo full-duplex)
	go func() {
		reader := bufio.NewReader(conn)
		for {
			// Lê a resposta do nó
			response, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println("Erro ao ler do nó:", err)
				return
			}
			fmt.Println("Cliente recebeu:", strings.TrimSpace(response))
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
