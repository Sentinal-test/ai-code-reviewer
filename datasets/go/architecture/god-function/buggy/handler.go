package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

type Handler struct {
	db *sql.DB
}

func (h *Handler) ProcessOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID    int    `json:"user_id"`
		ProductID int    `json:"product_id"`
		Quantity  int    `json:"quantity"`
		Coupon    string `json:"coupon"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	var userExists bool
	h.db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)", req.UserID).Scan(&userExists)
	if !userExists {
		http.Error(w, "User not found", 404)
		return
	}

	var product struct {
		Price float64
		Stock int
	}
	h.db.QueryRow("SELECT price, stock FROM products WHERE id = ?", req.ProductID).Scan(&product.Price, &product.Stock)
	if product.Stock < req.Quantity {
		http.Error(w, "Insufficient stock", 400)
		return
	}

	total := product.Price * float64(req.Quantity)

	if req.Coupon != "" {
		var discount float64
		h.db.QueryRow("SELECT discount FROM coupons WHERE code = ? AND expires_at > ?", req.Coupon, time.Now()).Scan(&discount)
		total = total - (total * discount / 100)
	}

	tx, _ := h.db.Begin()
	tx.Exec("UPDATE products SET stock = stock - ? WHERE id = ?", req.Quantity, req.ProductID)
	tx.Exec("INSERT INTO orders (user_id, product_id, quantity, total, created_at) VALUES (?, ?, ?, ?, ?)",
		req.UserID, req.ProductID, req.Quantity, total, time.Now())

	var orderID int64
	result, _ := tx.Exec("SELECT LAST_INSERT_ID()")
	orderID, _ = result.LastInsertId()

	tx.Exec("INSERT INTO order_history (order_id, status, timestamp) VALUES (?, 'pending', ?)", orderID, time.Now())
	tx.Commit()

	json.NewEncoder(w).Encode(map[string]interface{}{"order_id": orderID, "total": total})
}
