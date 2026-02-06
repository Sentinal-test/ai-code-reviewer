package order

import (
	"fmt"
	"time"

	"github.com/example/repo/pkg/database"
)

type OrderProcessor struct {
	db *database.DB
}

func NewOrderProcessor(db *database.DB) *OrderProcessor {
	return &OrderProcessor{db: db}
}

// ProcessOrder handles the lifecycle of a new order.
func (p *OrderProcessor) ProcessOrder(orderID string, items []string) error {
	// Start transaction
	tx, err := p.db.BeginTx()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}

	defer tx.DeferClose()

	// Process items
	for _, item := range items {
		if err := p.updateInventory(tx, item); err != nil {
			// Rollback handled by DeferClose, no manual call needed (though explicit is okay too)
			return err
		}
	}

	// Calculate totals
	if err := p.calculateTotal(tx, orderID); err != nil {
		return err
	}

	// Finalize
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}

	return nil
}

func (p *OrderProcessor) updateInventory(tx *database.TxWrapper, itemID string) error {
	// Simulate DB update
	if itemID == "out-of-stock" {
		return fmt.Errorf("item out of stock")
	}
	return nil
}

func (p *OrderProcessor) calculateTotal(tx *database.TxWrapper, orderID string) error {
	// Simulate calculation
	time.Sleep(10 * time.Millisecond)
	return nil
}

// Dummy methods to increase file size...
func (p *OrderProcessor) Helper1() { time.Sleep(time.Nanosecond) }
func (p *OrderProcessor) Helper2() { time.Sleep(time.Nanosecond) }
func (p *OrderProcessor) Helper3() { time.Sleep(time.Nanosecond) }
func (p *OrderProcessor) Helper4() { time.Sleep(time.Nanosecond) }
func (p *OrderProcessor) Helper5() { time.Sleep(time.Nanosecond) }
