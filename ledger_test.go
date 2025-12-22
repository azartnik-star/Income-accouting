package main

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestLedger(t *testing.T) *Ledger {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := InitDB(path)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewLedger(db)
}

func TestSummaryAndBudgets(t *testing.T) {
	ledger := newTestLedger(t)

	food, err := ledger.CreateCategory("Food")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	transport, err := ledger.CreateCategory("Transport")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	// Бюджет на еду — 35.00
	if _, err := ledger.UpsertBudget(food.ID, 3_500); err != nil {
		t.Fatalf("set budget: %v", err)
	}

	start := time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, time.March, 31, 23, 59, 0, 0, time.UTC)

	mustAdd := func(categoryID int64, amount int64, day int, note string) {
		_, err := ledger.AddTransaction(categoryID, amount, time.Date(2024, time.March, day, 12, 0, 0, 0, time.UTC), note)
		if err != nil {
			t.Fatalf("add transaction: %v", err)
		}
	}

	mustAdd(food.ID, -2_300, 10, "Groceries")
	mustAdd(food.ID, -1_500, 15, "Dinner out")
	mustAdd(food.ID, 2_000, 21, "Refund")
	mustAdd(transport.ID, -900, 11, "Taxi")

	// Вне периода: не должно попасть в мартовскую сводку.
	_, _ = ledger.AddTransaction(food.ID, -1_000, time.Date(2024, time.February, 28, 8, 0, 0, 0, time.UTC), "February groceries")

	summary, err := ledger.Summary(start, end)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}

	foodSummary := findSummary(summary, food.ID)
	if foodSummary.ExpenseKopeks != -3_800 {
		t.Fatalf("food expense expected -3800, got %d", foodSummary.ExpenseKopeks)
	}
	if foodSummary.IncomeKopeks != 2_000 {
		t.Fatalf("food income expected 2000, got %d", foodSummary.IncomeKopeks)
	}
	if foodSummary.NetKopeks != -1_800 {
		t.Fatalf("food net expected -1800, got %d", foodSummary.NetKopeks)
	}
	if foodSummary.Count != 3 {
		t.Fatalf("food count expected 3, got %d", foodSummary.Count)
	}

	transportSummary := findSummary(summary, transport.ID)
	if transportSummary.ExpenseKopeks != -900 || transportSummary.Count != 1 {
		t.Fatalf("transport summary unexpected: %+v", transportSummary)
	}

	alerts, err := ledger.ExceededBudgets(start, end)
	if err != nil {
		t.Fatalf("alerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	alert := alerts[0]
	if alert.CategoryID != food.ID {
		t.Fatalf("expected alert for %d, got %d", food.ID, alert.CategoryID)
	}
	if alert.ExceededByKopeks != 300 {
		t.Fatalf("expected exceed by 300, got %d", alert.ExceededByKopeks)
	}
}

func findSummary(in []CategorySummary, categoryID int64) CategorySummary {
	for _, s := range in {
		if s.CategoryID == categoryID {
			return s
		}
	}
	return CategorySummary{}
}

func TestAddTransactionRequiresCategory(t *testing.T) {
	ledger := newTestLedger(t)
	_, err := ledger.AddTransaction(999, -100, time.Now(), "Should fail")
	if err == nil {
		t.Fatal("expected error for missing category")
	}
}

func TestRublesToKopeksRounding(t *testing.T) {
	tests := []struct {
		rub   float64
		want  int64
		label string
	}{
		{rub: 12.34, want: 1234, label: "positive"},
		{rub: -32.00, want: -3200, label: "negative integer"},
		{rub: -0.01, want: -1, label: "negative kopek"},
	}

	for _, tc := range tests {
		if got := rublesToKopeks(tc.rub); got != tc.want {
			t.Fatalf("%s: expected %d, got %d", tc.label, tc.want, got)
		}
	}
}
