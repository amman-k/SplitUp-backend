package db

import (
    "database/sql"
    "fmt"
    "log"
    "os"

    _ "github.com/lib/pq"
)

var DB *sql.DB

func Connect() {
    // 1. Get the single Connection String from the environment
    // This works for Supabase, Render, Railway, and local testing.
    connStr := os.Getenv("DB_URL")
    
    if connStr == "" {
        log.Fatal("DATABASE_URL environment variable is not set")
    }

    var err error
    // 2. Pass the connection string directly to sql.Open
    DB, err = sql.Open("postgres", connStr)
    if err != nil {
        log.Fatalf("Error opening database: %v", err)
    }

    // 3. Test the connection
    err = DB.Ping()
    if err != nil {
        log.Fatalf("Error connecting to database: %v", err)
    }

    fmt.Println("Successfully Connected to Database")
}