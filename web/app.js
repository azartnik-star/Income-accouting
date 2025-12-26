const api = {
  async getCategories() {
    const res = await fetch("/categories");
    if (!res.ok) throw new Error("Не удалось получить категории");
    return res.json();
  },
  async getBudgets() {
    const res = await fetch("/budgets");
    if (!res.ok) throw new Error("Не удалось получить бюджеты");
    return res.json();
  },
  async createCategory(name) {
    const res = await fetch("/categories", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
    if (!res.ok) throw new Error((await res.json()).error || "Ошибка создания категории");
    return res.json();
  },
  async deleteCategory(id) {
    const res = await fetch(`/categories/${id}`, { method: "DELETE" });
    if (!res.ok) throw new Error((await res.json()).error || "Ошибка удаления категории");
  },
  async upsertBudget(data) {
    const res = await fetch("/budgets", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error((await res.json()).error || "Ошибка сохранения бюджета");
    return res.json();
  },
  async createTransaction(data) {
    const res = await fetch("/transactions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error((await res.json()).error || "Ошибка добавления операции");
    return res.json();
  },
  async getTransactions(params) {
    const q = new URLSearchParams(params);
    const res = await fetch(`/transactions?${q.toString()}`);
    if (!res.ok) throw new Error("Не удалось получить операции");
    return res.json();
  },
  async updateTransaction(id, data) {
    const res = await fetch(`/transactions/${id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error((await res.json()).error || "Ошибка обновления операции");
    return res.json();
  },
  async getSummary(params) {
    const q = new URLSearchParams(params);
    const res = await fetch(`/summary?${q.toString()}`);
    if (!res.ok) throw new Error("Не удалось получить сводку");
    return res.json();
  },
  async getAlerts(params) {
    const q = new URLSearchParams(params);
    const res = await fetch(`/alerts?${q.toString()}`);
    if (!res.ok) throw new Error("Не удалось получить алерты");
    return res.json();
  },
};

const state = {
  categories: [],
  transactions: [],
  summary: [],
  alerts: [],
  budgets: [],
  editingTxId: null,
  page: 0,
  pageSize: 20,
};

const els = {
  categoryForm: document.getElementById("category-form"),
  categoryName: document.getElementById("category-name"),
  categoryList: document.getElementById("category-list"),
  budgetForm: document.getElementById("budget-form"),
  budgetCategory: document.getElementById("budget-category"),
  budgetLimit: document.getElementById("budget-limit"),
  budgetList: document.getElementById("budget-list"),
  txForm: document.getElementById("tx-form"),
  txCategory: document.getElementById("tx-category"),
  txAmount: document.getElementById("tx-amount"),
  txDate: document.getElementById("tx-date"),
  txNote: document.getElementById("tx-note"),
  filterFrom: document.getElementById("filter-from"),
  filterTo: document.getElementById("filter-to"),
  filterCategory: document.getElementById("filter-category"),
  filterApply: document.getElementById("filter-apply"),
  txTableBody: document.querySelector("#tx-table tbody"),
  summaryList: document.getElementById("summary-list"),
  alertsList: document.getElementById("alerts-list"),
  toast: document.getElementById("toast"),
};

function showToast(msg, isError = false) {
  els.toast.textContent = msg;
  els.toast.classList.toggle("error", isError);
  els.toast.classList.remove("hidden");
  setTimeout(() => els.toast.classList.add("hidden"), 2500);
}

function formatRub(kopeks) {
  return (kopeks / 100).toFixed(2) + " ₽";
}

function renderCategories() {
  els.categoryList.innerHTML = "";
  els.txCategory.innerHTML = "";
  if (els.budgetCategory) els.budgetCategory.innerHTML = "";
  if (els.filterCategory) els.filterCategory.innerHTML = `<option value="">Все</option>`;
  state.categories.forEach((c) => {
    const id = c.ID ?? c.id;
    const name = c.Name ?? c.name ?? id;
    const li = document.createElement("li");
    li.className = "category-item";
    li.innerHTML = `<span>${name}</span><button type="button" class="ghost" data-id="${id}">×</button>`;
    els.categoryList.appendChild(li);

    const opt = document.createElement("option");
    opt.value = id;
    opt.textContent = name;
    els.txCategory.appendChild(opt);

    if (els.budgetCategory) {
      const opt2 = opt.cloneNode(true);
      els.budgetCategory.appendChild(opt2);
    }

    if (els.filterCategory) {
      const opt3 = opt.cloneNode(true);
      els.filterCategory.appendChild(opt3);
    }
  });
}

function renderTransactions() {
  els.txTableBody.innerHTML = "";
  state.transactions.forEach((tx) => {
    const tr = document.createElement("tr");
    const date = new Date(tx.occurred_at || tx.OccurredAt || tx.occurredAt);
    const cat = state.categories.find((c) => Number(c.id ?? c.ID) === Number(tx.category_id ?? tx.CategoryID)) || {};
    const amount = tx.amount_kopeks ?? tx.AmountKopeks ?? tx.amountKopeks;
    const note = tx.note || tx.Note || "";
    tr.dataset.id = tx.id ?? tx.ID;
    tr.dataset.categoryId = tx.category_id ?? tx.CategoryID;
    tr.dataset.amount = amount;
    tr.dataset.date = date.toISOString().slice(0, 10);
    tr.dataset.note = note;
    tr.innerHTML = `
      <td>${tr.dataset.date}</td>
      <td>${cat.name || cat.Name || tx.category_id}</td>
      <td class="amount ${amount >= 0 ? "positive" : "negative"}">${formatRub(amount)}</td>
      <td class="note">${note}</td>
    `;
    els.txTableBody.appendChild(tr);
  });
}

function renderSummary() {
  els.summaryList.innerHTML = "";
  state.summary.forEach((s) => {
    const cat = s.category_id ?? s.CategoryID;
    const income = s.income_rub ?? s.IncomeRub ?? s.income ?? 0;
    const expense = s.expense_rub ?? s.ExpenseRub ?? s.expense ?? 0;
    const net = s.net_rub ?? s.NetRub ?? s.net ?? 0;
    const count = s.count ?? s.Count ?? 0;

    const div = document.createElement("div");
    div.className = "summary-item";
    div.innerHTML = `
      <div class="title">Категория: ${cat}</div>
      <div class="numbers">Итог: ${net.toFixed(2)} ₽</div>
      <div class="muted">Доход: ${income.toFixed(2)} ₽ · Расход: ${expense.toFixed(2)} ₽ · Операций: ${count}</div>
    `;
    els.summaryList.appendChild(div);
  });
}

function renderAlerts() {
  els.alertsList.innerHTML = "";
  if (!state.alerts.length) {
    const div = document.createElement("div");
    div.className = "muted";
    div.textContent = "Превышений нет";
    els.alertsList.appendChild(div);
    return;
  }
  state.alerts.forEach((a) => {
    const div = document.createElement("div");
    div.className = "alert";
    div.innerHTML = `
      <strong>${a.category_name || a.CategoryName || a.category_id}</strong>
      <div class="muted">Лимит: ${(a.limit_rub ?? a.LimitRub ?? 0).toFixed(2)} ₽ · Потрачено: ${(a.spent_rub ?? a.SpentRub ?? 0).toFixed(2)} ₽ · Превышение: ${(a.exceeded_rub ?? a.ExceededRub ?? 0).toFixed(2)} ₽</div>
    `;
    els.alertsList.appendChild(div);
  });
}

function renderBudgets() {
  if (!els.budgetList) return;
  els.budgetList.innerHTML = "";
  if (!state.budgets.length) {
    const div = document.createElement("div");
    div.className = "muted";
    div.textContent = "Лимиты не заданы";
    els.budgetList.appendChild(div);
    return;
  }
  state.budgets.forEach((b) => {
    const div = document.createElement("div");
    div.className = "budget-item";
    const name = b.category_name ?? b.CategoryName ?? b.category_id;
    const limit = b.limit_rub ?? (b.LimitKopeks ? b.LimitKopeks / 100 : 0);
    div.innerHTML = `<div>${name}</div><div class="muted">Лимит: ${Number(limit).toFixed(2)} ₽</div>`;
    els.budgetList.appendChild(div);
  });
}

async function refreshAll() {
  try {
    const cats = await api.getCategories();
    state.categories = cats;
    renderCategories();

    try {
      state.budgets = await api.getBudgets();
    } catch (e) {
      state.budgets = [];
      console.error("budgets load failed", e);
      showToast("Не удалось загрузить бюджеты", true);
    }

    const filters = buildFilters();
    const [txs, summary, alerts] = await Promise.all([
      api.getTransactions(filters),
      api.getSummary(filters),
      api.getAlerts(filters),
    ]);
    state.transactions = txs;
    state.summary = summary;
    state.alerts = alerts;
    renderBudgets();
    renderTransactions();
    renderSummary();
    renderAlerts();
  } catch (e) {
    showToast(e.message || "Ошибка загрузки", true);
    console.error(e);
  }
}

function buildFilters() {
  const params = {};
  if (els.filterFrom.value) params.from = els.filterFrom.value;
  if (els.filterTo.value) params.to = els.filterTo.value;
  if (els.filterCategory.value) params.category_id = els.filterCategory.value;
  params.limit = state.pageSize;
  params.offset = state.page * state.pageSize;
  // если даты не указаны, ставим from = 1970-01-01 для предсказуемости
  if (!params.from) params.from = "1970-01-01";
  if (!params.to) params.to = new Date().toISOString().slice(0, 10);
  return params;
}

function wireEvents() {
  els.categoryForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    const name = els.categoryName.value.trim();
    if (!name) return;
    try {
      await api.createCategory(name);
      els.categoryName.value = "";
      showToast("Категория добавлена");
      await refreshAll();
    } catch (err) {
      showToast(err.message, true);
    }
  });

  els.txForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    const kindInput = document.querySelector('input[name="tx-kind"]:checked');
    const kind = kindInput ? kindInput.value : "expense";
    const raw = els.txAmount.value.trim().replace(",", ".");
    const numeric = Number(raw);
    if (Number.isNaN(numeric)) {
      showToast("Введите число в поле суммы", true);
      return;
    }
    const signed = kind === "expense" ? -Math.abs(numeric) : Math.abs(numeric);

    const payload = {
      category_id: Number(els.txCategory.value),
      amount_rub: String(signed),
      occurred_at: els.txDate.value,
      note: els.txNote.value.trim(),
    };
    try {
      if (state.editingTxId) {
        await api.updateTransaction(state.editingTxId, payload);
        showToast("Операция обновлена");
      } else {
        await api.createTransaction(payload);
        showToast("Операция записана");
      }
      state.editingTxId = null;
      els.txForm.reset();
      initDefaults();
      await refreshAll();
    } catch (err) {
      showToast(err.message, true);
    }
  });

  els.filterApply.addEventListener("click", (e) => {
    e.preventDefault();
    state.page = 0;
    refreshAll();
  });

  els.categoryList.addEventListener("click", async (e) => {
    if (e.target.matches("button[data-id]")) {
      const id = e.target.getAttribute("data-id");
      if (!id) {
        showToast("Некорректный идентификатор категории", true);
        return;
      }
      try {
        await api.deleteCategory(id);
        showToast("Категория удалена");
        await refreshAll();
      } catch (err) {
        showToast(err.message || "Не удалось удалить категорию", true);
      }
    }
  });

  if (els.budgetForm) {
    els.budgetForm.addEventListener("submit", async (e) => {
      e.preventDefault();
      const limitRaw = els.budgetLimit.value.trim().replace(",", ".");
      const limit = Number(limitRaw);
      if (Number.isNaN(limit) || limit <= 0) {
        showToast("Лимит должен быть положительным числом", true);
        return;
      }
      const payload = {
        category_id: Number(els.budgetCategory.value),
        limit_rub: String(limit),
      };
      try {
        await api.upsertBudget(payload);
        showToast("Бюджет сохранён");
        els.budgetForm.reset();
        initDefaults();
        await refreshAll();
      } catch (err) {
        showToast(err.message || "Не удалось сохранить бюджет", true);
      }
    });
  }

  els.txTableBody.addEventListener("click", (e) => {
    const tr = e.target.closest("tr");
    if (!tr) return;
    state.editingTxId = Number(tr.dataset.id);
    els.txCategory.value = tr.dataset.categoryId;
    const amount = Number(tr.dataset.amount);
    const kind = amount >= 0 ? "income" : "expense";
    document.querySelector(`input[name="tx-kind"][value="${kind}"]`).checked = true;
    els.txAmount.value = Math.abs(amount / 100).toFixed(2);
    els.txDate.value = tr.dataset.date;
    els.txNote.value = tr.dataset.note;
    showToast("Режим редактирования: измените данные и нажмите Записать");
  });

  document.getElementById("page-prev").addEventListener("click", () => {
    if (state.page > 0) {
      state.page -= 1;
      refreshAll();
    }
  });
  document.getElementById("page-next").addEventListener("click", () => {
    state.page += 1;
    refreshAll();
  });
}

function initDefaults() {
  const today = new Date().toISOString().slice(0, 10);
  els.txDate.value = today;
  els.filterTo.value = today;
  els.filterFrom.value = "1970-01-01";
}

window.addEventListener("DOMContentLoaded", () => {
  initDefaults();
  wireEvents();
  refreshAll();
});
