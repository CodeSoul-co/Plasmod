package main

import (
	"log"

	"plasmod/src/internal/app"
)

func main() {
	bundle, err := app.BuildServer()
	if err != nil {
		log.Fatalf("build server failed: %v", err)
	}
	defer func() {
		if err := bundle.Shutdown(); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()
	if err := app.RunServers(bundle); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
