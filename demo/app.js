const storageKey = "stockroom-pages-demo-v1";

const seed = {
  items: [
    { id: "oat-milk", name: "Oat milk", amount: 2, unit: "cartons", category: "Drinks", lowStock: 1 },
    { id: "pasta", name: "Fusilli pasta", amount: 1, unit: "bag", category: "Pantry", lowStock: 1 },
    { id: "dish-soap", name: "Dish soap", amount: 1, unit: "bottle", category: "Cleaning", lowStock: 1 }
  ],
  ready: null,
  filter: "all"
};

const cloneSeed = () => JSON.parse(JSON.stringify(seed));

function readState() {
  try {
    const parsed = JSON.parse(localStorage.getItem(storageKey));
    if (parsed && Array.isArray(parsed.items)) return { ...cloneSeed(), ...parsed };
  } catch {
    // A malformed browser value should never break the public demo.
  }
  return cloneSeed();
}

const state = readState();
const elements = {
  itemList: document.querySelector("#item-list"),
  itemCount: document.querySelector("#item-count"),
  lowCount: document.querySelector("#low-count"),
  readyCount: document.querySelector("#ready-count"),
  queueStatus: document.querySelector("#queue-status"),
  queueCard: document.querySelector("#queue-card"),
  applyReady: document.querySelector("#apply-ready"),
  addItemForm: document.querySelector("#add-item-form"),
  captureForm: document.querySelector("#capture-form"),
  captureText: document.querySelector("#capture-text"),
  resetDemo: document.querySelector("#reset-demo"),
  toast: document.querySelector("#toast")
};

function save() {
  localStorage.setItem(storageKey, JSON.stringify(state));
}

function isLow(item) {
  return item.amount <= item.lowStock;
}

function formatAmount(amount) {
  return Number.isInteger(amount) ? String(amount) : amount.toFixed(1).replace(/\.0$/, "");
}

function firstLetter(value) {
  return value.trim().slice(0, 1).toUpperCase() || "?";
}

function showToast(message) {
  elements.toast.textContent = message;
  elements.toast.classList.add("is-visible");
  window.clearTimeout(showToast.timer);
  showToast.timer = window.setTimeout(() => elements.toast.classList.remove("is-visible"), 2400);
}

function createItemRow(item) {
  const row = document.createElement("article");
  row.className = "item-row";
  const mark = document.createElement("span");
  mark.className = "item-mark";
  mark.textContent = firstLetter(item.name);
  const copy = document.createElement("div");
  copy.className = "item-copy";
  const name = document.createElement("strong");
  name.textContent = item.name;
  if (isLow(item)) {
    const dot = document.createElement("span");
    dot.className = "low-dot";
    dot.title = "Low stock";
    name.append(dot);
  }
  const category = document.createElement("small");
  category.textContent = item.category;
  copy.append(name, category);
  const quantity = document.createElement("div");
  quantity.className = "quantity";
  quantity.textContent = formatAmount(item.amount);
  const unit = document.createElement("small");
  unit.textContent = item.unit;
  quantity.append(unit);
  row.append(mark, copy, quantity);
  return row;
}

function renderInventory() {
  const shown = state.filter === "low" ? state.items.filter(isLow) : state.items;
  elements.itemList.replaceChildren();
  if (!shown.length) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = state.filter === "low" ? "Nothing is low right now." : "No demo items yet.";
    elements.itemList.append(empty);
  } else {
    shown.forEach((item) => elements.itemList.append(createItemRow(item)));
  }
  elements.itemCount.textContent = String(state.items.length);
  elements.lowCount.textContent = String(state.items.filter(isLow).length);
}

function renderQueue() {
  elements.queueCard.replaceChildren();
  const ready = state.ready;
  elements.readyCount.textContent = ready ? "1" : "0";
  elements.applyReady.hidden = !ready;
  if (!ready) {
    elements.queueStatus.textContent = "No staged changes";
    return;
  }
  elements.queueStatus.textContent = "1 safe suggestion";
  const card = document.createElement("div");
  card.className = "queue-card";
  const label = document.createElement("span");
  label.textContent = "Demo analysis · ready";
  const name = document.createElement("strong");
  name.textContent = ready.name;
  const details = document.createElement("small");
  details.textContent = `${ready.intent === "increase" ? "Add" : "Use"} ${formatAmount(ready.amount)} ${ready.unit}`;
  card.append(label, name, details);
  elements.queueCard.append(card);
}

function render() {
  document.querySelectorAll(".filter").forEach((button) => button.classList.toggle("is-active", button.dataset.filter === state.filter));
  renderInventory();
  renderQueue();
  save();
}

function cleanCaptureName(value) {
  const stripped = value
    .replace(/\b(bought|got|added|used|finished|ate|drank|of|the|a|an)\b/gi, " ")
    .replace(/[0-9.,]+/g, " ")
    .replace(/\b(cartons?|bottles?|bags?|packs?|items?)\b/gi, " ")
    .replace(/\s+/g, " ")
    .trim();
  return stripped ? stripped.replace(/\b\w/g, (letter) => letter.toUpperCase()) : "Demo item";
}

function stageCapture(value) {
  const amountMatch = value.match(/\b(\d+(?:[.,]\d+)?)\b/);
  const amount = amountMatch ? Number(amountMatch[1].replace(",", ".")) : 1;
  const lower = value.toLowerCase();
  const intent = /\b(used|finished|ate|drank|ran out)\b/.test(lower) ? "decrease" : "increase";
  const unitMatch = lower.match(/\b(cartons?|bottles?|bags?|packs?|items?)\b/);
  const unit = unitMatch ? unitMatch[0].replace(/s$/, "") : "item";
  state.ready = { name: cleanCaptureName(value), amount: Math.max(amount, 0.1), unit, intent };
  elements.captureText.value = "";
  render();
  showToast("Demo analysis staged. Review then apply it.");
}

function applyReady() {
  const ready = state.ready;
  if (!ready) return;
  const existing = state.items.find((item) => item.name.toLowerCase() === ready.name.toLowerCase());
  const delta = ready.intent === "increase" ? ready.amount : -ready.amount;
  if (existing) {
    existing.amount = Math.max(0, Number((existing.amount + delta).toFixed(1)));
  } else if (delta > 0) {
    state.items.unshift({ id: crypto.randomUUID(), name: ready.name, amount: ready.amount, unit: ready.unit, category: "Pantry", lowStock: 1 });
  } else {
    showToast("Full Stockroom would ask for review: item is not in inventory.");
    return;
  }
  state.ready = null;
  render();
  showToast("Ready change applied to demo inventory.");
}

elements.addItemForm.addEventListener("submit", (event) => {
  event.preventDefault();
  const form = new FormData(event.currentTarget);
  const name = String(form.get("name") || "").trim();
  const amount = Number(form.get("amount"));
  const unit = String(form.get("unit") || "").trim();
  const category = String(form.get("category") || "Other");
  if (!name || !unit || !Number.isFinite(amount) || amount <= 0) return;
  state.items.unshift({ id: crypto.randomUUID(), name, amount, unit, category, lowStock: 1 });
  event.currentTarget.reset();
  document.querySelector("#item-amount").value = "1";
  document.querySelector("#item-unit").value = "item";
  render();
  showToast(`${name} added to demo inventory.`);
});

elements.captureForm.addEventListener("submit", (event) => {
  event.preventDefault();
  const value = elements.captureText.value.trim();
  if (!value) {
    showToast("Describe a change first.");
    elements.captureText.focus();
    return;
  }
  stageCapture(value);
});

elements.applyReady.addEventListener("click", applyReady);
elements.resetDemo.addEventListener("click", () => {
  Object.assign(state, cloneSeed());
  render();
  showToast("Demo data reset.");
});
document.querySelectorAll(".filter").forEach((button) => button.addEventListener("click", () => {
  state.filter = button.dataset.filter;
  render();
}));

render();
