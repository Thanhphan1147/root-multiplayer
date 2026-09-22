// Command rmn-mp runs the Root Machine Notation correspondence-play server and
// the administrator CLI used to arrange rooms.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/Thanhphan1147/root-multiplayer/internal/httpapi"
	"github.com/Thanhphan1147/root-multiplayer/internal/room"
)

func main() {
	if len(os.Args) < 2 || strings.HasPrefix(os.Args[1], "-") {
		serve(os.Args[1:])
		return
	}
	switch os.Args[1] {
	case "serve":
		serve(os.Args[2:])
	case "create-room":
		createRoom(os.Args[2:])
	case "rooms":
		listRooms(os.Args[2:])
	case "add-bot":
		addBot(os.Args[2:])
	case "kick":
		kick(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `rmn-mp - correspondence ROOT server and administrator CLI

Usage:
  rmn-mp serve --addr :8080 --data ./data --web ./web
  rmn-mp create-room --data ./data [--name "Friday game"]
  rmn-mp add-bot --data ./data --room <id> --seat <n> [--faction MC] [--bot greedy:full]
  rmn-mp rooms --data ./data
  rmn-mp kick --data ./data --room <room-id> --seat <n>

Rooms are created only by the administrator and always have four seats. Players
take seats in the browser; the game starts with 2-4 seated players once they have
all picked a faction. Seats filled with add-bot are played by the server.
`)
}

func newStore(data string) *room.Store {
	return &room.Store{Dir: data, Secret: []byte(os.Getenv("RMN_SECRET"))}
}

func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "listen address")
	dataDir := fs.String("data", "data", "room data directory")
	webDir := fs.String("web", "web", "static client directory")
	_ = fs.Parse(args)

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

func createRoom(args []string) {
	fs := flag.NewFlagSet("create-room", flag.ExitOnError)
	dataDir := fs.String("data", "data", "room data directory")
	name := fs.String("name", "", "optional room name")
	_ = fs.Parse(args)

	rm, err := newStore(*dataDir).Create(*name)
	if err != nil {
		log.Fatal(err)
	}
	label := rm.ID
	if rm.Name != "" {
		label = fmt.Sprintf("%s (%s)", rm.ID, rm.Name)
	}
	fmt.Printf("created room %s (four seats)\n", label)
}

func listRooms(args []string) {
	fs := flag.NewFlagSet("rooms", flag.ExitOnError)
	dataDir := fs.String("data", "data", "room data directory")
	_ = fs.Parse(args)

	list, err := newStore(*dataDir).List()
	if err != nil {
		log.Fatal(err)
	}
	if len(list) == 0 {
		fmt.Println("no rooms")
		return
	}
	for _, rm := range list {
		status := "lobby"
		if rm.Started {
			status = "in progress"
		}
		label := rm.ID
		if rm.Name != "" {
			label = fmt.Sprintf("%s (%s)", rm.ID, rm.Name)
		}
		fmt.Printf("%s  %-11s seats:", label, status)
		for _, st := range rm.Seats {
			who := st.Faction
			if who == "" {
				if st.Occupied {
					who = "taken"
				} else {
					who = "free"
				}
			}
			fmt.Printf(" %d=%s", st.Index+1, who)
		}
		fmt.Println()
	}
}

func kick(args []string) {
	fs := flag.NewFlagSet("kick", flag.ExitOnError)
	dataDir := fs.String("data", "data", "room data directory")
	roomID := fs.String("room", "", "room id")
	seat := fs.Int("seat", 0, "seat number (1-based, as shown by `rooms`)")
	_ = fs.Parse(args)
	if *roomID == "" || *seat < 1 {
		log.Fatal("usage: rmn-mp kick --data DIR --room ID --seat N")
	}

	store := newStore(*dataDir)
	rm, _, err := store.Load(*roomID)
	if err != nil {
		log.Fatalf("room %s: %v", *roomID, err)
	}
	if *seat > len(rm.Seats) {
		log.Fatalf("room %s has only %d seats", *roomID, len(rm.Seats))
	}
	seatID := rm.Seats[*seat-1].ID
	faction := rm.Seats[*seat-1].Faction
	updated, _, err := store.Kick(*roomID, seatID)
	if err != nil {
		log.Fatal(err)
	}
	msg := fmt.Sprintf("removed seat %d from room %s", *seat, *roomID)
	if faction != "" {
		msg += fmt.Sprintf(" (dropped %s from the game)", faction)
	}
	fmt.Printf("%s; %d seats remain\n", msg, len(updated.Seats))
}

func addBot(args []string) {
	fs := flag.NewFlagSet("add-bot", flag.ExitOnError)
	dataDir := fs.String("data", "data", "room data directory")
	roomID := fs.String("room", "", "room id")
	seat := fs.Int("seat", 0, "seat number (1-based, as shown by `rooms`)")
	faction := fs.String("faction", "", "faction (MC|ED|WA|VB); default: first free")
	spec := fs.String("bot", "greedy:full", "bot spec: random | passive | greedy:<profile> | mcts:<profile>")
	_ = fs.Parse(args)
	if *roomID == "" || *seat < 1 {
		log.Fatal("usage: rmn-mp add-bot --data DIR --room ID --seat N [--faction MC] [--bot greedy:full]")
	}

	store := newStore(*dataDir)
	rm, _, err := store.Load(*roomID)
	if err != nil {
		log.Fatalf("room %s: %v", *roomID, err)
	}
	if *seat > len(rm.Seats) {
		log.Fatalf("room %s has only %d seats", *roomID, len(rm.Seats))
	}
	seatID := rm.Seats[*seat-1].ID
	updated, g, err := store.AddBot(*roomID, seatID, *faction, *spec)
	if err != nil {
		log.Fatal(err)
	}
	msg := fmt.Sprintf("seated bot %q as %s in seat %d of room %s",
		*spec, updated.Seats[*seat-1].Faction, *seat, *roomID)
	if updated.Started && g != nil {
		msg += " (game started)"
	}
	fmt.Println(msg)
}
