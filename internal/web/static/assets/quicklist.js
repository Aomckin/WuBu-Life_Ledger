"use strict";

const CATALOG = [
  [1, "Reis", "packs", "pantry", "rice_bowl"],
  [2, "Nudeln/Spaghetti", "packs", "pantry", "ramen_dining"],
  [3, "Mehl", "packs", "pantry", "grocery"],
  [4, "Zucker", "packs", "pantry", "grocery"],
  [5, "Salz", "packs", "pantry", "grocery"],
  [6, "Haferflocken", "packs", "pantry", "breakfast_dining"],
  [7, "Müsli", "packs", "pantry", "breakfast_dining"],
  [8, "Cornflakes", "packs", "pantry", "breakfast_dining"],
  [9, "Couscous", "packs", "pantry", "rice_bowl"],
  [10, "Bulgur", "packs", "pantry", "rice_bowl"],
  [11, "Linsen", "packs", "pantry", "grocery"],
  [12, "Getrocknete Kichererbsen", "packs", "pantry", "grocery"],
  [13, "Getrocknete Bohnen", "packs", "pantry", "grocery"],
  [14, "Kartoffelpüreepulver", "packs", "pantry", "grocery"],
  [15, "Paniermehl", "packs", "pantry", "bakery_dining"],
  [16, "Grieß", "packs", "pantry", "grocery"],
  [17, "Speisestärke", "packs", "pantry", "grocery"],
  [18, "Backpulver", "packs", "pantry", "bakery_dining"],
  [19, "Vanillezucker", "sachets", "pantry", "bakery_dining"],
  [20, "Trockenhefe", "sachets", "pantry", "bakery_dining"],
  [21, "Kakao/Backkakao", "packs", "pantry", "coffee"],
  [22, "Puderzucker", "packs", "pantry", "bakery_dining"],
  [23, "Schokostreusel", "packs", "pantry", "cake"],
  [24, "Gelatine/Agar-Agar", "packs", "pantry", "grocery"],
  [25, "Natron", "packs", "pantry", "grocery"],
  [26, "Tortillas/Wraps", "packs", "pantry", "lunch_dining"],
  [27, "Knäckebrot", "packs", "pantry", "bakery_dining"],
  [28, "Zwieback", "packs", "pantry", "bakery_dining"],
  [29, "Reis-/Maiswaffeln", "packs", "pantry", "cookie"],
  [30, "Instantnudeln", "packs", "pantry", "ramen_dining"],
  [31, "Gemüsebrühe", "packs", "pantry", "soup_kitchen"],
  [32, "Fleischbrühe", "packs", "pantry", "soup_kitchen"],
  [33, "Gemahlener Kaffee", "packs", "pantry", "coffee"],
  [34, "Kaffeebohnen", "packs", "pantry", "coffee"],
  [35, "Kaffeekapseln", "packs", "pantry", "coffee_maker"],
  [36, "Kaffeefilter", "packs", "pantry", "coffee_maker"],
  [37, "Schwarzer Tee", "packs", "pantry", "emoji_food_beverage"],
  [38, "Kräuter-/Früchtetee", "packs", "pantry", "emoji_food_beverage"],
  [39, "Honig", "jars", "pantry", "grocery"],
  [40, "Nuss-Nougat-Creme", "jars", "pantry", "grocery"],
  [41, "Marmelade", "jars", "pantry", "grocery"],
  [42, "Erdnussbutter", "jars", "pantry", "grocery"],
  [43, "Passierte Tomaten", "packs", "pantry", "grocery"],
  [44, "Stückige Tomaten", "cans", "pantry", "grocery"],
  [45, "Tomatenmark", "tubes", "pantry", "grocery"],
  [46, "Mais", "cans", "pantry", "grocery"],
  [47, "Erbsen", "cans", "pantry", "grocery"],
  [48, "Kidneybohnen", "cans", "pantry", "grocery"],
  [49, "Kichererbsen", "cans", "pantry", "grocery"],
  [50, "Thunfisch", "cans", "pantry", "set_meal"],
  [51, "Kokosmilch", "cans", "pantry", "local_drink"],
  [52, "Suppe/Eintopf", "cans", "pantry", "soup_kitchen"],
  [53, "Apfelmus", "jars", "pantry", "nutrition"],
  [54, "Obstkonserven", "cans", "pantry", "nutrition"],
  [55, "Gewürzgurken", "jars", "pantry", "grocery"],
  [56, "Oliven", "jars", "pantry", "grocery"],
  [57, "Pesto", "jars", "pantry", "grocery"],
  [58, "Pastasoße", "jars", "pantry", "grocery"],
  [59, "Ketchup", "bottles", "pantry", "grocery"],
  [60, "Mayonnaise", "jars", "pantry", "grocery"],
  [61, "Senf", "jars", "pantry", "grocery"],
  [62, "Sojasoße", "bottles", "pantry", "grocery"],
  [63, "Essig", "bottles", "pantry", "water_drop"],
  [64, "Speiseöl", "bottles", "pantry", "water_drop"],
  [65, "Olivenöl", "bottles", "pantry", "water_drop"],
  [66, "Schwarzer Pfeffer", "pcs", "pantry", "grocery"],
  [67, "Paprikapulver", "pcs", "pantry", "grocery"],
  [68, "Currypulver", "pcs", "pantry", "grocery"],
  [69, "Zimt", "pcs", "pantry", "grocery"],
  [70, "Kräutersalz/Gewürzmischung", "pcs", "pantry", "grocery"],
  [71, "Kartoffelchips", "packs", "pantry", "cookie"],
  [72, "Tortillachips", "packs", "pantry", "cookie"],
  [73, "Salzgebäck/Cracker", "packs", "pantry", "cookie"],
  [74, "Kekse", "packs", "pantry", "cookie"],
  [75, "Schokolade", "pcs", "pantry", "cake"],
  [76, "Gummibärchen", "packs", "pantry", "cookie"],
  [77, "Müsliriegel", "packs", "pantry", "nutrition"],
  [78, "Nüsse", "packs", "pantry", "nutrition"],
  [79, "Trockenfrüchte", "packs", "pantry", "nutrition"],
  [80, "Popcorn", "packs", "pantry", "cookie"],
  [81, "TK-Pizza", "pcs", "pantry", "local_pizza", "Freezer"],
  [82, "TK-Pommes", "packs", "pantry", "fastfood", "Freezer"],
  [83, "TK-Gemüse", "packs", "pantry", "nutrition", "Freezer"],
  [84, "TK-Obst/Beeren", "packs", "pantry", "nutrition", "Freezer"],
  [85, "TK-Fisch", "packs", "pantry", "set_meal", "Freezer"],
  [86, "TK-Fleisch", "packs", "pantry", "set_meal", "Freezer"],
  [87, "TK-Fertiggericht", "packs", "pantry", "dinner_dining", "Freezer"],
  [88, "Eiscreme", "packs", "pantry", "icecream", "Freezer"],
  [89, "TK-Kräuter", "packs", "pantry", "nutrition", "Freezer"],
  [90, "Eiswürfelbeutel", "packs", "pantry", "water_drop", "Freezer"],
  [91, "Milch", "packs", "pantry", "local_drink", "Fridge"],
  [92, "Hafer-/Mandelmilch", "packs", "pantry", "local_drink", "Fridge"],
  [93, "Butter", "packs", "pantry", "grocery", "Fridge"],
  [94, "Margarine", "packs", "pantry", "grocery", "Fridge"],
  [95, "Scheibenkäse", "packs", "pantry", "grocery", "Fridge"],
  [96, "Stückkäse", "packs", "pantry", "grocery", "Fridge"],
  [97, "Frischkäse", "packs", "pantry", "grocery", "Fridge"],
  [98, "Joghurt", "cups", "pantry", "grocery", "Fridge"],
  [99, "Quark", "cups", "pantry", "grocery", "Fridge"],
  [100, "Sahne", "cups", "pantry", "grocery", "Fridge"],
  [101, "Schmand/saure Sahne", "cups", "pantry", "grocery", "Fridge"],
  [102, "Eier", "packs", "pantry", "egg", "Fridge"],
  [103, "Aufschnitt", "packs", "pantry", "lunch_dining", "Fridge"],
  [104, "Schinken/Speck", "packs", "pantry", "lunch_dining", "Fridge"],
  [105, "Würstchen", "packs", "pantry", "lunch_dining", "Fridge"],
  [106, "Fleischportionen", "packs", "pantry", "dinner_dining", "Fridge"],
  [107, "Fischportionen", "packs", "pantry", "set_meal", "Fridge"],
  [108, "Tofu", "packs", "pantry", "nutrition", "Fridge"],
  [109, "Hummus", "packs", "pantry", "nutrition", "Fridge"],
  [110, "Baguette/Brot", "pcs", "pantry", "bakery_dining"],
  [111, "Bananen", "pcs", "pantry", "nutrition"],
  [112, "Äpfel", "pcs", "pantry", "nutrition"],
  [113, "Orangen", "pcs", "pantry", "nutrition"],
  [114, "Zitronen", "pcs", "pantry", "nutrition"],
  [115, "Kartoffeln", "bags", "pantry", "nutrition"],
  [116, "Zwiebeln", "bags", "pantry", "nutrition"],
  [117, "Knoblauch", "pcs", "pantry", "nutrition"],
  [118, "Tomaten", "packs", "pantry", "nutrition"],
  [119, "Gurken", "pcs", "pantry", "nutrition"],
  [120, "Möhren", "bags", "pantry", "nutrition"],
  [121, "Orangensaft", "packs", "drinks", "local_drink"],
  [122, "Apfelsaft", "packs", "drinks", "local_drink"],
  [123, "Multivitaminsaft", "packs", "drinks", "local_drink"],
  [124, "Cola/Limonade", "bottles", "drinks", "local_drink"],
  [125, "Energy-Drink", "cans", "drinks", "local_drink"],
  [126, "Bier", "crates", "drinks", "liquor"],
  [127, "Wein", "bottles", "drinks", "wine_bar"],
  [128, "Sekt", "bottles", "drinks", "wine_bar"],
  [129, "Getränkesirup", "bottles", "drinks", "local_drink"],
  [130, "Instantkaffee", "jars", "drinks", "coffee"],
  [131, "Löslicher Trinkkakao", "packs", "drinks", "coffee"],
  [132, "Isotonisches Getränk", "bottles", "drinks", "water_bottle"],
  [133, "Eistee", "bottles", "drinks", "local_drink"],
  [134, "Milchdrink/Proteinshake", "bottles", "drinks", "local_drink"],
  [135, "Trinkpäckchen", "packs", "drinks", "local_drink"],
  [136, "Spülmittel", "bottles", "cleaning", "soap"],
  [137, "Klarspüler", "bottles", "cleaning", "dishwasher"],
  [138, "Spülmaschinensalz", "packs", "cleaning", "dishwasher"],
  [139, "Allzweckreiniger", "bottles", "cleaning", "cleaning_services"],
  [140, "Badreiniger", "bottles", "cleaning", "bathroom"],
  [141, "WC-Reiniger", "bottles", "cleaning", "wc"],
  [142, "Glasreiniger", "bottles", "cleaning", "cleaning_services"],
  [143, "Küchenentfetter", "bottles", "cleaning", "cleaning_services"],
  [144, "Bodenreiniger", "bottles", "cleaning", "mop"],
  [145, "Desinfektionsspray", "bottles", "cleaning", "sanitizer"],
  [146, "Reinigungstücher", "packs", "cleaning", "cleaning"],
  [147, "Scheuermilch", "bottles", "cleaning", "cleaning_services"],
  [148, "Backofenreiniger", "bottles", "cleaning", "oven"],
  [149, "Abflussreiniger", "bottles", "cleaning", "water_drop"],
  [150, "Schimmelentferner", "bottles", "cleaning", "cleaning_services"],
  [151, "Kalkreiniger", "bottles", "cleaning", "cleaning_services"],
  [152, "Edelstahlreiniger", "bottles", "cleaning", "cleaning_services"],
  [153, "Möbelpolitur", "bottles", "cleaning", "chair"],
  [154, "Reinigungsschwämme", "packs", "cleaning", "cleaning"],
  [155, "Topfreiniger", "packs", "cleaning", "skillet"],
  [156, "Reinigungshandschuhe", "packs", "cleaning", "clean_hands"],
  [157, "Mopp-Bezüge/Refills", "pcs", "cleaning", "mop"],
  [158, "Staubsaugerbeutel", "packs", "cleaning", "vacuum"],
  [159, "Staubsaugerfilter", "pcs", "cleaning", "vacuum"],
  [160, "Lufterfrischer", "pcs", "cleaning", "air_freshener"],
  [161, "Entkalker", "packs", "cleaning", "cleaning_services"],
  [162, "Waschmittel Color", "packs", "laundry", "laundry"],
  [163, "Waschmittel Weiß", "packs", "laundry", "laundry"],
  [164, "Feinwaschmittel", "bottles", "laundry", "laundry"],
  [165, "Fleckenentferner", "bottles", "laundry", "laundry"],
  [166, "Toilettenpapier", "packs", "bathroom", "bathroom"],
  [167, "Taschentücher", "packs", "bathroom", "inventory_2"],
  [168, "Flüssigseife", "bottles", "bathroom", "soap"],
  [169, "Duschgel", "bottles", "bathroom", "shower"],
  [170, "Shampoo", "bottles", "bathroom", "wash"],
  [171, "Conditioner", "bottles", "bathroom", "wash"],
  [172, "Deo", "pcs", "bathroom", "air_freshener"],
  [173, "Zahnbürsten", "pcs", "bathroom", "dentistry"],
  [174, "Zahnseide", "packs", "bathroom", "dentistry"],
  [175, "Mundspülung", "bottles", "bathroom", "dentistry"],
  [176, "Rasierklingen", "packs", "bathroom", "styler"],
  [177, "Rasierschaum", "cans", "bathroom", "soap"],
  [178, "Wattestäbchen", "packs", "bathroom", "inventory_2"],
  [179, "Wattepads", "packs", "bathroom", "inventory_2"],
  [180, "Menstruationsprodukte", "packs", "bathroom", "medical_services"],
  [181, "Bodylotion", "bottles", "bathroom", "spa"],
  [182, "Handcreme", "tubes", "bathroom", "clean_hands"],
  [183, "Sonnencreme", "tubes", "bathroom", "spa"],
  [184, "Lippenpflege", "pcs", "bathroom", "spa"],
  [185, "Pflaster", "packs", "health", "healing"],
  [186, "Wunddesinfektion", "bottles", "health", "sanitizer"],
  [187, "Verbandsmaterial", "packs", "health", "medical_services"],
  [188, "Erkältungsmittel", "packs", "health", "medication"],
  [189, "Nasenspray", "bottles", "health", "medication_liquid"],
  [190, "Halstabletten", "packs", "health", "pill"],
  [191, "Küchenrolle", "packs", "household", "inventory_2"],
  [192, "Müllbeutel klein", "rolls", "household", "delete"],
  [193, "Müllbeutel groß", "rolls", "household", "delete"],
  [194, "Biomüllbeutel", "rolls", "household", "compost"],
  [195, "Gelbe Säcke", "rolls", "household", "recycling"],
  [196, "Gefrierbeutel", "packs", "household", "kitchen"],
  [197, "Zip-Beutel", "packs", "household", "inventory_2"],
  [198, "Frischhaltefolie", "rolls", "household", "inventory_2"],
  [199, "Alufolie", "rolls", "household", "inventory_2"],
  [200, "Backpapier", "rolls", "household", "bakery_dining"],
  [201, "Zahnpasta", "tubes", "bathroom", "dentistry"],
  [202, "Weichspüler", "bottles", "laundry", "laundry"],
  [203, "Spülmaschinentabs", "packs", "cleaning", "dishwasher"],
  [204, "Schmerzmittel", "boxes", "health", "pill"],
  [205, "Wasserkasten", "crates", "drinks", "water_bottle"],
  [206, "Toasti", "packs", "pantry", "bakery_dining"],
  [207, "Toasti TK", "packs", "pantry", "bakery_dining", "Freezer"],
  [208, "TK-Brötchen", "pcs", "pantry", "bakery_dining", "Freezer"],
].map(([id, name, unit, category, icon, location = ""]) => ({ id, name, unit, category, icon, location }));

const UNIT_LABELS = {
  packs: "packs", cans: "cans", jars: "jars", bottles: "bottles", pcs: "pcs",
  bags: "bags", rolls: "rolls", tubes: "tubes", boxes: "boxes",
  crates: "crates", cups: "cups", sachets: "sachets",
};

const CATEGORY_LABELS = {
  pantry: "Pantry & food", drinks: "Drinks", cleaning: "Cleaning",
  laundry: "Laundry", bathroom: "Bathroom & care", health: "Health",
  household: "Household",
};

const state = {
  existingByName: new Map(),
  baseline: new Map(),
  values: new Map(),
  dirty: new Set(),
  rows: new Map(),
  busy: false,
};

const elements = {
  list: document.querySelector("#quick-list"),
  template: document.querySelector("#item-template"),
  search: document.querySelector("#search"),
  category: document.querySelector("#category-filter"),
  changedOnly: document.querySelector("#changed-only"),
  visibleCount: document.querySelector("#visible-count"),
  loadStatus: document.querySelector("#load-status"),
  reset: document.querySelector("#reset-changes"),
  changedCount: document.querySelector("#changed-count"),
  quantityTotal: document.querySelector("#quantity-total"),
  previewButton: document.querySelector("#preview-button"),
  empty: document.querySelector("#empty-state"),
  dialog: document.querySelector("#preview-dialog"),
  closePreview: document.querySelector("#close-preview"),
  cancelPreview: document.querySelector("#cancel-preview"),
  importButton: document.querySelector("#import-button"),
  importSummary: document.querySelector("#import-summary"),
  jsonPreview: document.querySelector("#json-preview"),
  result: document.querySelector("#result-message"),
  toast: document.querySelector("#toast"),
};

function normalize(value) {
  return value.trim().toLocaleLowerCase("de-DE");
}

function roundQuantity(value) {
  const finite = Number.isFinite(value) ? value : 0;
  return Math.round(Math.max(0, Math.min(1_000_000_000, finite)) * 1000) / 1000;
}

function formatQuantity(value) {
  return new Intl.NumberFormat("en", { maximumFractionDigits: 3 }).format(value);
}

async function request(path, options = {}) {
  const headers = new Headers(options.headers || {});
  headers.set("Accept", "application/json");
  if (options.body) headers.set("Content-Type", "application/json");
  const response = await fetch(path, { ...options, headers });
  if (response.status === 204) return null;
  const data = await response.json().catch(() => null);
  if (!response.ok) throw new Error(data?.error?.message || `HTTP ${response.status}`);
  return data;
}

async function initialize() {
  if (CATALOG.length !== 208 || new Set(CATALOG.map((item) => item.id)).size !== CATALOG.length) {
    throw new Error("The Quicklist catalogue is invalid.");
  }
  bindEvents();
  renderCatalog();
  try {
    await loadInventory();
    elements.loadStatus.textContent = "Current inventory loaded.";
  } catch (error) {
    elements.loadStatus.textContent = `Inventory unavailable: ${error.message}`;
    showToast("The current inventory could not be loaded.");
  }
}

function bindEvents() {
  elements.search.addEventListener("input", applyFilters);
  elements.category.addEventListener("change", applyFilters);
  elements.changedOnly.addEventListener("change", applyFilters);
  elements.list.addEventListener("input", handleQuantityInput);
  elements.reset.addEventListener("click", resetChanges);
  elements.previewButton.addEventListener("click", openPreview);
  elements.closePreview.addEventListener("click", closePreview);
  elements.cancelPreview.addEventListener("click", closePreview);
  elements.importButton.addEventListener("click", importChanges);
  elements.dialog.addEventListener("cancel", (event) => {
    if (state.busy) event.preventDefault();
  });
}

function renderCatalog() {
  const fragment = document.createDocumentFragment();
  for (const definition of CATALOG) {
    const row = elements.template.content.firstElementChild.cloneNode(true);
    row.dataset.id = definition.id;
    row.dataset.category = definition.category;
    row.dataset.search = normalize(`${definition.name} ${CATEGORY_LABELS[definition.category]}`);
    row.querySelector(".item-id").textContent = definition.id;
    row.querySelector(".item-icon").textContent = definition.icon;
    row.querySelector(".item-copy strong").textContent = definition.name;
    row.querySelector(".item-copy span").textContent = `${CATEGORY_LABELS[definition.category]} · ${UNIT_LABELS[definition.unit]}`;
    const range = row.querySelector(".range-input");
    const number = row.querySelector(".number-input");
    range.dataset.id = definition.id;
    number.dataset.id = definition.id;
    range.setAttribute("aria-label", `${definition.name} quantity slider`);
    number.setAttribute("aria-label", `${definition.name} exact quantity`);
    number.value = "0";
    row.querySelector(".number-wrap small").textContent = UNIT_LABELS[definition.unit];
    state.rows.set(definition.id, row);
    state.baseline.set(definition.id, 0);
    state.values.set(definition.id, 0);
    fragment.append(row);
  }
  elements.list.append(fragment);
  applyFilters();
}

async function loadInventory() {
  const result = await request("/api/v1/items?limit=500");
  state.existingByName = new Map(result.items.map((item) => [normalize(item.name), item]));
  state.dirty.clear();
  for (const definition of CATALOG) {
    const existing = state.existingByName.get(normalize(definition.name));
    const quantity = existing?.quantity ?? 0;
    state.baseline.set(definition.id, quantity);
    state.values.set(definition.id, quantity);
    updateRow(definition.id, quantity, false, Boolean(existing));
  }
  updateSummary();
  applyFilters();
}

function handleQuantityInput(event) {
  const input = event.target.closest(".range-input, .number-input");
  if (!input || state.busy) return;
  const id = Number(input.dataset.id);
  const value = roundQuantity(Number(input.value));
  state.values.set(id, value);
  const baseline = state.baseline.get(id) ?? 0;
  if (Math.abs(value - baseline) < 0.0005) state.dirty.delete(id);
  else state.dirty.add(id);
  updateRow(id, value, state.dirty.has(id), state.existingByName.has(normalize(CATALOG[id - 1].name)));
  updateSummary();
  applyFilters();
}

function updateRow(id, value, dirty, exists) {
  const row = state.rows.get(id);
  if (!row) return;
  const range = row.querySelector(".range-input");
  const number = row.querySelector(".number-input");
  range.max = String(Math.max(20, Math.ceil(value)));
  range.value = String(Math.min(value, Number(range.max)));
  number.value = String(value);
  row.classList.toggle("is-dirty", dirty);
  const definition = CATALOG[id - 1];
  row.querySelector(".item-copy span").textContent = exists
    ? `${CATEGORY_LABELS[definition.category]} · currently ${formatQuantity(state.baseline.get(id))} ${UNIT_LABELS[definition.unit]}`
    : `${CATEGORY_LABELS[definition.category]} · ${UNIT_LABELS[definition.unit]}`;
}

function applyFilters() {
  const query = normalize(elements.search.value);
  const category = elements.category.value;
  const changedOnly = elements.changedOnly.checked;
  let visible = 0;
  for (const definition of CATALOG) {
    const row = state.rows.get(definition.id);
    const matches = (!query || row.dataset.search.includes(query))
      && (!category || definition.category === category)
      && (!changedOnly || state.dirty.has(definition.id));
    row.classList.toggle("is-hidden", !matches);
    if (matches) visible += 1;
  }
  elements.visibleCount.textContent = `${visible} ${visible === 1 ? "item" : "items"}`;
  elements.empty.hidden = visible !== 0;
}

function updateSummary() {
  const changes = changedDefinitions();
  let created = 0;
  let updated = 0;
  let zeroed = 0;
  for (const entry of changes) {
    const exists = state.existingByName.has(normalize(entry.name));
    if (!exists && entry.quantity > 0) created += 1;
    else if (exists && entry.quantity === 0) zeroed += 1;
    else if (exists) updated += 1;
  }
  elements.changedCount.textContent = `${changes.length} changed`;
  elements.quantityTotal.textContent = changes.length === 0
    ? "No changes yet"
    : `${created} new · ${updated} adjusted · ${zeroed} set to 0`;
  elements.previewButton.disabled = changes.length === 0 || state.busy;
  elements.reset.disabled = changes.length === 0 || state.busy;
}

function changedDefinitions() {
  return [...state.dirty].sort((a, b) => a - b).map((id) => ({
    ...CATALOG[id - 1],
    quantity: state.values.get(id) ?? 0,
  }));
}

function resetChanges() {
  for (const id of state.dirty) {
    const value = state.baseline.get(id) ?? 0;
    state.values.set(id, value);
    const definition = CATALOG[id - 1];
    updateRow(id, value, false, state.existingByName.has(normalize(definition.name)));
  }
  state.dirty.clear();
  updateSummary();
  applyFilters();
}

function buildPayload() {
  return {
    version: 1,
    generated_at: new Date().toISOString(),
    items: changedDefinitions().map(({ id, name, quantity, unit }) => ({ id, name, quantity, unit })),
  };
}

function openPreview() {
  const payload = buildPayload();
  if (payload.items.length === 0) return;
  elements.importSummary.replaceChildren();
  for (const item of payload.items) {
    const line = document.createElement("div");
    line.className = "summary-line";
    const name = document.createElement("span");
    name.textContent = `${item.id} · ${item.name}`;
    const amount = document.createElement("strong");
    amount.textContent = `${formatQuantity(item.quantity)} ${UNIT_LABELS[item.unit]}`;
    line.append(name, amount);
    elements.importSummary.append(line);
  }
  elements.jsonPreview.textContent = JSON.stringify(payload, null, 2);
  elements.result.hidden = true;
  elements.result.classList.remove("is-error");
  elements.importButton.disabled = false;
  elements.importButton.textContent = "Import into Stockroom";
  elements.dialog.showModal();
}

function closePreview() {
  if (!state.busy) elements.dialog.close();
}

async function importChanges() {
  const changes = changedDefinitions();
  if (changes.length === 0 || state.busy) return;
  setBusy(true);
  elements.result.hidden = true;
  elements.result.classList.remove("is-error");
  try {
    const latest = await request("/api/v1/items?limit=500");
    const currentByName = new Map(latest.items.map((item) => [normalize(item.name), item]));
    const jobs = changes.map((definition) => async () => {
      const existing = currentByName.get(normalize(definition.name));
      if (existing) {
        await request(`/api/v1/items/${encodeURIComponent(existing.id)}`, {
          method: "PATCH",
          body: JSON.stringify({ quantity: definition.quantity }),
        });
        return "updated";
      }
      if (definition.quantity === 0) return "skipped";
      await request("/api/v1/items", {
        method: "POST",
        body: JSON.stringify({
          name: definition.name,
          category: definition.category,
          icon: definition.icon,
          quantity: definition.quantity,
          unit: definition.unit,
          location: definition.location,
        }),
      });
      return "created";
    });

    const outcomes = [];
    for (let offset = 0; offset < jobs.length; offset += 8) {
      const batch = jobs.slice(offset, offset + 8);
      outcomes.push(...await Promise.allSettled(batch.map((job) => job())));
    }
    const failures = outcomes.filter((outcome) => outcome.status === "rejected");
    if (failures.length > 0) {
      throw new Error(`${failures.length} of ${outcomes.length} items could not be saved.`);
    }
    const created = outcomes.filter((outcome) => outcome.value === "created").length;
    const updated = outcomes.filter((outcome) => outcome.value === "updated").length;
    await loadInventory();
    elements.result.textContent = `${created} created, ${updated} updated.`;
    elements.result.hidden = false;
    elements.importButton.textContent = "Imported";
    elements.importButton.disabled = true;
    showToast("Quicklist imported successfully.");
    setTimeout(() => elements.dialog.close(), 700);
  } catch (error) {
    elements.result.textContent = error.message;
    elements.result.classList.add("is-error");
    elements.result.hidden = false;
    elements.importButton.textContent = "Try again";
  } finally {
    setBusy(false);
  }
}

function setBusy(busy) {
  state.busy = busy;
  elements.importButton.disabled = busy;
  elements.cancelPreview.disabled = busy;
  elements.closePreview.disabled = busy;
  elements.importButton.textContent = busy ? "Saving…" : elements.importButton.textContent;
  updateSummary();
}

let toastTimer;
function showToast(message) {
  clearTimeout(toastTimer);
  elements.toast.textContent = message;
  elements.toast.hidden = false;
  toastTimer = setTimeout(() => { elements.toast.hidden = true; }, 3200);
}

initialize();
