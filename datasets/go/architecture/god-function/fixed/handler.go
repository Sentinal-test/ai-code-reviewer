package handler

import (
	"encoding/json"
	"net/http"
)

type Handler struct {
	orderService *OrderService
}

func (h *Handler) ProcessOrder(w http.ResponseWriter, r *http.Request) {
	var req OrderRequest
	json.NewDecoder(r.Body).Decode(&req)

	order, err := h.orderService.CreateOrder(req)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	json.NewEncoder(w).Encode(order)
}
