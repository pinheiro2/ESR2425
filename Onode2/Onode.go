package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
)

// Estrutura para armazenar informações do nó
type Node struct {
	Name      string            // Nome do nó
	Neighbors map[string]string // Mapa de vizinhos: chave é o nome do nó e valor é o IP
	Port      int               // Porta UDP que o nó usará
}

// Função para inicializar o nó e obter a lista de vizinhos
func (node *Node) initialize(bootstrapAddress string) {
	// Conectar ao bootstrapper
	conn, err := net.Dial("tcp", bootstrapAddress)
	if err != nil {
		log.Fatal("Erro ao conectar ao bootstrapper:", err)
	}
	defer conn.Close()

	// Enviar o nome do nó para o bootstrapper
	_, err = conn.Write([]byte(node.Name))
	if err != nil {
		log.Fatal("Erro ao enviar nome do nó:", err)
	}

	// Receber a lista de vizinhos (mapa de nome -> IP) em JSON
	buffer := make([]byte, 4096)
	n, err := conn.Read(buffer)
	if err != nil {
		log.Fatal("Erro ao ler resposta do bootstrapper:", err)
	}

	// Deserializar a resposta JSON para obter o mapa de vizinhos
	var neighbors map[string]string
	if err := json.Unmarshal(buffer[:n], &neighbors); err != nil {
		log.Fatal("Erro ao desserializar resposta:", err)
	}

	// Armazenar os vizinhos na estrutura do nó
	node.Neighbors = neighbors

	// Exibir os vizinhos recebidos
	fmt.Printf("Nó %s - Vizinhos armazenados: %v\n", node.Name, node.Neighbors)
}

// Função para iniciar um listener UDP na porta especificada
func (node *Node) startUDPListener() {
	// Configurar o endereço UDP com a porta especificada
	udpAddress := fmt.Sprintf(":%d", node.Port)
	addr, err := net.ResolveUDPAddr("udp", udpAddress)
	if err != nil {
		log.Fatal("Erro ao resolver endereço UDP:", err)
	}

	// Iniciar o listener UDP
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal("Erro ao iniciar listener UDP:", err)
	}
	defer conn.Close()

	fmt.Printf("Nó %s ouvindo mensagens UDP na porta %d\n", node.Name, node.Port)

	// Loop para receber mensagens UDP
	buffer := make([]byte, 1024)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("Erro ao ler mensagem UDP: %v\n", err)
			continue
		}

		// Exibir a mensagem recebida e o endereço de origem
		message := string(buffer[:n])
		fmt.Printf("Nó %s recebeu mensagem de %s: %s\n", node.Name, remoteAddr, message)
	}
}

func main() {
	// Definir as flags para o nome do nó e a porta UDP
	nodeName := flag.String("name", "", "Nome do nó")
	port := flag.Int("port", 30000, "Porta UDP para escutar mensagens")

	// Fazer o parsing das flags
	flag.Parse()

	// Verificar se o nome do nó foi fornecido
	if *nodeName == "" {
		log.Fatal("Nome do nó não fornecido. Uso: go run node.go -name <NodeName> -port <Port>")
	}

	// Inicializar a estrutura do nó com a porta especificada
	node := Node{
		Name:      *nodeName,
		Neighbors: make(map[string]string),
		Port:      *port,
	}

	// Inicializar o nó e obter a lista de vizinhos
	node.initialize("localhost:8080") // Alterar "localhost" pelo IP do bootstrapper, se necessário

	// Iniciar o listener UDP em uma goroutine
	go node.startUDPListener()

	// Manter o programa ativo
	select {} // Aguarda indefinidamente
}
