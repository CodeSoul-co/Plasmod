package main

import (
	"log"

	"plasmod/src/internal/app"
)

func main() {
	servers, shutdown, err := app.BuildServer()
	if err != nil {
		log.Fatalf("build server failed: %v", err)
	}
	defer func() {
		if err := shutdown(); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()
	if err := app.RunServers(servers); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
