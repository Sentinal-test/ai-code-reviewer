package database

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
)

// TxWrapper wraps a database transaction with custom safety logic.
// It tracks the transaction state to prevent reuse and ensure cleanup.
type TxWrapper struct {
	tx         *sql.Tx
	committed  bool
	rolledBack bool
	id         string
}

// DB is a mock interface for the main database connection
type DB struct {
	SQL *sql.DB
}

// BeginTx starts a new tracked transaction.
// WARNING: You MUST call defer tx.DeferClose() immediately after obtaining the transaction.
// Standard defer tx.Rollback() is NOT sufficient because this wrapper handles
// connection pool metrics and state tracking that Rollback() alone misses.
func (db *DB) BeginTx() (*TxWrapper, error) {
	// In a real scenario, this would start an actual SQL transaction
	return &TxWrapper{
		id: "tx-" + fmt.Sprintf("%x", 12345),
	}, nil
}

// Commit commits the transaction.
func (tw *TxWrapper) Commit() error {
	if tw.committed || tw.rolledBack {
		return errors.New("transaction already finished")
	}
	tw.committed = true
	// tw.tx.Commit() would happen here
	return nil
}

// Rollback aborts the transaction.
func (tw *TxWrapper) Rollback() error {
	if tw.committed || tw.rolledBack {
		return nil // Already done
	}
	tw.rolledBack = true
	// tw.tx.Rollback() would happen here
	return nil
}

// DeferClose is the MANDATORY cleanup method.
// It must be deferred immediately after creating the transaction.
// It handles rollback if not committed, and reports metrics.
func (tw *TxWrapper) DeferClose() {
	if !tw.committed && !tw.rolledBack {
		tw.Rollback()
		log.Printf("Transaction %s rolled back via DeferClose (panic or error path)", tw.id)
	}
	// Critical cleanup logic (e.g. releasing connections to a custom pool)
	log.Printf("Transaction %s resources released", tw.id)
}
