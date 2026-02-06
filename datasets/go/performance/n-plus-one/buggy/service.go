package service

import "database/sql"

type OrderService struct {
	db *sql.DB
}

type Order struct {
	ID         int
	CustomerID int
	Total      float64
}

type Customer struct {
	ID   int
	Name string
}

func (s *OrderService) GetOrdersWithCustomers() ([]map[string]interface{}, error) {
	rows, err := s.db.Query("SELECT id, customer_id, total FROM orders")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var order Order
		rows.Scan(&order.ID, &order.CustomerID, &order.Total)

		var customer Customer
		s.db.QueryRow("SELECT id, name FROM customers WHERE id = ?", order.CustomerID).Scan(&customer.ID, &customer.Name)

		results = append(results, map[string]interface{}{
			"order":    order,
			"customer": customer,
		})
	}
	return results, nil
}
