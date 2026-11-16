"use strict";

const ICON_NAMES = `
inventory_2 grocery nutrition bakery_dining egg rice_bowl soup_kitchen cookie icecream restaurant flatware food_bank
local_pizza lunch_dining dinner_dining breakfast_dining ramen_dining set_meal takeout_dining kebab_dining fastfood cake
coffee coffee_maker local_drink water_bottle liquor wine_bar local_cafe emoji_food_beverage kitchen microwave oven skillet
skillet_cooktop blender countertops dishwasher stockpot kettle cleaning_services cleaning cleaning_bucket mop vacuum soap
sanitizer wash air_freshener recycling compost delete bathroom bathtub shower wc clean_hands dentistry oral_disease spa
laundry iron dry_cleaning checkroom medication medication_liquid pill vaccines medical_services stethoscope healing thermometer
medical_mask pets pet_supplies child_friendly baby_changing_station stroller bedroom_baby home box package_2 battery_full
lightbulb cable electrical_services power key lock chair bed table_restaurant umbrella hardware construction tools_wrench
tools_power_drill shelves potted_plant yard garden_cart garage propane_tank print book school description note_stack styler
inventory category water_drop oil_barrel fire_extinguisher light router devices extension remote_gen blinds curtains
bedroom_parent tv desktop_windows phone_iphone watch headphones photo_camera sports_soccer exercise toys backpack shopping_bag
`.trim().split(/\s+/);

const ICON_KEYWORDS = {
  inventory_2: "general item storage crate stock",
  grocery: "groceries pantry flour sugar cereal pasta food",
  nutrition: "fruit vegetables apple produce healthy",
  bakery_dining: "bread rolls toast bakery",
  egg: "eggs dairy breakfast",
  rice_bowl: "rice grain bowl",
  soup_kitchen: "soup can tin meal",
  cookie: "cookies biscuits snacks",
  icecream: "ice cream frozen dessert",
  local_drink: "juice soda soft drink glass",
  water_bottle: "water bottle milk beverage",
  coffee: "coffee tea beans ground",
  liquor: "beer spirits alcohol",
  kitchen: "fridge freezer kitchen food storage",
  stockpot: "pot pan cookware",
  cleaning_services: "cleaner cleaning spray polish wipes",
  cleaning_bucket: "bucket pail cleaning",
  mop: "mop floor cleaner",
  vacuum: "vacuum dust bag",
  soap: "soap hand wash dish soap",
  sanitizer: "disinfectant sanitizer hygiene",
  air_freshener: "air freshener fragrance room spray",
  wash: "shampoo conditioner body wash",
  bathroom: "toilet paper tissues paper towels bathroom",
  dentistry: "toothbrush toothpaste floss dental",
  laundry: "detergent fabric softener laundry pods",
  iron: "ironing starch clothes",
  medication: "medicine pharmacy first aid",
  medication_liquid: "syrup drops liquid medicine",
  pill: "pills tablets supplements vitamins",
  medical_mask: "mask gloves protective",
  pets: "pet dog cat animal food",
  pet_supplies: "pet supplies litter treats",
  baby_changing_station: "diapers nappies wipes baby",
  battery_full: "battery batteries rechargeable",
  lightbulb: "bulb lamp light",
  cable: "cable wire cord charger",
  paper: "paper towels tissues",
  potted_plant: "plant pot soil indoor",
  garden_cart: "garden soil fertilizer compost",
  propane_tank: "gas propane fuel",
  print: "printer ink toner paper",
  note_stack: "notebook sticky notes paper",
  styler: "clothes clothing textile",
  water_drop: "water liquid drops",
  oil_barrel: "oil fuel lubricant",
  fire_extinguisher: "fire safety extinguisher",
  extension: "extension cord power strip",
  remote_gen: "remote control",
  shopping_bag: "bags shopping reusable",
};

const AUTO_ICON_RULES = [
  [/toilet paper|bathroom tissue|paper towel|tissues?/, "bathroom"],
  [/dish ?soap|washing[- ]?up liquid/, "soap"],
  [/hand ?soap|bar soap/, "soap"],
  [/tooth ?paste|tooth ?brush|dental floss|mouthwash/, "dentistry"],
  [/shampoo|conditioner|body wash|shower gel/, "wash"],
  [/laundry|detergent|fabric softener|washing powder|laundry pod/, "laundry"],
  [/cleaner|cleaning spray|window spray|surface spray|polish/, "cleaning_services"],
  [/disinfect|sanitizer|sanitiser/, "sanitizer"],
  [/mop|floor wipe/, "mop"],
  [/vacuum|dust bag/, "vacuum"],
  [/air freshener|room spray|fragrance/, "air_freshener"],
  [/coffee|espresso/, "coffee"],
  [/tea|cocoa|hot chocolate/, "emoji_food_beverage"],
  [/water/, "water_bottle"],
  [/milk|juice|soda|cola|lemonade/, "local_drink"],
  [/wine|prosecco|champagne/, "wine_bar"],
  [/beer|liquor|vodka|whisky|whiskey|rum/, "liquor"],
  [/bread|toast|bagel|rolls?/, "bakery_dining"],
  [/eggs?/, "egg"],
  [/rice/, "rice_bowl"],
  [/pasta|noodles?|ramen/, "ramen_dining"],
  [/soup|stew/, "soup_kitchen"],
  [/cookie|biscuit|cracker/, "cookie"],
  [/ice ?cream|frozen dessert/, "icecream"],
  [/pizza/, "local_pizza"],
  [/flour|sugar|salt|cereal|oats|grain|canned|tin/, "grocery"],
  [/fruit|apple|banana|orange|vegetable|potato|onion/, "nutrition"],
  [/medicine|first aid|bandage|plaster/, "medical_services"],
  [/tablet|pill|vitamin|supplement/, "pill"],
  [/syrup|drops/, "medication_liquid"],
  [/mask|gloves/, "medical_mask"],
  [/dog|cat|pet food/, "pets"],
  [/litter|pet treat|pet supplies/, "pet_supplies"],
  [/diaper|nappy|baby wipe/, "baby_changing_station"],
  [/battery|batteries/, "battery_full"],
  [/light ?bulb|lamp/, "lightbulb"],
  [/charger|cable|wire|cord/, "cable"],
  [/printer|toner|ink cartridge/, "print"],
  [/plant|potting soil/, "potted_plant"],
  [/fertilizer|garden soil/, "garden_cart"],
  [/propane|gas bottle/, "propane_tank"],
  [/fire extinguisher/, "fire_extinguisher"],
];

const DEFAULT_META = {
  categories: [
    { value: "pantry", label: "Pantry", icon: "grocery" },
    { value: "drinks", label: "饮品", icon: "local_drink" },
    { value: "cleaning", label: "清洁用品", icon: "cleaning_services" },
    { value: "bathroom", label: "Bathroom", icon: "shower" },
    { value: "laundry", label: "洗衣用品", icon: "laundry" },
    { value: "household", label: "家居用品", icon: "home" },
    { value: "health", label: "Health", icon: "medication" },
    { value: "pets", label: "Pets", icon: "pets" },
    { value: "baby", label: "Baby", icon: "child_friendly" },
    { value: "other", label: "Other", icon: "inventory_2" },
  ],
  suggested_units: ["pcs", "packs", "boxes", "bags", "bottles", "cans", "jars", "rolls", "tubes", "tablets", "g", "kg", "ml", "l"],
  suggested_locations: ["Pantry", "Kitchen", "Fridge", "Freezer", "Bathroom", "Laundry room", "Utility closet", "Basement", "Garage", "Bedroom", "Office"],
};

const state = {
  items: [],
  total: 0,
  meta: DEFAULT_META,
  filters: { search: "", category: "", location: "", status: "" },
  showOutOfStock: false,
  editingId: null,
  selectedIcon: "inventory_2",
  iconManuallyChosen: false,
  requestSequence: 0,
  loading: true,
};

const elements = {
  add: document.querySelector("#add-item"),
  emptyAdd: document.querySelector("#empty-add"),
  itemGrid: document.querySelector("#item-grid"),
  cardTemplate: document.querySelector("#item-card-template"),
  loadingGrid: document.querySelector("#loading-grid"),
  emptyState: document.querySelector("#empty-state"),
  emptyTitle: document.querySelector("#empty-title"),
  emptyCopy: document.querySelector("#empty-copy"),
  clearFilters: document.querySelector("#clear-filters"),
  errorState: document.querySelector("#error-state"),
  errorCopy: document.querySelector("#error-copy"),
  retry: document.querySelector("#retry"),
  resultCount: document.querySelector("#result-count"),
  inventoryTitle: document.querySelector("#inventory-title"),
  search: document.querySelector("#search"),
  clearSearch: document.querySelector("#clear-search"),
  categoryFilter: document.querySelector("#category-filter"),
  locationFilter: document.querySelector("#location-filter"),
  showOutOfStock: document.querySelector("#show-out-of-stock"),
  statusTabs: [...document.querySelectorAll(".status-tab")],
  metricCards: [...document.querySelectorAll(".metric-card[data-summary-filter]")],
  refresh: document.querySelector("#refresh"),
  filterRow: document.querySelector(".filter-row"),
  mobileFilter: document.querySelector("#mobile-filter-button"),
  connectionPill: document.querySelector("#connection-pill"),
  connectionLabel: document.querySelector("#connection-label"),
  metrics: {
    tracked: document.querySelector("#metric-tracked"),
    low: document.querySelector("#metric-low"),
    expiring: document.querySelector("#metric-expiring"),
    out: document.querySelector("#metric-out"),
    value: document.querySelector("#metric-value"),
  },
  dialog: document.querySelector("#item-dialog"),
  form: document.querySelector("#item-form"),
  formAlert: document.querySelector("#form-alert"),
  dialogTitle: document.querySelector("#dialog-title"),
  dialogEyebrow: document.querySelector("#dialog-eyebrow"),
  closeDialog: document.querySelector("#close-dialog"),
  cancelDialog: document.querySelector("#cancel-dialog"),
  deleteItem: document.querySelector("#delete-item"),
  saveItem: document.querySelector("#save-item"),
  name: document.querySelector("#item-name"),
  category: document.querySelector("#item-category"),
  quantity: document.querySelector("#item-quantity"),
  unit: document.querySelector("#item-unit"),
  price: document.querySelector("#item-price"),
  location: document.querySelector("#item-location"),
  threshold: document.querySelector("#item-threshold"),
  expiry: document.querySelector("#item-expiry"),
  shortLived: document.querySelector("#item-short-lived"),
  notes: document.querySelector("#item-notes"),
  chosenIcon: document.querySelector("#chosen-icon"),
  chosenIconSymbol: document.querySelector("#chosen-icon-symbol"),
  iconPicker: document.querySelector("#icon-picker"),
  iconSearch: document.querySelector("#icon-search"),
  iconGrid: document.querySelector("#icon-grid"),
  iconEmpty: document.querySelector("#icon-empty"),
  unitSuggestions: document.querySelector("#unit-suggestions"),
  locationSuggestions: document.querySelector("#location-suggestions"),
  confirmDialog: document.querySelector("#confirm-dialog"),
  confirmName: document.querySelector("#confirm-name"),
  cancelDelete: document.querySelector("#cancel-delete"),
  confirmDelete: document.querySelector("#confirm-delete"),
  toastRegion: document.querySelector("#toast-region"),
};

class APIError extends Error {
  constructor(message, status, detail = null) {
    super(message);
    this.name = "APIError";
    this.status = status;
    this.detail = detail;
  }
}

async function request(path, options = {}) {
  const headers = new Headers(options.headers || {});
  headers.set("Accept", "application/json");
  if (options.body && !(options.body instanceof FormData)) headers.set("Content-Type", "application/json");
  const response = await fetch(path, { ...options, headers });
  if (response.status === 204) return null;
  const contentType = response.headers.get("content-type") || "";
  const body = contentType.includes("json") ? await response.json() : null;
  if (!response.ok) {
    const detail = body?.error || null;
    throw new APIError(WubuZh.error(detail, response.status), response.status, detail);
  }
  return body;
}

async function initialize() {
  bindEvents();
  try {
    state.meta = await request("/api/v1/meta");
  } catch {
    state.meta = DEFAULT_META;
  }
  populateMetadata();
  renderIconPicker();
  await refreshAll();
}

function bindEvents() {
  elements.add.addEventListener("click", () => openEditor());
  elements.emptyAdd.addEventListener("click", () => openEditor());
  elements.closeDialog.addEventListener("click", closeEditor);
  elements.cancelDialog.addEventListener("click", closeEditor);
  elements.form.addEventListener("submit", saveEditor);
  elements.dialog.addEventListener("cancel", (event) => {
    if (elements.saveItem.disabled) event.preventDefault();
  });
  elements.deleteItem.addEventListener("click", openDeleteConfirmation);
  elements.cancelDelete.addEventListener("click", () => elements.confirmDialog.close());
  elements.confirmDelete.addEventListener("click", deleteCurrentItem);
  elements.chosenIcon.addEventListener("click", toggleIconPicker);
  elements.iconSearch.addEventListener("input", () => renderIconPicker(elements.iconSearch.value));
  elements.name.addEventListener("input", suggestIconFromName);
  elements.category.addEventListener("change", suggestIconFromCategory);
  elements.search.addEventListener("input", debounceSearch);
  elements.clearSearch.addEventListener("click", () => {
    elements.search.value = "";
    state.filters.search = "";
    elements.clearSearch.hidden = true;
    refreshItems();
    elements.search.focus();
  });
  elements.categoryFilter.addEventListener("change", () => {
    state.filters.category = elements.categoryFilter.value;
    refreshItems();
  });
  elements.locationFilter.addEventListener("change", () => {
    state.filters.location = elements.locationFilter.value;
    refreshItems();
  });
	  elements.showOutOfStock.addEventListener("change", () => {
		state.showOutOfStock = elements.showOutOfStock.checked;
		if (!state.showOutOfStock && state.filters.status === "out") setStatus("");
		else refreshItems();
	  });
  elements.statusTabs.forEach((tab) => tab.addEventListener("click", () => setStatus(tab.dataset.status)));
  elements.metricCards.forEach((card) => card.addEventListener("click", () => setStatus(card.dataset.summaryFilter)));
  elements.refresh.addEventListener("click", refreshAll);
  elements.retry.addEventListener("click", refreshAll);
  elements.clearFilters.addEventListener("click", clearAllFilters);
  elements.mobileFilter.addEventListener("click", () => elements.filterRow.classList.toggle("is-open"));
  elements.itemGrid.addEventListener("click", handleCardAction);
  document.addEventListener("keydown", handleKeyboardShortcut);
}

function populateMetadata() {
  for (const category of state.meta.categories) {
    elements.categoryFilter.append(new Option(WubuZh.category(category.value), category.value));
    elements.category.append(new Option(WubuZh.category(category.value), category.value));
  }
  for (const unit of new Set([...WubuZh.commonUnits, ...state.meta.suggested_units.map(WubuZh.unit)])) elements.unitSuggestions.append(new Option(unit));
  for (const location of state.meta.suggested_locations) elements.locationSuggestions.append(new Option(WubuZh.location(location)));
  rebuildLocationFilter(state.meta.suggested_locations);
}

async function refreshAll() {
  const sequence = ++state.requestSequence;
  setLoading(state.items.length === 0);
  try {
    const [list, summary] = await Promise.all([fetchItems(), request("/api/v1/summary")]);
    if (sequence !== state.requestSequence) return;
    applyList(list);
    renderSummary(summary);
    setConnection(true);
    elements.errorState.hidden = true;
  } catch (error) {
    if (sequence !== state.requestSequence) return;
    showLoadError(error);
  } finally {
    if (sequence === state.requestSequence) setLoading(false);
  }
}

async function refreshItems() {
  const sequence = ++state.requestSequence;
  try {
    const list = await fetchItems();
    if (sequence !== state.requestSequence) return;
    applyList(list);
    setConnection(true);
    elements.errorState.hidden = true;
  } catch (error) {
    if (sequence === state.requestSequence) showLoadError(error);
  }
}

async function refreshSummary() {
  try {
    renderSummary(await request("/api/v1/summary"));
  } catch {
    // Item operation already surfaced any useful error.
  }
}

function fetchItems() {
  const query = new URLSearchParams({ limit: "500" });
  for (const [key, value] of Object.entries(state.filters)) {
    if (value) query.set(key, value);
  }
	  if (!state.showOutOfStock && state.filters.status !== "out") query.set("hide_out", "true");
  return request(`/api/v1/items?${query}`);
}

function applyList(list) {
  state.items = list.items;
  state.total = list.total;
  renderItems();
  const customLocations = state.items.map((item) => item.location).filter(Boolean);
  rebuildLocationFilter([...state.meta.suggested_locations, ...customLocations]);
}

function renderItems() {
  elements.itemGrid.replaceChildren();
  const fragment = document.createDocumentFragment();
  for (const item of state.items) fragment.append(buildItemCard(item));
  elements.itemGrid.append(fragment);

  const filtered = hasActiveFilters();
  elements.emptyState.hidden = state.items.length !== 0;
  elements.clearFilters.hidden = !filtered;
  elements.emptyAdd.hidden = filtered;
  if (state.items.length === 0 && filtered) {
    elements.emptyTitle.textContent = "没有匹配的物品。";
    elements.emptyCopy.textContent = "请更换搜索内容或清除筛选条件。";
  } else {
    elements.emptyTitle.textContent = "从第一件物品开始。";
    elements.emptyCopy.textContent = "咖啡、肥皂、电池……把家中常备的物品记下来吧。";
  }

  const labels = { low: "需要补货的物品", out: "已用完的物品", expiring: "即将到期的物品", expired: "已过期的物品", short_lived: "需要尽快使用的物品" };
  elements.inventoryTitle.textContent = labels[state.filters.status] || (state.filters.search ? "搜索结果" : "全部物品");
  elements.resultCount.textContent = `${state.total} 件物品`;
}

function buildItemCard(item) {
  const card = elements.cardTemplate.content.firstElementChild.cloneNode(true);
  card.dataset.id = item.id;
  card.querySelector(".item-icon").textContent = item.icon;
  card.querySelector(".item-name").textContent = WubuZh.product(item);
  card.querySelector(".item-edit").setAttribute("aria-label", `编辑 ${WubuZh.product(item)}`);
  const shortLivedToggle = card.querySelector(".short-lived-toggle");
  shortLivedToggle.classList.toggle("is-active", item.short_lived);
  shortLivedToggle.setAttribute("aria-pressed", String(item.short_lived));
  shortLivedToggle.setAttribute("aria-label", item.short_lived ? `取消 ${WubuZh.product(item)} 的尽快使用计时` : `将 ${WubuZh.product(item)} 标记为尽快使用`);
  shortLivedToggle.title = item.short_lived ? "取消 7 天计时" : "7 天内使用";

  const category = WubuZh.category(item.category);
  card.querySelector(".item-meta").textContent = item.location ? `${category} · ${WubuZh.location(item.location)}` : category;
  card.querySelector(".item-notes").textContent = item.notes;
  card.querySelector(".stock-amount strong").textContent = formatQuantity(item.quantity);
  card.querySelector(".stock-amount span").textContent = WubuZh.unit(item.unit);

  const badges = card.querySelector(".item-badges");
  const status = itemStatus(item);
  if (item.short_lived) {
    addBadge(badges, shortLivedLabel(item), "calendar_month", "short-lived");
    if (item.quantity > 0) addActionBadge(badges, "已用完", "check", "consume");
  }
  if (status.expired) addBadge(badges, "已过期", "event", "expired");
  else if (status.expiring) addBadge(badges, expiryLabel(item.expiry_date), "event", "expiring");
  if (status.out) addBadge(badges, "已用完", "block", "out");
  else if (status.low) addBadge(badges, "需要补货", "low_priority", "low");
  if (badges.childElementCount === 0 && item.expiry_date) addBadge(badges, formatDate(item.expiry_date), "event", "neutral");
  if (item.unit_price_cents !== null) addBadge(badges, `每${WubuZh.unit(item.unit)} ${formatCurrency(item.unit_price_cents)}`, "shopping_bag", "neutral");

  const step = stepFor(item.unit);
  const minus = card.querySelector('[data-adjust="minus"]');
  const plus = card.querySelector('[data-adjust="plus"]');
  minus.disabled = item.quantity <= 0;
  minus.title = `减少 ${formatQuantity(Math.min(step, item.quantity))} ${WubuZh.unit(item.unit)}`;
  plus.title = `增加 ${formatQuantity(step)} ${WubuZh.unit(item.unit)}`;
  return card;
}

function addBadge(container, text, icon, kind) {
  const badge = document.createElement("span");
  badge.className = `badge badge-${kind}`;
  const symbol = document.createElement("span");
  symbol.className = "material-symbols";
  symbol.setAttribute("aria-hidden", "true");
  symbol.textContent = icon;
  badge.append(symbol, document.createTextNode(text));
  container.append(badge);
}

function addActionBadge(container, text, icon, action) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "badge badge-short-lived badge-action";
  button.dataset.action = action;
  const symbol = document.createElement("span");
  symbol.className = "material-symbols";
  symbol.setAttribute("aria-hidden", "true");
  symbol.textContent = icon;
  button.append(symbol, document.createTextNode(text));
  container.append(button);
}

function renderSummary(summary) {
  elements.metrics.tracked.textContent = summary.tracked;
  elements.metrics.low.textContent = summary.low_stock;
  elements.metrics.expiring.textContent = summary.expiring_soon;
  elements.metrics.out.textContent = summary.out_of_stock;
  elements.metrics.value.textContent = formatCurrency(summary.inventory_value_cents);
}

async function handleCardAction(event) {
  const card = event.target.closest(".item-card");
  if (!card) return;
  const item = state.items.find((entry) => entry.id === card.dataset.id);
  if (!item) return;
  if (event.target.closest('[data-action="toggle-short-lived"]')) {
    await updateItemLifecycle(item, { short_lived: !item.short_lived }, item.short_lived ? "已取消尽快使用计时。" : "已开始 7 天计时。", card);
    return;
  }
  if (event.target.closest('[data-action="consume"]')) {
    await updateItemLifecycle(item, { quantity: 0, clear_auto_consume_at: true }, `已将 ${WubuZh.product(item)} 标记为用完。`, card);
    return;
  }
  if (event.target.closest('[data-action="edit"]')) {
    openEditor(item);
    return;
  }
  const adjustment = event.target.closest("[data-adjust]");
  if (adjustment) {
    const step = stepFor(item.unit);
    const delta = adjustment.dataset.adjust === "plus" ? step : -Math.min(step, item.quantity);
    if (delta !== 0) await adjustQuantity(item, delta, card);
  }
}

async function updateItemLifecycle(item, payload, message, card) {
  card.classList.add("is-adjusting");
  card.querySelectorAll("button").forEach((button) => { button.disabled = true; });
  try {
    await request(`/api/v1/items/${encodeURIComponent(item.id)}`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    });
    showToast(message);
    await refreshAll();
  } catch (error) {
    showToast(WubuZh.error(error), true);
    renderItems();
  }
}

async function adjustQuantity(item, delta, card) {
  card.classList.add("is-adjusting");
  card.querySelectorAll(".stepper button").forEach((button) => { button.disabled = true; });
  try {
    const updated = await request(`/api/v1/items/${encodeURIComponent(item.id)}/adjust`, {
      method: "POST",
      body: JSON.stringify({ delta }),
    });
    state.items = state.items.map((entry) => entry.id === updated.id ? updated : entry);
    renderItems();
    refreshSummary();
  } catch (error) {
    showToast(WubuZh.error(error), true);
    renderItems();
  }
}

function openEditor(item = null) {
  clearFormErrors();
  state.editingId = item?.id || null;
  state.iconManuallyChosen = Boolean(item);
  elements.form.reset();
  elements.iconPicker.hidden = true;
  elements.chosenIcon.setAttribute("aria-expanded", "false");
  elements.iconSearch.value = "";

  elements.dialogTitle.textContent = item ? "编辑物品" : "添加物品";
  elements.dialogEyebrow.textContent = item ? "更新库存" : "新增库存物品";
  elements.deleteItem.hidden = !item;
  elements.name.value = WubuZh.product(item) || "";
  elements.category.value = item?.category || "pantry";
  elements.quantity.value = item?.quantity ?? 1;
  WubuZh.setEditable(elements.unit, item?.unit || "个", "unit");
  elements.price.value = item?.unit_price_cents === null || item?.unit_price_cents === undefined
    ? "" : (item.unit_price_cents / 100).toFixed(2);
  WubuZh.setEditable(elements.location, item?.location || "", "location");
  elements.threshold.value = item?.low_stock_threshold ?? "";
  elements.expiry.value = item?.expiry_date || "";
  elements.shortLived.checked = item?.short_lived || false;
  elements.notes.value = item?.notes || "";
  setSelectedIcon(item?.icon || categoryIcon(elements.category.value), false);
  renderIconPicker();

  elements.dialog.showModal();
  requestAnimationFrame(() => elements.name.focus());
}

function closeEditor() {
  if (!elements.saveItem.disabled) elements.dialog.close();
}

async function saveEditor(event) {
  event.preventDefault();
  clearFormErrors();
  if (!elements.form.reportValidity()) return;

  const thresholdBlank = elements.threshold.value === "";
  const expiryBlank = elements.expiry.value === "";
  const priceBlank = elements.price.value === "";
  const payload = {
    name: elements.name.value,
    category: elements.category.value,
    icon: state.selectedIcon,
    quantity: Number(elements.quantity.value),
    unit: WubuZh.originalValue(elements.unit),
    unit_price_cents: priceBlank ? null : Math.round(Number(elements.price.value) * 100),
    location: WubuZh.originalValue(elements.location),
    low_stock_threshold: thresholdBlank ? null : Number(elements.threshold.value),
    expiry_date: expiryBlank ? null : elements.expiry.value,
    short_lived: elements.shortLived.checked,
    notes: elements.notes.value,
  };
  if (state.editingId) {
    payload.clear_low_stock_threshold = thresholdBlank;
    payload.clear_expiry_date = expiryBlank;
    payload.clear_unit_price = priceBlank;
  }

  setSaving(true);
  try {
    const item = await request(state.editingId ? `/api/v1/items/${encodeURIComponent(state.editingId)}` : "/api/v1/items", {
      method: state.editingId ? "PATCH" : "POST",
      body: JSON.stringify(payload),
    });
    elements.dialog.close();
    showToast(state.editingId ? `已更新 ${WubuZh.product(item)}。` : `已添加 ${WubuZh.product(item)}。`);
    await refreshAll();
  } catch (error) {
    if (error.detail?.fields) showFieldErrors(error.detail.fields);
    elements.formAlert.textContent = WubuZh.error(error);
    elements.formAlert.hidden = false;
  } finally {
    setSaving(false);
  }
}

function openDeleteConfirmation() {
  const item = state.items.find((entry) => entry.id === state.editingId);
  if (!item) return;
  elements.confirmName.textContent = WubuZh.product(item);
  elements.confirmDialog.showModal();
}

async function deleteCurrentItem() {
  const item = state.items.find((entry) => entry.id === state.editingId);
  if (!item) return;
  elements.confirmDelete.disabled = true;
  try {
    await request(`/api/v1/items/${encodeURIComponent(item.id)}`, { method: "DELETE" });
    elements.confirmDialog.close();
    elements.dialog.close();
    showToast(`已删除 ${WubuZh.product(item)}。`);
    await refreshAll();
  } catch (error) {
    showToast(WubuZh.error(error), true);
  } finally {
    elements.confirmDelete.disabled = false;
  }
}

function toggleIconPicker() {
  elements.iconPicker.hidden = !elements.iconPicker.hidden;
  elements.chosenIcon.setAttribute("aria-expanded", String(!elements.iconPicker.hidden));
  if (!elements.iconPicker.hidden) requestAnimationFrame(() => elements.iconSearch.focus());
}

function renderIconPicker(search = "") {
  const query = search.trim().toLowerCase();
  const matches = ICON_NAMES.filter((name) => `${WubuZh.icons[name]} ${titleCase(name)} ${ICON_KEYWORDS[name] || ""}`.toLowerCase().includes(query));
  const fragment = document.createDocumentFragment();
  for (const name of matches) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `icon-option${name === state.selectedIcon ? " is-selected" : ""}`;
    button.title = WubuZh.icons[name];
    button.setAttribute("aria-label", WubuZh.icons[name]);
    const symbol = document.createElement("span");
    symbol.className = "material-symbols";
    symbol.setAttribute("aria-hidden", "true");
    symbol.textContent = name;
    button.append(symbol);
    button.addEventListener("click", () => {
      setSelectedIcon(name, true);
      renderIconPicker(elements.iconSearch.value);
      elements.iconPicker.hidden = true;
      elements.chosenIcon.setAttribute("aria-expanded", "false");
      elements.chosenIcon.focus();
    });
    fragment.append(button);
  }
  elements.iconGrid.replaceChildren(fragment);
  elements.iconEmpty.hidden = matches.length !== 0;
}

function setSelectedIcon(icon, manual) {
  state.selectedIcon = ICON_NAMES.includes(icon) ? icon : "inventory_2";
  if (manual) state.iconManuallyChosen = true;
  elements.chosenIconSymbol.textContent = state.selectedIcon;
}

function suggestIconFromName() {
  if (state.iconManuallyChosen) return;
  const name = elements.name.value.trim().toLowerCase();
  const match = AUTO_ICON_RULES.find(([pattern]) => pattern.test(name));
  const chineseMatch = ICON_NAMES.find(icon => name.includes(WubuZh.icons[icon]));
  setSelectedIcon(match?.[1] || chineseMatch || categoryIcon(elements.category.value), false);
}

function suggestIconFromCategory() {
  if (!state.iconManuallyChosen || !elements.name.value.trim()) {
    state.iconManuallyChosen = false;
    suggestIconFromName();
  }
}

function categoryIcon(category) {
  return state.meta.categories.find((entry) => entry.value === category)?.icon || "inventory_2";
}

function setSaving(saving) {
  elements.saveItem.disabled = saving;
  elements.cancelDialog.disabled = saving;
  elements.deleteItem.disabled = saving;
  elements.saveItem.classList.toggle("is-saving", saving);
}

function clearFormErrors() {
  elements.formAlert.hidden = true;
  elements.formAlert.textContent = "";
  document.querySelectorAll(".field.has-error").forEach((field) => field.classList.remove("has-error"));
  document.querySelectorAll(".field-error").forEach((error) => { error.textContent = ""; });
}

function showFieldErrors(fields) {
  for (const [name, message] of Object.entries(fields)) {
    const error = document.querySelector(`[data-error-for="${CSS.escape(name)}"]`);
    if (!error) continue;
    error.textContent = WubuZh.fieldMessage(name, message);
    error.closest(".field")?.classList.add("has-error");
  }
}

let searchTimer;
function debounceSearch() {
  elements.clearSearch.hidden = elements.search.value === "";
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    state.filters.search = elements.search.value.trim();
    refreshItems();
  }, 240);
}

function setStatus(status) {
  state.filters.status = status;
	  if (status === "out") {
		state.showOutOfStock = true;
		elements.showOutOfStock.checked = true;
	  }
  elements.statusTabs.forEach((tab) => tab.classList.toggle("is-active", tab.dataset.status === status));
  elements.metricCards.forEach((card) => card.classList.toggle("is-active", card.dataset.summaryFilter === status));
  refreshItems();
  document.querySelector("#inventory").scrollIntoView({ behavior: "smooth", block: "start" });
}

function clearAllFilters() {
  state.filters = { search: "", category: "", location: "", status: "" };
	  state.showOutOfStock = false;
	  elements.showOutOfStock.checked = false;
  elements.search.value = "";
  elements.clearSearch.hidden = true;
  elements.categoryFilter.value = "";
  elements.locationFilter.value = "";
  elements.statusTabs.forEach((tab) => tab.classList.toggle("is-active", tab.dataset.status === ""));
  elements.metricCards.forEach((card) => card.classList.toggle("is-active", card.dataset.summaryFilter === ""));
  refreshItems();
}

function rebuildLocationFilter(locations) {
  const current = elements.locationFilter.value || state.filters.location;
  const unique = [...new Set([...locations, current].filter(Boolean).map((value) => value.trim()))].sort((a, b) => a.localeCompare(b));
  elements.locationFilter.replaceChildren(new Option("全部位置", ""));
  for (const location of unique) elements.locationFilter.append(new Option(WubuZh.location(location), location));
  elements.locationFilter.value = current;
}

function handleKeyboardShortcut(event) {
  const target = event.target;
  const typing = target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement || target.isContentEditable;
  if (event.key === "/" && !typing && !elements.dialog.open) {
    event.preventDefault();
    elements.search.focus();
  } else if (event.key.toLowerCase() === "n" && !typing && !elements.dialog.open && !elements.confirmDialog.open) {
    event.preventDefault();
    openEditor();
  }
}

function showLoadError(error) {
  setConnection(false);
  setLoading(false);
  elements.errorState.hidden = false;
  elements.errorState.style.display = "flex";
  elements.errorCopy.textContent = WubuZh.error(error);
}

function setLoading(loading) {
  state.loading = loading;
  elements.loadingGrid.hidden = !loading;
  if (loading) {
    elements.itemGrid.replaceChildren();
    elements.emptyState.hidden = true;
    elements.errorState.hidden = true;
  }
}

function setConnection(online) {
  elements.connectionPill.classList.toggle("is-online", online);
  elements.connectionPill.classList.toggle("is-offline", !online);
  elements.connectionLabel.textContent = online ? "本地数据库已就绪" : "服务器暂不可用";
}

function showToast(message, isError = false) {
  const toast = document.createElement("div");
  toast.className = `toast${isError ? " is-error" : ""}`;
  const icon = document.createElement("span");
  icon.className = "material-symbols";
  icon.setAttribute("aria-hidden", "true");
  icon.textContent = isError ? "error" : "check";
  toast.append(icon, document.createTextNode(message));
  elements.toastRegion.append(toast);
  setTimeout(() => toast.remove(), 3600);
}

function hasActiveFilters() {
	return Object.values(state.filters).some(Boolean) || state.showOutOfStock;
}

function itemStatus(item) {
  const days = item.expiry_date ? daysFromToday(item.expiry_date) : null;
  return {
    out: item.quantity <= 0,
    low: item.low_stock_threshold !== null && item.quantity <= item.low_stock_threshold,
    expired: days !== null && days < 0,
    expiring: days !== null && days >= 0 && days <= 30,
  };
}

function daysFromToday(date) {
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const target = new Date(`${date}T00:00:00`);
  return Math.round((target - today) / 86_400_000);
}

function expiryLabel(date) {
  const days = daysFromToday(date);
  if (days === 0) return "今天到期";
  if (days === 1) return "明天到期";
  return `${days} 天后到期`;
}

function shortLivedLabel(item) {
  if (!item.auto_consume_at) return item.quantity <= 0 ? "短期物品 · 已用完" : "尽快使用";
  const days = Math.max(1, Math.ceil((new Date(item.auto_consume_at) - Date.now()) / 86_400_000));
  return days === 1 ? "明天自动归零" : `${days} 天后自动归零`;
}

function formatDate(date) {
  return new Intl.DateTimeFormat("zh-CN", { month: "short", day: "numeric", year: "numeric" }).format(new Date(`${date}T00:00:00`));
}

function formatQuantity(value) {
  return new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 3 }).format(value);
}

function formatCurrency(cents) {
  return new Intl.NumberFormat("zh-CN", { style: "currency", currency: "CNY" }).format((cents || 0) / 100);
}

function stepFor(unit) {
  const normalized = unit.trim().toLowerCase();
  if (["g", "ml", "克", "毫升"].includes(normalized)) return 100;
  if (["kg", "l", "千克", "升"].includes(normalized)) return 0.1;
  return 1;
}

function titleCase(value) {
  if (value === "wc") return "Toilet";
  return value.replaceAll("_", " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}

initialize();
