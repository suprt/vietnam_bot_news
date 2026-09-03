package main

import (
	"context"
	"log"
	"os"

	"github.com/maine/vietnam_bot_news/internal/config"
	"github.com/maine/vietnam_bot_news/internal/state"
)

func main() {
	if err := config.LoadEnvFile(); err != nil {
		log.Fatalf("load environment: %v", err)
	}

	key := os.Getenv("STATE_ENCRYPTION_KEY")
	idCipher, err := state.NewIDCipher(key)
	if err != nil {
		log.Fatalf("create state encryption cipher: %v", err)
	}

	store := state.NewFileStore("state/state.json")
	current, err := store.Load(context.Background())
	if err != nil {
		log.Fatalf("load state: %v", err)
	}

	migrated, count, err := idCipher.MigrateRecipients(current)
	if err != nil {
		log.Fatalf("migrate recipients: %v", err)
	}
	if err := store.Save(context.Background(), migrated); err != nil {
		log.Fatalf("save state: %v", err)
	}

	log.Printf("state migration completed: %d recipient(s) encrypted", count)
}
