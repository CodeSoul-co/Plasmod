package main

import (
	"log"

	"andb/src/internal/app"
)

func main() {
	s, cleanup, err := app.BuildServer()
	if err != nil {
		log.Fatalf("build server failed: %v", err)
	}
	defer func() {
		if cleanup != nil {
			if err := cleanup(); err != nil {
				log.Printf("storage cleanup: %v", err)
			}
		}
	}()
	log.Printf("ANDB server listen on %s", s.Addr)
	if err := s.ListenAndServe(); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
