package main

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Category описывает пользовательскую категорию расходов/доходов.
type Category struct {
	ID   int64
	Name string
}

// Transaction хранит одну операцию: доход (плюс) или расход (минус) в копейках.
type Transaction struct {
	ID           int64
	CategoryID   int64
	AmountKopeks int64
	OccurredAt   time.Time
	Note         string
}

// CategorySummary агрегирует суммы и количество транзакций за период.
type CategorySummary struct {
	CategoryID    int64
	IncomeKopeks  int64
	ExpenseKopeks int64
	NetKopeks     int64
	Count         int
}

// Budget хранит лимит на категорию (в копейках).
type Budget struct {
	CategoryID   int64
	LimitKopeks  int64
	CategoryName string
}

// BudgetAlert сигнализирует о превышении лимита.
type BudgetAlert struct {
	CategoryID       int64
	CategoryName     string
	LimitKopeks      int64
	SpentKopeks      int64
	ExceededByKopeks int64
}

// Ledger работает поверх SQLite.
type Ledger struct {
	db *sql.DB
}

func NewLedger(db *sql.DB) *Ledger {
	return &Ledger{db: db}
}

func ensureDataDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

// InitDB создаёт файл БД (если нет) и накатывает минимальную схему.
func InitDB(path string) (*sql.DB, error) {
	if err := ensureDataDir(path); err != nil {
		return nil, fmt.Errorf("создание директории данных: %w", err)
	}

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=foreign_keys(1)", path))
	if err != nil {
		return nil, fmt.Errorf("открытие БД: %w", err)
	}

	schema := `
CREATE TABLE IF NOT EXISTS categories (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS budgets (
	category_id INTEGER PRIMARY KEY REFERENCES categories(id) ON DELETE CASCADE,
	limit_kopeks INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS transactions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	category_id INTEGER NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
	amount_kopeks INTEGER NOT NULL,
	occurred_at TEXT NOT NULL,
	note TEXT NOT NULL DEFAULT ''
);
`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("миграция схемы: %w", err)
	}

	return db, nil
}

func (l *Ledger) CreateCategory(name string) (Category, error) {
	if name == "" {
		return Category{}, errors.New("название категории пустое")
	}
	res, err := l.db.Exec("INSERT INTO categories (name) VALUES (?)", name)
	if err != nil {
		return Category{}, fmt.Errorf("сохранение категории: %w", err)
	}
	id, _ := res.LastInsertId()
	return Category{ID: id, Name: name}, nil
}

func (l *Ledger) ListCategories() ([]Category, error) {
	rows, err := l.db.Query("SELECT id, name FROM categories ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("чтение категорий: %w", err)
	}
	defer rows.Close()

	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (l *Ledger) AddTransaction(categoryID int64, amountKopeks int64, occurredAt time.Time, note string) (Transaction, error) {
	if categoryID == 0 {
		return Transaction{}, errors.New("categoryID не указан")
	}
	if occurredAt.IsZero() {
		return Transaction{}, errors.New("дата операции не указана")
	}
	occurredAt = occurredAt.UTC()

	txObj, err := l.db.Begin()
	if err != nil {
		return Transaction{}, fmt.Errorf("begin tx: %w", err)
	}

	// Убедимся, что категория существует.
	var exists int
	if err := txObj.QueryRow("SELECT 1 FROM categories WHERE id = ?", categoryID).Scan(&exists); err != nil {
		txObj.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return Transaction{}, fmt.Errorf("категория %d не найдена", categoryID)
		}
		return Transaction{}, fmt.Errorf("проверка категории: %w", err)
	}

	res, err := txObj.Exec(
		"INSERT INTO transactions (category_id, amount_kopeks, occurred_at, note) VALUES (?, ?, ?, ?)",
		categoryID,
		amountKopeks,
		occurredAt.Format(time.RFC3339),
		note,
	)
	if err != nil {
		txObj.Rollback()
		return Transaction{}, fmt.Errorf("сохранение транзакции: %w", err)
	}
	id, _ := res.LastInsertId()

	if err := txObj.Commit(); err != nil {
		return Transaction{}, fmt.Errorf("commit: %w", err)
	}

	return Transaction{
		ID:           id,
		CategoryID:   categoryID,
		AmountKopeks: amountKopeks,
		OccurredAt:   occurredAt,
		Note:         note,
	}, nil
}

func (l *Ledger) Summary(from, to time.Time) ([]CategorySummary, error) {
	if to.Before(from) {
		from, to = to, from
	}
	if from.IsZero() {
		from = time.Unix(0, 0)
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}

	rows, err := l.db.Query(`
SELECT
	category_id,
	SUM(CASE WHEN amount_kopeks >= 0 THEN amount_kopeks ELSE 0 END) AS income,
	SUM(CASE WHEN amount_kopeks < 0 THEN amount_kopeks ELSE 0 END) AS expense,
	SUM(amount_kopeks) AS net,
	COUNT(*) AS cnt
FROM transactions
WHERE occurred_at BETWEEN ? AND ?
GROUP BY category_id
`, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("сводка: %w", err)
	}
	defer rows.Close()

	var out []CategorySummary
	for rows.Next() {
		var s CategorySummary
		if err := rows.Scan(&s.CategoryID, &s.IncomeKopeks, &s.ExpenseKopeks, &s.NetKopeks, &s.Count); err != nil {
			return nil, fmt.Errorf("scan summary: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (l *Ledger) UpsertBudget(categoryID int64, limitKopeks int64) (Budget, error) {
	if categoryID == 0 {
		return Budget{}, errors.New("categoryID не указан")
	}
	if limitKopeks <= 0 {
		return Budget{}, errors.New("лимит должен быть больше 0")
	}

	_, err := l.db.Exec(`
INSERT INTO budgets (category_id, limit_kopeks)
VALUES (?, ?)
ON CONFLICT(category_id) DO UPDATE SET limit_kopeks=excluded.limit_kopeks
`, categoryID, limitKopeks)
	if err != nil {
		return Budget{}, fmt.Errorf("сохранение бюджета: %w", err)
	}

	var b Budget
	if err := l.db.QueryRow(`
SELECT b.category_id, b.limit_kopeks, c.name
FROM budgets b
JOIN categories c ON c.id = b.category_id
WHERE b.category_id = ?
`, categoryID).Scan(&b.CategoryID, &b.LimitKopeks, &b.CategoryName); err != nil {
		return Budget{}, fmt.Errorf("чтение бюджета: %w", err)
	}

	return b, nil
}

func (l *Ledger) ListBudgets() ([]Budget, error) {
	rows, err := l.db.Query(`
SELECT b.category_id, b.limit_kopeks, c.name
FROM budgets b
JOIN categories c ON c.id = b.category_id
ORDER BY c.name
`)
	if err != nil {
		return nil, fmt.Errorf("чтение бюджетов: %w", err)
	}
	defer rows.Close()

	var out []Budget
	for rows.Next() {
		var b Budget
		if err := rows.Scan(&b.CategoryID, &b.LimitKopeks, &b.CategoryName); err != nil {
			return nil, fmt.Errorf("scan budget: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ExceededBudgets возвращает превышения бюджетов за период.
func (l *Ledger) ExceededBudgets(from, to time.Time) ([]BudgetAlert, error) {
	summary, err := l.Summary(from, to)
	if err != nil {
		return nil, err
	}

	rows, err := l.db.Query(`
SELECT b.category_id, b.limit_kopeks, c.name
FROM budgets b
JOIN categories c ON c.id = b.category_id
`)
	if err != nil {
		return nil, fmt.Errorf("чтение бюджетов: %w", err)
	}
	defer rows.Close()

	byCat := make(map[int64]CategorySummary, len(summary))
	for _, s := range summary {
		byCat[s.CategoryID] = s
	}

	var alerts []BudgetAlert
	for rows.Next() {
		var categoryID, limit int64
		var name string
		if err := rows.Scan(&categoryID, &limit, &name); err != nil {
			return nil, fmt.Errorf("scan budget: %w", err)
		}
		spent := -byCat[categoryID].ExpenseKopeks
		if spent > limit {
			alerts = append(alerts, BudgetAlert{
				CategoryID:       categoryID,
				CategoryName:     name,
				LimitKopeks:      limit,
				SpentKopeks:      spent,
				ExceededByKopeks: spent - limit,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return alerts, nil
}

func rublesToKopeks(rub float64) int64 {
	// Используем округление, а не отсечение к нулю, чтобы не терять копейки на отрицательных суммах.
	return int64(math.Round(rub * 100))
}

func kopeksToRubles(kopeks int64) float64 {
	return float64(kopeks) / 100
}
