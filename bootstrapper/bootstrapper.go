package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
)

// Função para ler o arquivo de configuração JSON
func readConfig() map[string][]string {
	byteValue, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatal(err)
	}

	var result map[string][]string
	if err := json.Unmarshal(byteValue, &result); err != nil {
		log.Fatal(err)
	}

	return result
}

// Função para lidar com cada conexão TCP
func handleConnection(conn net.Conn, networkData map[string][]string) {
	defer conn.Close()

	// Buffer para ler o nome do nó enviado pelo cliente
	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		log.Printf("Erro ao ler do cliente: %v\n", err)
		return
	}

	// Obter o nome do nó do buffer
	nodeName := string(buffer[:n])

	// Buscar a lista de vizinhos para o nome do nó
	neighbors, exists := networkData[nodeName]
	if !exists {
		neighbors = []string{} // Caso o nome não exista no mapa, retornar uma lista vazia
	}

	// Serializar a lista de vizinhos em JSON
	response, err := json.Marshal(neighbors)
	if err != nil {
		log.Printf("Erro ao serializar resposta: %v\n", err)
		return
	}

	// Enviar a resposta para o cliente
	_, err = conn.Write(response)
	if err != nil {
		log.Printf("Erro ao enviar resposta para o cliente: %v\n", err)
		return
	}

	fmt.Printf("Enviado para %s: %v\n", nodeName, neighbors)
}

func main() {
	// Carregar o mapa de configuração da rede
	networkData := readConfig()

	// Iniciar o servidor TCP na porta 8080
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatal("Erro ao iniciar o servidor TCP:", err)
	}
	defer listener.Close()

	fmt.Println("Servidor TCP a escutar na porta 8080...")

	// Loop para aceitar conexões
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Erro ao aceitar conexão:", err)
			continue
		}

		// Iniciar uma nova goroutine para tratar cada conexão
		go handleConnection(conn, networkData)
	}
}
