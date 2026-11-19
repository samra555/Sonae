package app

import (
	"fmt"
	"log"
	"net/http"

	"gotovnost/internal/database"
	"gotovnost/internal/transport"
)

// Run opens the accounts and starts the HTTP server. It is the assembly point:
// all dependencies are wired here and invoked from cmd/gotovnost.
func Run(dataDir, listenAddress string) error {
	accounts, err := database.OpenAccounts(dataDir)
	if err != nil {
		return fmt.Errorf("не удалось открыть учётки: %w", err)
	}

	server, err := transport.NewServer(accounts, transport.Files)
	if err != nil {
		return fmt.Errorf("не удалось создать сервер: %w", err)
	}

	log.Printf("иии запускаюсь: http://%s", listenAddress)
	return http.ListenAndServe(listenAddress, server.Routes())
}
