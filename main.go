package main

import (
	"embed"
	"flag"
	"log"
	"net/http"
)

//go:embed templates/*.html prompt.tmpl risks.tmpl static
var embeddedFiles embed.FS

func main() {
	dataPath := flag.String("data", "data.json", "путь к файлу с данными")
	listenAddress := flag.String("addr", "127.0.0.1:8080", "адрес сервера")
	flag.Parse()

	store, err := NewStore(*dataPath)
	if err != nil {
		log.Fatalf("старт не удался: %v", err)
	}

	server, err := NewServer(store, embeddedFiles)
	if err != nil {
		log.Fatalf("шаблоны: %v", err)
	}

	// Binds to localhost only: there is no authentication, so it must not be reachable from the network.
	log.Printf("запущено: http://%s", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, server.Routes()))
}
