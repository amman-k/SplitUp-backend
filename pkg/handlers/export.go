package handlers

import (
	"fmt"
	"money-splitter/pkg/db"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
)

// --- STRUCTS (Unchanged) ---
type Member struct {
	ID   int
	Name string
}

type ExpenseMatrixRow struct {
	Title, Date, Payer string
	TotalAmount        float64
	UserImpacts        map[int]float64
}

type SuggestedPayment struct {
	From, To string
	Amount   float64
}

// --- HANDLER ---

func ExportGroupPDF(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")

	// 1. FETCH GROUP NAME
	var groupName string
	err := db.DB.QueryRow("SELECT name FROM groups WHERE id=$1", groupID).Scan(&groupName)
	if err != nil {
		fmt.Println("Error fetching group:", err)
		http.Error(w, "Group not found", http.StatusNotFound)
		return
	}

	// 2. FETCH MEMBERS
	var members []Member
	rowsMem, err := db.DB.Query(`
        SELECT u.id, u.name 
        FROM group_members gm 
        JOIN users u ON gm.user_id = u.id 
        WHERE gm.group_id = $1 
        ORDER BY u.id ASC`, groupID)

	if err != nil {
		http.Error(w, "Error fetching members", http.StatusInternalServerError)
		return
	}
	defer rowsMem.Close()

	for rowsMem.Next() {
		var m Member
		rowsMem.Scan(&m.ID, &m.Name)
		members = append(members, m)
	}

	// 3. FETCH EXPENSES
	rowsExp, err := db.DB.Query(`
        SELECT id, title, amount, created_at 
        FROM expenses 
        WHERE group_id = $1 
        ORDER BY created_at DESC`, groupID)

	if err != nil {
		http.Error(w, "Error fetching expenses", http.StatusInternalServerError)
		return
	}
	defer rowsExp.Close()

	var matrixRows []ExpenseMatrixRow
	grandTotals := make(map[int]float64)

	type ExpTemp struct {
		ID        int
		Title     string
		Amount    float64
		CreatedAt string
	}
	var rawExpenses []ExpTemp
	for rowsExp.Next() {
		var e ExpTemp
		rowsExp.Scan(&e.ID, &e.Title, &e.Amount, &e.CreatedAt)
		rawExpenses = append(rawExpenses, e)
	}

	for _, raw := range rawExpenses {
		e := ExpenseMatrixRow{
			Title:       raw.Title,
			TotalAmount: raw.Amount,
			UserImpacts: make(map[int]float64),
		}
		if len(raw.CreatedAt) >= 10 {
			e.Date = raw.CreatedAt[:10]
		}

		// A. Who Paid?
		rowsPayers, err := db.DB.Query(`
            SELECT u.name, ep.user_id, ep.paid_amount 
            FROM expense_payers ep 
            JOIN users u ON ep.user_id = u.id 
            WHERE ep.expense_id = $1`, raw.ID)

		if err != nil {
			continue
		}

		var payerNames []string
		for rowsPayers.Next() {
			var pName string
			var uID int
			var amt float64
			rowsPayers.Scan(&pName, &uID, &amt)
			payerNames = append(payerNames, pName)
			e.UserImpacts[uID] += amt 
		}
		rowsPayers.Close()
		e.Payer = strings.Join(payerNames, ", ")

		// B. Who Owes?
		rowsSplits, err := db.DB.Query(`SELECT user_id, amount_owed FROM expense_splits WHERE expense_id = $1`, raw.ID)
		if err != nil {
			continue
		}

		for rowsSplits.Next() {
			var uID int
			var amt float64
			rowsSplits.Scan(&uID, &amt)
			e.UserImpacts[uID] -= amt 
		}
		rowsSplits.Close()

		// C. Update Grand Totals
		for uid, impact := range e.UserImpacts {
			grandTotals[uid] += impact
		}

		matrixRows = append(matrixRows, e)
	}

	// 4. CALCULATE SETTLEMENTS
	var creditors, debtors []struct {
		ID      int
		Name    string
		Balance float64
	}

	for _, m := range members {
		bal := grandTotals[m.ID]
		if bal > 0.01 {
			creditors = append(creditors, struct {
				ID      int
				Name    string
				Balance float64
			}{m.ID, m.Name, bal})
		} else if bal < -0.01 {
			debtors = append(debtors, struct {
				ID      int
				Name    string
				Balance float64
			}{m.ID, m.Name, bal})
		}
	}

	sort.Slice(creditors, func(i, j int) bool { return creditors[i].Balance > creditors[j].Balance })
	sort.Slice(debtors, func(i, j int) bool { return debtors[i].Balance < debtors[j].Balance })

	var settlements []SuggestedPayment
	cIdx, dIdx := 0, 0

	tempCred := make([]float64, len(creditors))
	for i, c := range creditors {
		tempCred[i] = c.Balance
	}
	tempDebt := make([]float64, len(debtors))
	for i, d := range debtors {
		tempDebt[i] = -d.Balance
	}

	for cIdx < len(creditors) && dIdx < len(debtors) {
		credit := tempCred[cIdx]
		debt := tempDebt[dIdx]

		if credit < 0.01 {
			cIdx++
			continue
		}
		if debt < 0.01 {
			dIdx++
			continue
		}

		amount := credit
		if debt < credit {
			amount = debt
		}

		if amount > 0.01 {
			settlements = append(settlements, SuggestedPayment{
				From:   debtors[dIdx].Name,
				To:     creditors[cIdx].Name,
				Amount: amount,
			})
		}

		tempCred[cIdx] -= amount
		tempDebt[dIdx] -= amount

		if tempCred[cIdx] < 0.01 {
			cIdx++
		}
		if tempDebt[dIdx] < 0.01 {
			dIdx++
		}
	}

	// ================= PDF DESIGN (DYNAMIC WIDTH) =================

	// --- 1. CALCULATE REQUIRED WIDTH ---
	// Fixed Columns
	dateColW := 25.0
	titleColW := 45.0
	payerColW := 30.0
	totalColW := 25.0
	fixedWidth := dateColW + titleColW + payerColW + totalColW
	
	// Member Columns (Ensure at least 25mm per person so names fit)
	minMemberW := 25.0 
	memberCount := float64(len(members))
	membersWidth := memberCount * minMemberW
	
	margins := 30.0 // 15 left + 15 right
	totalRequiredWidth := fixedWidth + membersWidth + margins

	// Standard A4 Landscape Width is ~297mm
	// If we need more, we use custom size. If we need less, we stick to A4 (but use the calc width for layout)
	pageWidth := 297.0 
	if totalRequiredWidth > pageWidth {
		pageWidth = totalRequiredWidth
	}

	// --- 2. INIT PDF WITH DYNAMIC SIZE ---
	pdf := gofpdf.NewCustom(&gofpdf.InitType{
		UnitStr: "mm",
		Size:    gofpdf.SizeType{Wd: pageWidth, Ht: 210}, // Keep height A4 (210), stretch Width
	})
	
	pdf.SetTitle("MoneySplitter Report", false)
	pdf.SetMargins(15, 15, 15)
	pdf.AddPage()

	// Recalculate column width to fill page exactly if it's a small group
	// If it's a large group, this will just equal minMemberW
	usableW := pageWidth - margins
	remainingW := usableW - fixedWidth
	memberColW := remainingW / memberCount 

	// --- COLORS ---
	primaryColor := []int{255, 107, 44}   // Orange
	lightOrange  := []int{255, 248, 242}  // Subtle Orange
	headerBg     := []int{255, 240, 230}  // Darker Orange Header
	netBalBg     := []int{255, 230, 215}  // Minimalist Net Balance
	textColor    := []int{40, 40, 40}
	lineColor    := []int{230, 230, 230}

	// --- 3. REPORT HEADER ---
	pdf.SetFillColor(primaryColor[0], primaryColor[1], primaryColor[2])
	pdf.Rect(0, 0, 8, 297, "F") 

	pdf.SetFont("Arial", "B", 22)
	pdf.SetTextColor(textColor[0], textColor[1], textColor[2])
	pdf.Cell(10, 10, "") 
	pdf.Cell(0, 10, "Group Expense Report")
	pdf.Ln(10)

	pdf.SetFont("Arial", "", 12)
	pdf.SetTextColor(100, 100, 100)
	pdf.Cell(10, 10, "")
	pdf.Cell(0, 8, fmt.Sprintf("%s  |  %s", groupName, time.Now().Format("Jan 02, 2006")))
	pdf.Ln(15)

	// --- 4. TABLE HEADER ---
	pdf.SetFillColor(headerBg[0], headerBg[1], headerBg[2])
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Arial", "B", 10)
	pdf.SetLineWidth(0.2)
	pdf.SetDrawColor(lineColor[0], lineColor[1], lineColor[2])

	headers := []struct { Text string; Width float64; Align string }{
		{"Date", dateColW, "C"},
		{"Description", titleColW, "L"},
		{"Paid By", payerColW, "L"},
		{"Total", totalColW, "R"},
	}

	for _, h := range headers {
		pdf.CellFormat(h.Width, 10, h.Text, "B", 0, h.Align, true, 0, "")
	}
	for _, m := range members {
		// Truncate if name is wider than column allows
		// Simple heuristic: 1 char ~= 2mm approx in size 10 font
		maxLen := int(memberColW / 2.5) 
		name := m.Name
		if len(name) > maxLen { name = name[:maxLen-2] + ".." }
		
		pdf.CellFormat(memberColW, 10, name, "B", 0, "C", true, 0, "")
	}
	pdf.Ln(10)

	// --- 5. MATRIX ROWS ---
	pdf.SetFont("Arial", "", 9)
	pdf.SetTextColor(60, 60, 60)

	fillRow := false 

	for _, row := range matrixRows {
		if fillRow {
			pdf.SetFillColor(lightOrange[0], lightOrange[1], lightOrange[2])
		} else {
			pdf.SetFillColor(255, 255, 255)
		}

		// Truncate text logic
		tLimit := int(titleColW / 2.5)
		displayTitle := row.Title
		if len(displayTitle) > tLimit { displayTitle = displayTitle[:tLimit-2] + ".." }

		pLimit := int(payerColW / 2.5)
		displayPayer := row.Payer
		if len(displayPayer) > pLimit { displayPayer = displayPayer[:pLimit-2] + ".." }

		pdf.CellFormat(dateColW, 9, row.Date, "B", 0, "C", true, 0, "")
		pdf.CellFormat(titleColW, 9, displayTitle, "B", 0, "L", true, 0, "")
		pdf.CellFormat(payerColW, 9, displayPayer, "B", 0, "L", true, 0, "")
		
		pdf.SetFont("Arial", "B", 9)
		pdf.CellFormat(totalColW, 9, fmt.Sprintf("%.2f", row.TotalAmount), "B", 0, "R", true, 0, "")
		pdf.SetFont("Arial", "", 9)

		for _, m := range members {
			impact := row.UserImpacts[m.ID]
			txt := "-"
			r, g, b := 200, 200, 200 

			if impact > 0.01 {
				txt = fmt.Sprintf("+%.2f", impact)
				r, g, b = 0, 150, 0 // Green
			} else if impact < -0.01 {
				txt = fmt.Sprintf("%.2f", impact)
				r, g, b = 200, 50, 50 // Red
			}

			pdf.SetTextColor(r, g, b)
			pdf.CellFormat(memberColW, 9, txt, "B", 0, "C", true, 0, "")
		}

		pdf.SetTextColor(60, 60, 60) 
		pdf.Ln(9)
		fillRow = !fillRow
	}

	// --- 6. NET BALANCES ROW ---
	pdf.Ln(1)
	pdf.SetFont("Arial", "B", 10)
	pdf.SetFillColor(netBalBg[0], netBalBg[1], netBalBg[2]) 
	pdf.SetTextColor(0, 0, 0)

	pdf.CellFormat(dateColW+titleColW+payerColW+totalColW, 10, "NET BALANCE  ", "0", 0, "R", true, 0, "")

	for _, m := range members {
		total := grandTotals[m.ID]
		txt := fmt.Sprintf("%.2f", total)
		
		if total > 0.01 {
			txt = "+" + txt
			pdf.SetTextColor(0, 150, 0)
		} else if total < -0.01 {
			pdf.SetTextColor(220, 50, 50)
		} else {
			pdf.SetTextColor(150, 150, 150)
		}
		
		pdf.CellFormat(memberColW, 10, txt, "0", 0, "C", true, 0, "")
	}
	pdf.Ln(18)

	// --- 7. SUGGESTED SETTLEMENTS ---
	if len(settlements) > 0 {
		pdf.SetTextColor(0, 0, 0)
		pdf.SetFont("Arial", "B", 14)
		pdf.Cell(0, 10, "Suggested Settlements")
		pdf.Ln(8)

		pdf.SetFont("Arial", "", 10)
		pdf.SetLineWidth(0.2)
		cardW := 85.0
		cardH := 12.0

		for _, s := range settlements {
			if pdf.GetY() > 190 { pdf.AddPage() } // Lower threshold because custom height is 210

			x := pdf.GetX()
			y := pdf.GetY()

			pdf.SetDrawColor(220, 220, 220)
			pdf.RoundedRect(x, y, cardW, cardH, 2, "D", "")

			pdf.SetXY(x+4, y)
			pdf.SetFont("Arial", "", 10)
			pdf.CellFormat(25, cardH, s.From, "", 0, "R", false, 0, "")

			pdf.SetTextColor(primaryColor[0], primaryColor[1], primaryColor[2])
			pdf.SetFont("Arial", "B", 10)
			pdf.CellFormat(10, cardH, ">", "", 0, "C", false, 0, "")
			pdf.SetTextColor(0, 0, 0)

			pdf.SetFont("Arial", "", 10)
			pdf.CellFormat(25, cardH, s.To, "", 0, "L", false, 0, "")

			pdf.SetFont("Arial", "B", 10)
			pdf.SetTextColor(0, 120, 0) 
			pdf.CellFormat(18, cardH, fmt.Sprintf("%.2f", s.Amount), "", 0, "R", false, 0, "")
			
			pdf.SetTextColor(0, 0, 0)
			
			if x + cardW*2 + 20 < pageWidth {
				pdf.SetXY(x + cardW + 10, y)
			} else {
				pdf.Ln(cardH + 4)
			}
		}
	}
	// Output
	safeName := strings.ReplaceAll(groupName, " ", "_")
    filename := fmt.Sprintf("%s_Report.pdf", safeName)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	pdf.Output(w)
}