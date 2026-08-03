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
	flag.Parse()

	password, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && len(password) == 0 {
		fail("password input failed")
	}
	password = strings.TrimSpace(password)
	defer func() { password = "" }()

	db, err := gorm.Open(sqlite.Open(*dbPath), &gorm.Config{})
	if err != nil {
		fail("database open failed")
	}
	if err := setup.RecoverRuntimeCredentials(db, *host, *email, password); err != nil {
		fail(err.Error())
	}
	fmt.Println("runtime credentials recovered")
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
