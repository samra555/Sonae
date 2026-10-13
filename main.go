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
	dataDir := flag.String("dir", "data", "папка с учётками и их данными")
	legacyDataPath := flag.String("data", "data.json", "старый файл данных: его получит первая созданная учётка")
	listenAddress := flag.String("addr", "127.0.0.1:8080", "адрес сервера")
	flag.Parse()

	accounts, err := OpenAccounts(*dataDir, *legacyDataPath)
	if err != nil {
		log.Fatalf("старт не удался: %v", err)
	}

	server, err := NewServer(accounts, embeddedFiles)
	if err != nil {
		log.Fatalf("шаблоны: %v", err)
	}

	// Binds to localhost only: accounts separate people sharing this computer, but the server
	// has no HTTPS and no brute-force protection, so it must not be reachable from the network.
	log.Printf("запущено: http://%s", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, server.Routes()))
}
