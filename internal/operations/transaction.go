package operations

import (
	"fmt"
	"time"
)

// TxStatus is the lifecycle state of a transaction.
type TxStatus string

const (
	TxPrepared       TxStatus = "prepared"
	TxCommitted      TxStatus = "committed"
	TxRolledBack     TxStatus = "rolled_back"
	TxUnknownOutcome TxStatus = "unknown_outcome"
	TxFailed         TxStatus = "failed"
)

// Transaction tracks one mutation attempt from prepare to settlement.
type Transaction struct {
	ID                        string
	DocumentID                string
	Operation                 string
	Status                    TxStatus
	BeforeDigest, AfterDigest string
	StartedAt, EndedAt        time.Time
}

// Receipt is the settlement record handed to the caller.
type Receipt struct {
	TransactionID  string   `json:"transaction_id"`
	Status         TxStatus `json:"status"`
	BeforeDigest   string   `json:"before_digest,omitempty"`
	AfterDigest    string   `json:"after_digest,omitempty"`
	RolledBack     bool     `json:"rolled_back"`
	UnknownOutcome bool     `json:"unknown_outcome"`
}

// NewTransaction opens a transaction in prepared state.
func NewTransaction(id, documentID, operation, before string) *Transaction {
	return &Transaction{
		ID: id, DocumentID: documentID, Operation: operation,
		Status: TxPrepared, BeforeDigest: before, StartedAt: time.Now(),
	}
}

// Commit settles a prepared transaction; any other state is an error.
func (t *Transaction) Commit(after string) (Receipt, error) {
	if t.Status != TxPrepared {
		return Receipt{}, fmt.Errorf("operations: commit requires a prepared transaction (status %s)", t.Status)
	}
	t.Status = TxCommitted
	t.AfterDigest = after
	t.EndedAt = time.Now()
	return Receipt{
		TransactionID: t.ID, Status: TxCommitted,
		BeforeDigest: t.BeforeDigest, AfterDigest: after,
	}, nil
}

// Rollback aborts a prepared transaction or reverses a committed one;
// both paths return a receipt with RolledBack set.
func (t *Transaction) Rollback() (Receipt, error) {
	switch t.Status {
	case TxPrepared, TxCommitted:
	default:
		return Receipt{}, fmt.Errorf("operations: rollback requires a prepared or committed transaction (status %s)", t.Status)
	}
	t.Status = TxRolledBack
	t.EndedAt = time.Now()
	return Receipt{
		TransactionID: t.ID, Status: TxRolledBack,
		BeforeDigest: t.BeforeDigest, AfterDigest: t.AfterDigest,
		RolledBack: true,
	}, nil
}

// MarkUnknown settles a prepared transaction whose outcome is unknowable
// (e.g. backend timeout); allowed from prepared only.
func (t *Transaction) MarkUnknown(_ string) (Receipt, error) {
	if t.Status != TxPrepared {
		return Receipt{}, fmt.Errorf("operations: mark-unknown requires a prepared transaction (status %s)", t.Status)
	}
	t.Status = TxUnknownOutcome
	t.EndedAt = time.Now()
	return Receipt{
		TransactionID: t.ID, Status: TxUnknownOutcome,
		BeforeDigest: t.BeforeDigest, AfterDigest: t.AfterDigest,
		UnknownOutcome: true,
	}, nil
}
