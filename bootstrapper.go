package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
)

func readConfig(result map[string][]string) {
	// Abrir o ficheiro JSON
	file, err := os.Open("config.json")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// Ler o conteúdo do ficheiro
	byteValue, err := ioutil.ReadAll(file)
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
	//result é a estrutura com informação da rede
	var result map[string][]string
	readConfig(result)
}
