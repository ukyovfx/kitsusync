package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"app/src/setup"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func main() {
	dbPath := flag.String("db", "data/sqlite.db", "KitsuSync SQLite path")
	host := flag.String("host", "http://127.0.0.1:8080", "Kitsu API host")
	email := flag.String("email", "", "owned runtime bot email")
	phase := flag.String("phase", "recover", "recover, prepare, or finalize")
	oldID := flag.String("old-id", "", "verified old bot ID")
	tempID := flag.String("temp-id", "", "verified replacement bot ID")
	tempEmail := flag.String("temp-email", "", "verified replacement email")
	flag.Parse()

	reader := bufio.NewReader(os.Stdin)
	readLine := func() string { value, _ := reader.ReadString('\n'); return strings.TrimSpace(value) }
	first := readLine()

	db, err := gorm.Open(sqlite.Open(*dbPath), &gorm.Config{})
	if err != nil {
		fail("database open failed")
	}
	switch *phase {
	case "recover":
		if err := setup.RecoverRuntimeCredentials(db, *host, *email, first); err != nil {
			fail(err.Error())
		}
	case "prepare":
		adminPassword := readLine()
		if id, err := setup.PrepareRuntimeBotReplacement(db, *host, *email, first, *tempEmail, adminPassword); err != nil {
			fail(err.Error())
		} else {
			fmt.Printf("REPLACEMENT_ID=%s\n", id)
		}
	case "finalize":
		adminPassword := first
		if err := setup.FinalizeRuntimeBotReplacement(db, *host, *email, adminPassword, *oldID, *tempID, *tempEmail); err != nil {
			fail(err.Error())
		}
	default:
		fail("unknown recovery phase")
	}
	first = ""
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
