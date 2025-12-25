package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
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

	// Раздача статических файлов (web фронтенд).
	fs := http.FileServer(http.Dir("web"))
	http.Handle("/", fs)

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	http.HandleFunc("/categories", s.handleCategories)
	http.HandleFunc("/transactions", s.handleTransactions)
	http.HandleFunc("/summary", s.handleSummary)
	http.HandleFunc("/budgets", s.handleBudgets)
	http.HandleFunc("/alerts", s.handleAlerts)
	http.HandleFunc("/categories/", s.handleCategoryByID)
	http.HandleFunc("/transactions/", s.handleTransactionByID)

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
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, errors.New("название категории пустое"))
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

// handleCategoryByID поддерживает DELETE /categories/{id}.
func (s *server) handleCategoryByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, err := parseIDFromPath(r.URL.Path, "/categories/")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.ledger.DeleteCategory(id); err != nil {
		if errors.Is(err, errNotFound) {
			writeError(w, http.StatusNotFound, err)
		} else {
			writeError(w, http.StatusBadRequest, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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
			if errors.Is(err, errNotFound) {
				writeError(w, http.StatusNotFound, err)
			} else {
				writeError(w, http.StatusBadRequest, err)
			}
			return
		}
		writeJSON(w, http.StatusCreated, tx)
	case http.MethodGet:
		params, err := parseTxQuery(r.URL.Query())
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		rows, err := s.ledger.db.Query(
			`SELECT id, category_id, amount_kopeks, occurred_at, note
			 FROM transactions
			 WHERE occurred_at BETWEEN ? AND ?
			 AND (? = 0 OR category_id = ?)
			 ORDER BY occurred_at DESC
			 LIMIT ? OFFSET ?`,
			params.from.UTC().Format(time.RFC3339),
			params.to.UTC().Format(time.RFC3339),
			params.categoryID,
			params.categoryID,
			params.limit,
			params.offset,
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

// handleTransactionByID поддерживает PUT для обновления одной транзакции по пути /transactions/{id}.
func (s *server) handleTransactionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, err := parseIDFromPath(r.URL.Path, "/transactions/")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		CategoryID int64  `json:"category_id"`
		AmountRub  string `json:"amount_rub"`
		OccurredAt string `json:"occurred_at"`
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

	tx, err := s.ledger.UpdateTransaction(id, req.CategoryID, rublesToKopeks(amount), date, req.Note)
	if err != nil {
		if errors.Is(err, errNotFound) {
			writeError(w, http.StatusNotFound, err)
		} else {
			writeError(w, http.StatusBadRequest, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, tx)
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
		if req.CategoryID == 0 {
			writeError(w, http.StatusBadRequest, errors.New("category_id обязателен"))
			return
		}
		limitRub, err := parseRub(req.LimitRub)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		budget, err := s.ledger.UpsertBudget(req.CategoryID, rublesToKopeks(limitRub))
		if err != nil {
			if errors.Is(err, errNotFound) {
				writeError(w, http.StatusNotFound, err)
			} else {
				writeError(w, http.StatusBadRequest, err)
			}
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

func parseIDFromPath(path, prefix string) (int64, error) {
	if !strings.HasPrefix(path, prefix) {
		return 0, fmt.Errorf("некорректный путь")
	}
	idStr := strings.TrimPrefix(path, prefix)
	idStr = strings.Trim(idStr, "/")
	if idStr == "" {
		return 0, fmt.Errorf("id не указан в пути")
	}
	val, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("id должен быть числом")
	}
	return val, nil
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

type txQuery struct {
	from       time.Time
	to         time.Time
	categoryID int64
	limit      int
	offset     int
}

func parseTxQuery(values url.Values) (txQuery, error) {
	var q txQuery

	from, err := parseDate(values.Get("from"))
	if err != nil && values.Get("from") != "" {
		return q, err
	}
	to, err := parseDate(values.Get("to"))
	if err != nil && values.Get("to") != "" {
		return q, err
	}
	if from.IsZero() {
		q.from = time.Unix(0, 0).UTC()
	} else {
		q.from = from.UTC()
	}
	if to.IsZero() {
		q.to = time.Now().UTC()
	} else {
		q.to = to.UTC()
	}

	if cid := values.Get("category_id"); cid != "" {
		parsed, err := strconv.ParseInt(cid, 10, 64)
		if err != nil {
			return q, fmt.Errorf("category_id должен быть числом")
		}
		q.categoryID = parsed
	}

	q.limit = 100
	q.offset = 0
	if l := values.Get("limit"); l != "" {
		parsed, err := strconv.Atoi(l)
		if err != nil || parsed <= 0 {
			return q, fmt.Errorf("limit должен быть положительным числом")
		}
		q.limit = parsed
	}
	if o := values.Get("offset"); o != "" {
		parsed, err := strconv.Atoi(o)
		if err != nil || parsed < 0 {
			return q, fmt.Errorf("offset должен быть неотрицательным числом")
		}
		q.offset = parsed
	}

	return q, nil
}
