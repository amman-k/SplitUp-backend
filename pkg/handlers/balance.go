package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"

	"money-splitter/pkg/db"
)

// Transaction represents a simplified payment instruction
type Transaction struct {
	FromUser int     `json:"from_user_id"`
	ToUser   int     `json:"to_user_id"`
	Amount   float64 `json:"amount"`
}

// balanceItem is a helper struct for sorting users by debt amount
type balanceItem struct {
	UserID int
	Amount float64
}

// GetGroupBalance handles the API request to fetch balances and settlements
func GetGroupBalance(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")

	// 1. Calculate Total Paid by each user
	rows, err := db.DB.Query(`
        SELECT ep.user_id, SUM(ep.paid_amount)
        FROM expense_payers ep
        JOIN expenses e ON ep.expense_id = e.id
        WHERE e.group_id = $1
        GROUP BY ep.user_id
    `, groupID)

	if err != nil {
		fmt.Println("Error calculating paid:", err)
		http.Error(w, "Database error (Paid)", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	paidMap := make(map[int]float64)
	for rows.Next() {
		var userID int
		var amount float64
		if err := rows.Scan(&userID, &amount); err != nil {
			continue
		}
		paidMap[userID] = amount
	}

	// 2. Calculate Total Owed by each user
	rows, err = db.DB.Query(`
        SELECT es.user_id, SUM(es.amount_owed)
        FROM expense_splits es
        JOIN expenses e ON es.expense_id = e.id
        WHERE e.group_id = $1
        GROUP BY es.user_id
    `, groupID)

	if err != nil {
		fmt.Println("Error calculating owed:", err)
		http.Error(w, "Database error (Owed)", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	owedMap := make(map[int]float64)
	for rows.Next() {
		var userID int
		var amount float64
		if err := rows.Scan(&userID, &amount); err != nil {
			continue
		}
		owedMap[userID] = amount
	}

	// 3. Calculate Net Balance (Paid - Owed)
	balances := make(map[int]float64)
	allUsers := make(map[int]bool)
	for uid := range paidMap {
		allUsers[uid] = true
	}
	for uid := range owedMap {
		allUsers[uid] = true
	}

	for uid := range allUsers {
		net := paidMap[uid] - owedMap[uid]
		// Round to 2 decimal places to prevent float errors
		balances[uid] = math.Round(net*100) / 100
	}

	// 4. Run the Simplification Algorithm
	transactions := minimizeDebts(balances)

	// 5. Send Response
	response := map[string]interface{}{
		"balances":     balances,
		"transactions": transactions,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// minimizeDebts reduces the number of transactions required to settle up
func minimizeDebts(balances map[int]float64) []Transaction {
	var debtors []balanceItem
	var creditors []balanceItem

	// Separate into those who owe money (-) and those owed money (+)
	for uid, amount := range balances {
		if amount < -0.01 {
			debtors = append(debtors, balanceItem{uid, amount})
		} else if amount > 0.01 {
			creditors = append(creditors, balanceItem{uid, amount})
		}
	}

	// Sort to prioritize largest debts/credits
	sort.Slice(debtors, func(i, j int) bool { return debtors[i].Amount < debtors[j].Amount })    // Ascending (most negative first)
	sort.Slice(creditors, func(i, j int) bool { return creditors[i].Amount > creditors[j].Amount }) // Descending (most positive first)

	var transactions []Transaction
	i, j := 0, 0

	// Greedy matching: Match biggest debtor with biggest creditor
	for i < len(debtors) && j < len(creditors) {
		debtor := &debtors[i]
		creditor := &creditors[j]

		// Find the minimum amount to settle
		amount := math.Min(math.Abs(debtor.Amount), creditor.Amount)
		amount = math.Round(amount*100) / 100 // Round to 2 decimals

		if amount > 0 {
			transactions = append(transactions, Transaction{
				FromUser: debtor.UserID,
				ToUser:   creditor.UserID,
				Amount:   amount,
			})
		}

		// Update remaining amounts
		debtor.Amount += amount
		creditor.Amount -= amount

		// Move indices if settled
		// Use epsilon for float comparison
		if math.Abs(debtor.Amount) < 0.01 {
			i++
		}
		if creditor.Amount < 0.01 {
			j++
		}
	}

	return transactions
}