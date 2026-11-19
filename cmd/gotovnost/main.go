package main

import (
	"flag"
	"log"

	"gotovnost/internal/app"
)

func main() {
	dataDir := flag.String("dir", "data", "папка с данными")
	listenAddress := flag.String("addr", "127.0.0.1:8080", "адрес прослушивания")
	flag.Parse()

	if err := app.Run(*dataDir, *listenAddress); err != nil {
		log.Fatal(err)
	}
}
