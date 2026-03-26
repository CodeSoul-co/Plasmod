package main

import (
	"log"

	"andb/src/internal/app"
)

func main() {
	srv, shutdown, err := app.BuildServer()
	if err != nil {
		log.Fatalf("build server failed: %v", err)
	}
	defer func() {
		if err := shutdown(); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()
	log.Printf("ANDB server listen on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
