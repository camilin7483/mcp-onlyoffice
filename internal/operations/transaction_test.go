package operations

import (
	"testing"
)

func TestTransactionPreparedToCommitted(t *testing.T) {
	tx := NewTransaction("t1", "doc1", "writer.replace_range", "before1")
	if tx.Status != TxPrepared || tx.StartedAt.IsZero() {
		t.Fatalf("new tx must be prepared with start time: %+v", tx)
	}
	rc, err := tx.Commit("after1")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if tx.Status != TxCommitted || rc.Status != TxCommitted {
		t.Fatalf("status not committed: %+v %+v", tx, rc)
	}
	if rc.TransactionID != "t1" || rc.BeforeDigest != "before1" || rc.AfterDigest != "after1" {
		t.Fatalf("bad receipt: %+v", rc)
	}
	if rc.RolledBack || rc.UnknownOutcome || tx.EndedAt.IsZero() {
		t.Fatalf("bad commit flags: %+v", rc)
	}
}

func TestTransactionPreparedToRolledBack(t *testing.T) {
	tx := NewTransaction("t2", "doc1", "op", "b")
	rc, err := tx.Rollback()
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if tx.Status != TxRolledBack || rc.Status != TxRolledBack || !rc.RolledBack || rc.UnknownOutcome {
		t.Fatalf("bad rollback receipt: %+v %+v", tx, rc)
	}
}

func TestTransactionCommittedReversal(t *testing.T) {
	tx := NewTransaction("t3", "doc1", "op", "b")
	if _, err := tx.Commit("a"); err != nil {
		t.Fatal(err)
	}
	rc, err := tx.Rollback() // explicit reversal of committed work
	if err != nil {
		t.Fatalf("reversal: %v", err)
	}
	if tx.Status != TxRolledBack || !rc.RolledBack {
		t.Fatalf("reversal must roll back with flag: %+v %+v", tx, rc)
	}
}

func TestTransactionIllegalTransitions(t *testing.T) {
	tx := NewTransaction("t4", "doc1", "op", "b")
	if _, err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Commit("a"); err == nil {
		t.Fatal("commit after rollback must fail")
	}
	if _, err := tx.Rollback(); err == nil {
		t.Fatal("double rollback must fail")
	}
	if _, err := tx.MarkUnknown("late"); err == nil {
		t.Fatal("mark-unknown after rollback must fail")
	}

	tx2 := NewTransaction("t5", "doc1", "op", "b")
	if _, err := tx2.Commit("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx2.Commit("a"); err == nil {
		t.Fatal("double commit must fail")
	}
	if _, err := tx2.MarkUnknown("late"); err == nil {
		t.Fatal("mark-unknown after commit must fail")
	}
}

func TestTransactionMarkUnknown(t *testing.T) {
	tx := NewTransaction("t6", "doc1", "op", "b")
	rc, err := tx.MarkUnknown("backend timeout")
	if err != nil {
		t.Fatalf("mark-unknown: %v", err)
	}
	if tx.Status != TxUnknownOutcome || rc.Status != TxUnknownOutcome {
		t.Fatalf("status not unknown_outcome: %+v %+v", tx, rc)
	}
	if !rc.UnknownOutcome || rc.RolledBack {
		t.Fatalf("bad unknown flags: %+v", rc)
	}
	if _, err := tx.Commit("a"); err == nil {
		t.Fatal("commit after mark-unknown must fail")
	}
	if _, err := tx.Rollback(); err == nil {
		t.Fatal("rollback after mark-unknown must fail")
	}
}
