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
	Type      string            // Tipo do nó (POP, Node, Content Server)
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

	// Exibir os vizinhos e o tipo de nó recebido
	fmt.Printf("Nó %s (Tipo: %s) - Vizinhos armazenados: %v\n", node.Name, node.Type, node.Neighbors)
}

func main() {
	// Definir as flags para o nome do nó, porta UDP e tipo de nó
	nodeName := flag.String("name", "", "Nome do nó")
	port := flag.Int("port", 30000, "Porta UDP para escutar mensagens")
	nodeType := flag.String("type", "Node", "Tipo do nó (POP, Node, Content Server)")

	// Fazer o parsing das flags
	flag.Parse()

	// Verificar se o nome do nó foi fornecido
	if *nodeName == "" {
		log.Fatal("Nome do nó não fornecido. Uso: go run node.go -name <NodeName> -port <Port> -type <Type>")
	}

	// Inicializar a estrutura do nó com a porta e tipo especificados
	node := Node{
		Name:      *nodeName,
		Type:      *nodeType,
		Neighbors: make(map[string]string),
		Port:      *port,
	}

	// Inicializar o nó e obter a lista de vizinhos
	node.initialize("localhost:8080") // Alterar "localhost" pelo IP do bootstrapper, se necessário

	// Manter o programa ativo
	select {} // Aguarda indefinidamente
}
