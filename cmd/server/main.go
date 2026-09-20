// Command server runs the Root Machine Notation correspondence-play server.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/Thanhphan1147/root-multiplayer/internal/httpapi"
	"github.com/Thanhphan1147/root-multiplayer/internal/room"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", "data", "room data directory")
	webDir := flag.String("web", "web", "static client directory")
	flag.Parse()

	secret := os.Getenv("RMN_SECRET")
	if secret == "" {
		secret = "dev-secret-change-me"
		log.Println("warning: RMN_SECRET is not set; using an insecure development secret")
	}

	store := &room.Store{Dir: *dataDir, Secret: []byte(secret)}
	srv := &httpapi.Server{Store: store, WebDir: *webDir}

	log.Printf("root-multiplayer listening on %s (data=%s, web=%s)", *addr, *dataDir, *webDir)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
