package main

import (
	"log"

	"andb/src/internal/app"
)

func main() {
	s, err := app.BuildServer()
	if err != nil {
		log.Fatalf("build server failed: %v", err)
	}
	log.Printf("ANDB server listen on %s", s.Addr)
	if err := s.ListenAndServe(); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
