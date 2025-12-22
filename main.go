package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type server struct {
	ledger *Ledger
}

func main() {
	const dbPath = "data/ledger.db"

	db, err := InitDB(dbPath)
	if err != nil {
		log.Fatalf("инициализация БД: %v", err)
	}
	defer db.Close()

	s := &server{ledger: NewLedger(db)}

	http.HandleFunc("/categories", s.handleCategories)
	http.HandleFunc("/transactions", s.handleTransactions)
	http.HandleFunc("/summary", s.handleSummary)
	http.HandleFunc("/budgets", s.handleBudgets)
	http.HandleFunc("/alerts", s.handleAlerts)

	addr := ":8080"
	log.Printf("Сервер слушает %s (БД %s)", addr, dbPath)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("сервер упал: %v", err)
	}
}

// handleCategories поддерживает GET (список) и POST (создание).
func (s *server) handleCategories(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cats, err := s.ledger.ListCategories()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, cats)
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("ошибка разбора json: %w", err))
			return
		}
		cat, err := s.ledger.CreateCategory(req.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, cat)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleTransactions поддерживает POST (создание) и GET (выборка по периоду).
func (s *server) handleTransactions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			CategoryID int64  `json:"category_id"`
			AmountRub  string `json:"amount_rub"`
			OccurredAt string `json:"occurred_at"` // YYYY-MM-DD
			Note       string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("ошибка разбора json: %w", err))
			return
		}

		amount, err := parseRub(req.AmountRub)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		date, err := parseDate(req.OccurredAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		tx, err := s.ledger.AddTransaction(req.CategoryID, rublesToKopeks(amount), date, req.Note)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, tx)
	case http.MethodGet:
		from, err := parseDate(r.URL.Query().Get("from"))
		if err != nil && r.URL.Query().Get("from") != "" {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		to, err := parseDate(r.URL.Query().Get("to"))
		if err != nil && r.URL.Query().Get("to") != "" {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		rows, err := s.ledger.db.Query(
			"SELECT id, category_id, amount_kopeks, occurred_at, note FROM transactions WHERE occurred_at BETWEEN ? AND ? ORDER BY occurred_at",
			from.UTC().Format(time.RFC3339),
			timeOrNow(to).UTC().Format(time.RFC3339),
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("получение операций: %w", err))
			return
		}
		defer rows.Close()

		var out []Transaction
		for rows.Next() {
			var tx Transaction
			var ts string
			if err := rows.Scan(&tx.ID, &tx.CategoryID, &tx.AmountKopeks, &ts, &tx.Note); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			t, _ := time.Parse(time.RFC3339, ts)
			tx.OccurredAt = t
			out = append(out, tx)
		}
		if err := rows.Err(); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleSummary возвращает агрегаты по категориям за период.
func (s *server) handleSummary(w http.ResponseWriter, r *http.Request) {
	from, err := parseDate(r.URL.Query().Get("from"))
	if err != nil && r.URL.Query().Get("from") != "" {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	to, err := parseDate(r.URL.Query().Get("to"))
	if err != nil && r.URL.Query().Get("to") != "" {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	summary, err := s.ledger.Summary(from, timeOrNow(to))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	type summaryResp struct {
		CategoryID int64   `json:"category_id"`
		IncomeRub  float64 `json:"income_rub"`
		ExpenseRub float64 `json:"expense_rub"`
		NetRub     float64 `json:"net_rub"`
		Count      int     `json:"count"`
	}

	resp := make([]summaryResp, 0, len(summary))
	for _, s := range summary {
		resp = append(resp, summaryResp{
			CategoryID: s.CategoryID,
			IncomeRub:  kopeksToRubles(s.IncomeKopeks),
			ExpenseRub: kopeksToRubles(-s.ExpenseKopeks),
			NetRub:     kopeksToRubles(s.NetKopeks),
			Count:      s.Count,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleBudgets поддерживает GET (список) и POST (создание/обновление).
func (s *server) handleBudgets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		budgets, err := s.ledger.ListBudgets()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, budgets)
	case http.MethodPost:
		var req struct {
			CategoryID int64  `json:"category_id"`
			LimitRub   string `json:"limit_rub"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("ошибка разбора json: %w", err))
			return
		}
		limitRub, err := parseRub(req.LimitRub)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		budget, err := s.ledger.UpsertBudget(req.CategoryID, rublesToKopeks(limitRub))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, budget)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleAlerts возвращает превышения бюджетов за период.
func (s *server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	from, err := parseDate(r.URL.Query().Get("from"))
	if err != nil && r.URL.Query().Get("from") != "" {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	to, err := parseDate(r.URL.Query().Get("to"))
	if err != nil && r.URL.Query().Get("to") != "" {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	alerts, err := s.ledger.ExceededBudgets(from, timeOrNow(to))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	type alertResp struct {
		CategoryID   int64   `json:"category_id"`
		CategoryName string  `json:"category_name"`
		LimitRub     float64 `json:"limit_rub"`
		SpentRub     float64 `json:"spent_rub"`
		ExceededRub  float64 `json:"exceeded_rub"`
	}

	resp := make([]alertResp, 0, len(alerts))
	for _, a := range alerts {
		resp = append(resp, alertResp{
			CategoryID:   a.CategoryID,
			CategoryName: a.CategoryName,
			LimitRub:     kopeksToRubles(a.LimitKopeks),
			SpentRub:     kopeksToRubles(a.SpentKopeks),
			ExceededRub:  kopeksToRubles(a.ExceededByKopeks),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func parseRub(s string) (float64, error) {
	if s == "" {
		return 0, errors.New("сумма не указана")
	}
	s = strings.ReplaceAll(s, ",", ".")
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("некорректная сумма: %w", err)
	}
	return val, nil
}

// parseDate ожидает формат YYYY-MM-DD. Пустая строка возвращает нулевое время.
func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("дата должна быть в формате YYYY-MM-DD")
	}
	return t, nil
}

func timeOrNow(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t.UTC()
}
