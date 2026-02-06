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
	rows, err := s.db.Query(`
		SELECT o.id, o.customer_id, o.total, c.id, c.name 
		FROM orders o 
		JOIN customers c ON o.customer_id = c.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var order Order
		var customer Customer
		rows.Scan(&order.ID, &order.CustomerID, &order.Total, &customer.ID, &customer.Name)

		results = append(results, map[string]interface{}{
			"order":    order,
			"customer": customer,
		})
	}
	return results, nil
}
