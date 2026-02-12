package main

import (
	"fmt"
	"log"
	"net/http"

	"os"

	"money-splitter/pkg/db"
	"money-splitter/pkg/handlers"
	"money-splitter/pkg/middleware"

	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found")
	}
	db.Connect()
	db.Migrate()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if err := db.DB.Ping(); err != nil {
			http.Error(w, "DataBase is down", http.StatusInternalServerError)
			return
		}
		w.Write([]byte("Go backend is running and database is connected"))

	})

	mux.HandleFunc("POST /register", handlers.RegisterUser)
	mux.HandleFunc("POST /login", handlers.LoginUser)
	mux.HandleFunc("POST /groups", middleware.AuthMiddleware(handlers.CreateGroup))
	mux.HandleFunc("POST /groups/{id}/members", middleware.AuthMiddleware(handlers.AddMember))
	mux.HandleFunc("POST /groups/{id}/expenses", middleware.AuthMiddleware(handlers.CreateExpense))
	mux.HandleFunc("GET /groups/{id}/balance", middleware.AuthMiddleware(handlers.GetGroupBalance))
	mux.HandleFunc("GET /groups", middleware.AuthMiddleware(handlers.GetGroups))
	mux.HandleFunc("GET /groups/{id}/expenses", middleware.AuthMiddleware(handlers.GetGroupExpenses))
	mux.HandleFunc("GET /groups/{id}/members", middleware.AuthMiddleware(handlers.GetGroupMembers))
	mux.HandleFunc("DELETE /expenses/{id}", middleware.AuthMiddleware(handlers.DeleteExpense))
	mux.HandleFunc("GET /expenses/{id}", middleware.AuthMiddleware(handlers.GetExpenseDetails))
	mux.HandleFunc("GET /me", middleware.AuthMiddleware(handlers.GetCurrentUser))
	mux.HandleFunc("PUT /expenses/{id}", middleware.AuthMiddleware(handlers.UpdateExpense))
	mux.HandleFunc("GET /groups/{id}/export", middleware.AuthMiddleware(handlers.ExportGroupPDF))
	mux.HandleFunc("DELETE /groups/{id}", handlers.DeleteGroup)
	mux.HandleFunc("GET /groups/{id}/name",middleware.AuthMiddleware(handlers.GroupName))

	port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }
	fmt.Printf("Server starting on port %v", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}

}
