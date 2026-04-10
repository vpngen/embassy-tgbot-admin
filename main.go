package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/vpngen/embassy-tgbot-admin/internal/auth"
	"github.com/vpngen/embassy-tgbot-admin/internal/handler"
	"github.com/vpngen/embassy-tgbot-admin/internal/storage"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	flowsDir := flag.String("flows-dir", "flows", "directory for flow JSON files")
	botWebhook := flag.String("bot-webhook", "", "URL to POST when flows are published (optional)")
	usersFile := flag.String("users-file", "users.json", "path to users JSON file")
	addUser := flag.String("add-user", "", "add/update user: username:password, then exit")
	flag.Parse()

	if v := os.Getenv("ADMIN_ADDR"); v != "" {
		*addr = v
	}
	if v := os.Getenv("ADMIN_FLOWS_DIR"); v != "" {
		*flowsDir = v
	}
	if v := os.Getenv("ADMIN_BOT_WEBHOOK"); v != "" {
		*botWebhook = v
	}
	if v := os.Getenv("ADMIN_USERS_FILE"); v != "" {
		*usersFile = v
	}

	usersPath := *usersFile

	a, err := auth.New(usersPath)
	if err != nil {
		log.Fatalf("init auth: %s", err)
	}

	// CLI: add user and exit
	if *addUser != "" {
		parts := splitOnce(*addUser, ':')
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			log.Fatal("--add-user format: username:password")
		}
		if err := a.AddUser(parts[0], parts[1]); err != nil {
			log.Fatalf("add user: %s", err)
		}
		fmt.Printf("user %q added/updated in %s\n", parts[0], usersPath)
		return
	}

	if !a.HasUsers() {
		log.Fatal("no users configured. Use --add-user username:password to create one")
	}

	store, err := storage.NewFileStore(*flowsDir)
	if err != nil {
		log.Fatalf("init storage: %s", err)
	}

	h := handler.New(store, a, *botWebhook)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	log.Printf("admin panel listening on %s (flows in %s)", *addr, *flowsDir)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("server error: %s", err)
	}
}

func splitOnce(s string, sep byte) []string {
	i := 0
	for i < len(s) && s[i] != sep {
		i++
	}
	if i == len(s) {
		return []string{s}
	}
	return []string{s[:i], s[i+1:]}
}
