package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"money-splitter/pkg/db"
)

// --- STRUCTS ---

type Split struct {
	UserID int     `json:"user_id"`
	Amount float64 `json:"amount"`
}

type PayerSplit struct {
	UserID     int     `json:"user_id"`
	PaidAmount float64 `json:"paid_amount"`
}

type CreateExpenseRequest struct {
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Amount      float64      `json:"amount"`
	Category    string       `json:"category"`
	Payers      []PayerSplit `json:"payers"`
	Splits      []Split      `json:"splits"`
}

type ExpenseResponse struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
	PayerName   string  `json:"payer_name"`
	Date        string  `json:"date"`
	Category    string  `json:"category"`
}

type SplitDetail struct {
	UserID   int     `json:"user_id"`
	UserName string  `json:"user_name"`
	Amount   float64 `json:"amount"`
}

type PayerDetail struct {
	UserID     int     `json:"user_id"`
	UserName   string  `json:"user_name"`
	PaidAmount float64 `json:"paid_amount"`
}

type ExpenseDetailResponse struct {
	ID          int           `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Amount      float64       `json:"amount"`
	Category    string        `json:"category"`
	Date        string        `json:"date"`
	Payers      []PayerDetail `json:"payers"`
	PayerName   string        `json:"payer_name"`
	PayerID     int           `json:"payer_id"`
	Splits      []SplitDetail `json:"splits"`
}

// --- HANDLERS ---

func CreateExpense(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")

	var req CreateExpenseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// 1. Validate Total Split
	var totalSplit float64
	for _, s := range req.Splits {
		totalSplit += s.Amount
	}
	if math.Abs(totalSplit-req.Amount) > 0.01 {
		http.Error(w, "Split amounts do not match total amount", http.StatusBadRequest)
		return
	}

	// 2. Start Transaction
	tx, err := db.DB.Begin()
	if err != nil {
		http.Error(w, "Server Error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var expenseID int

	// 3. Insert Expense Record
	// Note: We insert created_at manually to ensure accuracy
	queryExpense := `
		INSERT INTO expenses (group_id, amount, title, description, category, created_at) 
		VALUES ($1, $2, $3, $4, $5, $6) 
		RETURNING id`

	err = tx.QueryRow(queryExpense, groupID, req.Amount, req.Title, req.Description, req.Category, time.Now()).Scan(&expenseID)
	if err != nil {
		fmt.Println("Error inserting Expense:", err)
		http.Error(w, "Failed to save Expense", http.StatusInternalServerError)
		return
	}

	// 4. Insert Payers
	queryPayer := `INSERT INTO expense_payers (expense_id, user_id, paid_amount) VALUES ($1, $2, $3)`
	stmtPayer, _ := tx.Prepare(queryPayer)
	defer stmtPayer.Close()

	for _, payer := range req.Payers {
		_, err := stmtPayer.Exec(expenseID, payer.UserID, payer.PaidAmount)
		if err != nil {
			fmt.Println("Error inserting payer:", err)
			http.Error(w, "Failed to save payers", http.StatusInternalServerError)
			return
		}
	}

	// 5. Insert Splits
	querySplits := `INSERT INTO expense_splits (expense_id, user_id, amount_owed) VALUES ($1, $2, $3)`
	stmtSplits, _ := tx.Prepare(querySplits)
	defer stmtSplits.Close()

	for _, split := range req.Splits {
		_, err := stmtSplits.Exec(expenseID, split.UserID, split.Amount)
		if err != nil {
			fmt.Println("Error inserting split:", err)
			http.Error(w, "Failed to save splits", http.StatusInternalServerError)
			return
		}
	}

	// 6. Commit
	if err = tx.Commit(); err != nil {
		http.Error(w, "Failed to Commit transaction", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"message":    "Expense added successfully",
		"expense_id": expenseID,
	})
}

func GetGroupExpenses(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")

	// 1. Fetch Expenses
	// We use a subquery to get the first payer's name, since we don't have payer_id in the expenses table anymore.
	query := `
		SELECT e.id, e.title, e.description, e.amount, e.category, e.created_at,
		       COALESCE((
		           SELECT u.name 
		           FROM expense_payers ep 
		           JOIN users u ON ep.user_id = u.id 
		           WHERE ep.expense_id = e.id 
		           LIMIT 1
		       ), 'Unknown') as payer_name
		FROM expenses e
		WHERE e.group_id = $1
		ORDER BY e.created_at DESC
	`
	rows, err := db.DB.Query(query, groupID)
	if err != nil {
		fmt.Println("Error fetching expenses:", err)
		http.Error(w, "Failed to fetch Expenses", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var expenses []ExpenseResponse
	for rows.Next() {
		var e ExpenseResponse
		var createdAtStr string
		
		// Scan matches the SELECT order
		err := rows.Scan(&e.ID, &e.Title, &e.Description, &e.Amount, &e.Category, &createdAtStr, &e.PayerName)
		if err != nil {
			continue
		}

		// Format Date safely
		if len(createdAtStr) > 10 {
			e.Date = createdAtStr[:10]
		} else {
			e.Date = createdAtStr
		}
		expenses = append(expenses, e)
	}

	if expenses == nil {
		expenses = []ExpenseResponse{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(expenses)
}

func GetExpenseDetails(w http.ResponseWriter, r *http.Request) {
	expenseID := r.PathValue("id")

	// 1. Get Basic Info
	queryInfo := `
		SELECT id, title, description, amount, category, created_at
		FROM expenses
		WHERE id = $1
	`
	var e ExpenseDetailResponse
	var createdAtStr string

	err := db.DB.QueryRow(queryInfo, expenseID).Scan(
		&e.ID, &e.Title, &e.Description, &e.Amount, &e.Category, &createdAtStr,
	)
	if err != nil {
		http.Error(w, "Expense not found", http.StatusNotFound)
		return
	}
	if len(createdAtStr) > 10 {
		e.Date = createdAtStr[:10]
	} else {
		e.Date = createdAtStr
	}

	// 2. Get Payers
	queryPayers := `
		SELECT p.user_id, u.name, p.paid_amount
		FROM expense_payers p
		JOIN users u ON p.user_id = u.id
		WHERE p.expense_id = $1
	`
	rowsPayers, err := db.DB.Query(queryPayers, expenseID)
	if err != nil {
		fmt.Println("Error fetching payers:", err)
		http.Error(w, "Failed to fetch payers", http.StatusInternalServerError)
		return
	}
	defer rowsPayers.Close()

	for rowsPayers.Next() {
		var p PayerDetail
		rowsPayers.Scan(&p.UserID, &p.UserName, &p.PaidAmount)
		e.Payers = append(e.Payers, p)
	}

	// Format Payer Display Name
	if len(e.Payers) > 0 {
		e.PayerID = e.Payers[0].UserID
		if len(e.Payers) == 1 {
			e.PayerName = e.Payers[0].UserName
		} else {
			e.PayerName = fmt.Sprintf("%s +%d others", e.Payers[0].UserName, len(e.Payers)-1)
		}
	} else {
		e.PayerName = "Unknown"
	}

	// 3. Get Splits (Who owes)
	querySplits := `
		SELECT s.user_id, u.name, s.amount_owed
		FROM expense_splits s
		JOIN users u ON s.user_id = u.id
		WHERE s.expense_id = $1
	`
	rows, err := db.DB.Query(querySplits, expenseID)
	if err != nil {
		http.Error(w, "Failed to fetch splits", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var s SplitDetail
		rows.Scan(&s.UserID, &s.UserName, &s.Amount)
		e.Splits = append(e.Splits, s)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(e)
}

func DeleteExpense(w http.ResponseWriter, r *http.Request) {
	expenseID := r.PathValue("id")
	query := `DELETE FROM expenses WHERE id = $1`

	result, err := db.DB.Exec(query, expenseID)
	if err != nil {
		http.Error(w, "Failed to delete expense", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "Expense not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Expense deleted"})
}

func UpdateExpense(w http.ResponseWriter, r *http.Request) {
	expenseID := r.PathValue("id")

	var req CreateExpenseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}

	// 1. Update Main Expense Table
	queryUpdate := `
		UPDATE expenses 
		SET description=$1, amount=$2, category=$3, title=$4
		WHERE id=$5
	`
	_, err = tx.Exec(queryUpdate, req.Description, req.Amount, req.Category, req.Title, expenseID)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Failed to update expense", http.StatusInternalServerError)
		return
	}

	// 2. Refresh Payers (Delete Old -> Insert New)
	_, err = tx.Exec(`DELETE FROM expense_payers WHERE expense_id=$1`, expenseID)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Failed to clear old payers", http.StatusInternalServerError)
		return
	}

	queryPayer := `INSERT INTO expense_payers (expense_id, user_id, paid_amount) VALUES ($1, $2, $3)`
	stmtPayer, _ := tx.Prepare(queryPayer)
	defer stmtPayer.Close()

	for _, payer := range req.Payers {
		_, err := stmtPayer.Exec(expenseID, payer.UserID, payer.PaidAmount)
		if err != nil {
			tx.Rollback()
			http.Error(w, "Failed to save new payers", http.StatusInternalServerError)
			return
		}
	}

	// 3. Refresh Splits (Delete Old -> Insert New)
	_, err = tx.Exec(`DELETE FROM expense_splits WHERE expense_id=$1`, expenseID)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Failed to clear old splits", http.StatusInternalServerError)
		return
	}

	querySplit := `INSERT INTO expense_splits (expense_id, user_id, amount_owed) VALUES ($1, $2, $3)`
	stmtSplit, _ := tx.Prepare(querySplit)
	defer stmtSplit.Close()

	for _, split := range req.Splits {
		_, err := stmtSplit.Exec(expenseID, split.UserID, split.Amount)
		if err != nil {
			tx.Rollback()
			http.Error(w, "Failed to save new splits", http.StatusInternalServerError)
			return
		}
	}

	tx.Commit()
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Expense updated"})
}