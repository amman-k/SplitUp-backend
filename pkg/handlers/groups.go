package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"money-splitter/pkg/db"
	"money-splitter/pkg/middleware"
	"net/http"
)

type CreateGroupRequest struct {
	Name string `json:"name"`
}

func CreateGroup(w http.ResponseWriter, r *http.Request) {
	userId, ok := r.Context().Value(middleware.UserIDKey).(int)

	if !ok {
		http.Error(w, "User not Authenicated", http.StatusUnauthorized)
		return
	}
	var req CreateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	tx, err := db.DB.Begin()
	if err != nil {
		http.Error(w, "Server Error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var groupID int
	queryGroup := `INSERT INTO groups (name,created_by) VALUES ($1,$2) RETURNING id`
	err = tx.QueryRow(queryGroup, req.Name, userId).Scan(&groupID)
	if err != nil {
		fmt.Println(" GROUP INSERT ERROR:", err)
		http.Error(w, "Failed to create Group", http.StatusInternalServerError)
		return
	}

	queryMember := `INSERT INTO group_members (group_id,user_id) VALUES ($1,$2)`
	_, err = tx.Exec(queryMember, groupID, userId)
	if err != nil {
		http.Error(w, "Failed to add member", http.StatusInternalServerError)
		return
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "Failed to commit transaction", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "Group created successfully",
		"group_Id": groupID,
	})
	fmt.Printf("User %d created Group %d\n", userId, groupID)

}

type AddMemberRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

func AddMember(w http.ResponseWriter, r *http.Request) {
	groupId := r.PathValue("id")
	var req AddMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	var memberID int

	queryFind := `SELECT id from users WHERE email=$1`
	err := db.DB.QueryRow(queryFind, req.Email).Scan(&memberID)

	if err != nil {
		if req.Name == "" {
			http.Error(w, "User not found. provide a name ", http.StatusBadRequest)
			return
		}

		fmt.Println("User not found , Creating ghost user :", req.Name)

		queryCreateGhost := `INSERT INTO users (name, email, is_ghost) VALUES ($1, $2, TRUE) RETURNING id`

		err = db.DB.QueryRow(queryCreateGhost, req.Name, req.Email).Scan(&memberID)
		if err != nil {
			fmt.Println("Error creating ghost:", err)
			http.Error(w, "Failed to create ghost user", http.StatusInternalServerError)
			return
		}
	}

	var exists bool
	checkQuery := `SELECT EXISTS(SELECT 1 FROM group_members WHERE group_id=$1 AND user_id=$2)`
	_ = db.DB.QueryRow(checkQuery, groupId, memberID).Scan(&exists)
	if exists {
		http.Error(w, "User is already in the group", http.StatusConflict)
		return
	}

	_, err = db.DB.Exec("INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)", groupId, memberID)
	if err != nil {
		http.Error(w, "Failed to add member", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"message":  "Member added successfully",
		"user_id":  memberID,
		"is_ghost": true,
	})

}

func GetGroups(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.UserIDKey).(int)
	query := `
		SELECT g.id, g.name 
		FROM groups g
		JOIN group_members gm ON g.id = gm.group_id
		WHERE gm.user_id = $1
	`
	rows, err := db.DB.Query(query, userID)
	if err != nil {
		http.Error(w, "Failed to fetch groups", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var groups []map[string]interface{}
	for rows.Next() {
		var id int
		var name string
		rows.Scan(&id, &name)
		groups = append(groups, map[string]any{"id": id, "name": name})
	}

	if groups == nil {
		groups = []map[string]any{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(groups)
}

type MemberResponse struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	IsGhost bool   `json:"is_ghost"`
}

func GetGroupMembers(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	query := `SELECT u.id, u.name, u.email, u.is_ghost
		FROM users u
		JOIN group_members gm ON u.id = gm.user_id
		WHERE gm.group_id = $1
		`
	rows, err := db.DB.Query(query, groupID)
	if err != nil {
		http.Error(w, "Falied to fetch members", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var members []MemberResponse
	for rows.Next() {
		var m MemberResponse
		var email sql.NullString
		if err := rows.Scan(&m.ID, &m.Name, &email, &m.IsGhost); err != nil {
			continue
		}
		m.Email = email.String
		members = append(members, m)
	}
	if members == nil {
		members = []MemberResponse{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(members)
}


func DeleteGroup(w http.ResponseWriter, r *http.Request) {
    // 1. Get the ID from the URL (e.g., /groups/5)
    groupID := r.PathValue("id")

    // 2. Execute Delete Command
    // detailed cleanup isn't needed if tables were created with ON DELETE CASCADE
    query := `DELETE FROM groups WHERE id = $1`
    
    result, err := db.DB.Exec(query, groupID)
    if err != nil {
        fmt.Println("Error deleting group:", err)
        http.Error(w, "Failed to delete group", http.StatusInternalServerError)
        return
    }

    // 3. Check if a row was actually deleted
    rowsAffected, _ := result.RowsAffected()
    if rowsAffected == 0 {
        http.Error(w, "Group not found", http.StatusNotFound)
        return
    }

    // 4. Send Success Response
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{"message": "Group deleted successfully"})
}

func GroupName(w http.ResponseWriter, r *http.Request){
	groupId:=r.PathValue("id")
	query:=`SELECT name FROM groups WHERE id = $1`
	groupName:=""
	err:=db.DB.QueryRow(query,groupId).Scan(&groupName)
	if(err!=nil){
		http.Error(w,"Group not found",http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type","application/json")
	json.NewEncoder(w).Encode(groupName)




}