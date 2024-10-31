package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
)

func readConfig(result map[string][]string) {
	// Ler o conteúdo do ficheiro diretamente usando os.ReadFile
	byteValue, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatal(err)
	}

	// Fazer o unmarshal dos dados JSON para o mapa
	if err := json.Unmarshal(byteValue, &result); err != nil {
		log.Fatal(err)
	}

	// Imprimir os dados lidos
	for ip, vizinhos := range result {
		fmt.Printf("IP: %s, Vizinhos: %v\n", ip, vizinhos)
	}
}

func main() {
	// result é a estrutura com informação da rede
	var result map[string][]string
	readConfig(result)
}
