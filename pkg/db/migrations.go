package db

import (
	"fmt"
	"log"
)

func Migrate() {
	schema := `
    CREATE TABLE IF NOT EXISTS users (
        id SERIAL PRIMARY KEY,
        name VARCHAR(100) NOT NULL,
        email VARCHAR(255) UNIQUE, 
        password_hash VARCHAR(255), 
        is_ghost BOOLEAN DEFAULT FALSE,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );

    CREATE TABLE IF NOT EXISTS groups (
        id SERIAL PRIMARY KEY,
        name VARCHAR(100) NOT NULL,
        created_by INT REFERENCES users(id),
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );

    CREATE TABLE IF NOT EXISTS group_members (
        group_id INT REFERENCES groups(id) ON DELETE CASCADE,
        user_id INT REFERENCES users(id) ON DELETE CASCADE,
        joined_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (group_id, user_id)
    );

    -- UPDATED: Added title, category. Removed payer_id.
    CREATE TABLE IF NOT EXISTS expenses (
        id SERIAL PRIMARY KEY,
        group_id INT REFERENCES groups(id) ON DELETE CASCADE,
        title VARCHAR(100) NOT NULL,          -- New Column
        description VARCHAR(255),
        amount DECIMAL(10, 2) NOT NULL,
        category VARCHAR(50) DEFAULT 'General', -- New Column
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );

    -- NEW TABLE: Handles multiple people paying for one expense
    CREATE TABLE IF NOT EXISTS expense_payers (
        id SERIAL PRIMARY KEY,
        expense_id INT REFERENCES expenses(id) ON DELETE CASCADE,
        user_id INT REFERENCES users(id),
        paid_amount DECIMAL(10, 2) NOT NULL
    );

    CREATE TABLE IF NOT EXISTS expense_splits (
        id SERIAL PRIMARY KEY,
        expense_id INT REFERENCES expenses(id) ON DELETE CASCADE,
        user_id INT REFERENCES users(id),
        amount_owed DECIMAL(10, 2) NOT NULL
    );
    `

	_, err := DB.Exec(schema)
	if err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	fmt.Println("Database tables checked/created successfully!")
}