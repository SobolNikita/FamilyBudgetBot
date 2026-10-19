package main

import (
	"errors"
	"log"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	source := os.Getenv("MIGRATIONS_URL")
	if source == "" {
		source = "file://migrations"
	}
	migration, err := migrate.New(source, databaseURL)
	if err != nil {
		log.Fatalf("cannot initialize migrations: %s", migrationDiagnostic(err))
	}
	defer migration.Close()
	if err := migration.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatalf("migration failed: %s", migrationDiagnostic(err))
	}
	log.Print("migrations applied")
}
