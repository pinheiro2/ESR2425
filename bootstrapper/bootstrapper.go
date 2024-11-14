package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
)

// Função para ler o arquivo de configuração JSON
func readConfig(configFilePath string) (map[string][]string, map[string]string) {
	byteValue, err := os.ReadFile(configFilePath)
	if err != nil {
		log.Fatalf("Erro ao ler o arquivo de configuração: %v\n", err)
	}

	// Estrutura para armazenar o mapeamento
	var result struct {
		Neighbors map[string][]string `json:"neighbors"`
		Addresses map[string]string   `json:"addresses"`
	}

	// Fazer unmarshal do JSON
	if err := json.Unmarshal(byteValue, &result); err != nil {
		log.Fatalf("Erro ao desserializar o JSON: %v\n", err)
	}

	// Verificar se os mapas foram carregados corretamente
	if result.Neighbors == nil || result.Addresses == nil {
		log.Fatal("Erro: 'neighbors' ou 'addresses' não encontrados na configuração.")
	}

	fmt.Printf("Configuração lida do arquivo:\nVizinhos: %v\nEndereços: %v\n\n", result.Neighbors, result.Addresses)

	return result.Neighbors, result.Addresses
}

// Função para lidar com cada conexão TCP
func handleConnection(conn net.Conn, networkData map[string][]string, addressMap map[string]string) {
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
	neighborNames, exists := networkData[nodeName]
	if !exists {
		neighborNames = []string{} // Caso o nome não exista no mapa, retornar uma lista vazia
	}

	// Montar um mapa nome -> IP para os vizinhos
	neighbors := make(map[string]string)
	for _, neighborName := range neighborNames {
		if ip, ok := addressMap[neighborName]; ok {
			neighbors[neighborName] = ip
		} else {
			log.Printf("Aviso: IP para o nó %s não encontrado em 'addresses'.\n", neighborName)
		}
	}

	// Serializar o mapa de vizinhos em JSON
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
	// Definir um argumento de linha de comando para o caminho do arquivo de configuração
	configFilePath := flag.String("config", "config.json", "Path to the configuration file")
	flag.Parse()

	// Carregar o mapa de configuração da rede e os endereços IP dos nós
	networkData, addressMap := readConfig(*configFilePath)

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
		go handleConnection(conn, networkData, addressMap)
	}
}
