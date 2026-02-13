package handlers

import (
	"encoding/json"
	"fmt"
	"money-splitter/pkg/db"
	"money-splitter/pkg/middleware"
	"money-splitter/pkg/models"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type RegisterRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}


func RegisterUser(w http.ResponseWriter, r *http.Request){
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err!=nil {
		http.Error(w,"Invalid request Payload",http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Email=="" || req.Password=="" {
		http.Error(w,"Name, Email and Password are required",http.StatusBadRequest)
		return
	}

	hashedPassowrd,err := bcrypt.GenerateFromPassword([]byte(req.Password),bcrypt.DefaultCost)

	if err != nil {
		http.Error(w,"Server error hashing password",http.StatusInternalServerError)
		return
	}

	var newUser models.User
	query := `INSERT INTO users (name,email,password_hash) VALUES ($1,$2,$3) RETURNING id,created_at`
	err= db.DB.QueryRow(query,req.Name,req.Email,string(hashedPassowrd)).Scan(&newUser.ID,&newUser.CreatedAt)
	if err != nil {
		fmt.Printf("DATABASE ERROR: %s",err)
		http.Error(w,"User cereation failed",http.StatusConflict)
		return
	}
	newUser.Name = req.Name
	newUser.Email = req.Email
	newUser.IsGhost = false

	w.Header().Set("Content-Type","application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newUser)

}

type LoginRequest struct {
	Email string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
	User models.User `json:"user"`
}

func LoginUser(w http.ResponseWriter, r *http.Request){
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w,"Invalid Request",http.StatusBadRequest)
		return
	}
	var user models.User

	query := `SELECT id,name,email,password_hash,is_ghost,created_at FROM users WHERE email=$1`
	err:= db.DB.QueryRow(query,req.Email).Scan(&user.ID,&user.Name,&user.Email,&user.PasswordHash,&user.IsGhost,&user.CreatedAt)

	if err != nil {
		http.Error(w,"Invalid Email or password",http.StatusUnauthorized)
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash),[]byte(req.Password))
	if err != nil {
		http.Error(w,"Invalid Email or Password",http.StatusUnauthorized)
		return
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256,jwt.MapClaims{
		"sub":user.ID,
		"exp":time.Now().Add(time.Hour*24*365*10).Unix(),
	})

	tokenString,err := token.SignedString([]byte(os.Getenv("JWT_SECRET")))
	if err !=nil {
		http.Error(w,"Failed to generate token",http.StatusInternalServerError)
		return
	} 

	w.Header().Set("Content-Type","application/json")
	json.NewEncoder(w).Encode(LoginResponse{
		Token: tokenString,
		User: user,
	})

}

func GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	// Get ID from context (set by Middleware)
	userID := r.Context().Value(middleware.UserIDKey).(int)

	var user models.User
	query := `SELECT id, name, email, created_at FROM users WHERE id = $1`
	
	err := db.DB.QueryRow(query, userID).Scan(&user.ID, &user.Name, &user.Email, &user.CreatedAt)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}