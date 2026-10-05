const MAX_FILE_BYTES = 100 * 1024 * 1024;
const MAX_BATCH_BYTES = 250 * 1024 * 1024;
const MAX_BATCH_FILES = 10;
const ALLOWED_TYPES = new Set([
  "image/jpeg", "image/png", "image/webp", "image/heic", "image/heif", "application/pdf",
  "audio/webm", "audio/ogg", "audio/mp4", "audio/mpeg", "audio/wav", "audio/x-wav",
  "video/mp4", "video/webm", "video/quicktime",
]);

const DEFAULT_META = {
  categories: [
    { value: "pantry", label: "Pantry & food" }, { value: "drinks", label: "饮品" },
    { value: "cleaning", label: "清洁用品" }, { value: "bathroom", label: "Bathroom" },
    { value: "laundry", label: "洗衣用品" }, { value: "household", label: "家居用品" },
    { value: "health", label: "Health" }, { value: "pets", label: "Pets" },
    { value: "baby", label: "Baby" }, { value: "other", label: "Other" },
  ],
};

const state = {
  entries: [],
  files: [],
  meta: DEFAULT_META,
  selectedBatch: new URLSearchParams(window.location.search).get("batch") || "",
  saving: false,
  loading: false,
  pollTimer: null,
};

const elements = {
  autoAnalyze: document.querySelector("#auto-analyze"),
  autoAnalyzeNote: document.querySelector("#auto-analyze-note"),
  captureForm: document.querySelector("#inbox-capture-form"),
  files: document.querySelector("#inbox-files"),
  uploadZone: document.querySelector("#inbox-upload-zone"),
  text: document.querySelector("#inbox-text"),
  fileList: document.querySelector("#inbox-capture-files"),
  captureStatus: document.querySelector("#inbox-capture-status"),
  captureSubmit: document.querySelector("#inbox-capture-submit"),
  error: document.querySelector("#inbox-error"),
  focus: document.querySelector("#inbox-focus"),
  processing: document.querySelector("#processing-list"),
  processingCount: document.querySelector("#processing-count"),
  processingEmpty: document.querySelector("#processing-empty"),
  ready: document.querySelector("#ready-list"),
  readyCount: document.querySelector("#ready-count"),
  readyEmpty: document.querySelector("#ready-empty"),
  review: document.querySelector("#review-list"),
  reviewCount: document.querySelector("#review-count"),
  reviewEmpty: document.querySelector("#review-empty"),
  applyReady: document.querySelector("#apply-ready"),
  analyzeSelected: document.querySelector("#analyze-selected"),
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
  await Promise.all([refreshSettings(), refreshInbox()]);
}

function bindEvents() {
  elements.autoAnalyze.addEventListener("change", saveSettings);
  elements.captureForm.addEventListener("submit", submitCapture);
  elements.files.addEventListener("change", () => {
    addFiles(elements.files.files);
    elements.files.value = "";
  });
  elements.uploadZone.addEventListener("dragover", handleDragOver);
  elements.uploadZone.addEventListener("dragleave", () => elements.uploadZone.classList.remove("is-dragging"));
  elements.uploadZone.addEventListener("drop", handleDrop);
  elements.fileList.addEventListener("click", removeFile);
  elements.applyReady.addEventListener("click", applyReady);
  elements.analyzeSelected.addEventListener("click", analyzeSelected);
  elements.review.addEventListener("click", handleReviewAction);
}

async function refreshSettings() {
  try {
    const settings = await request("/api/v1/intake/settings");
    elements.autoAnalyze.checked = settings.auto_analyze;
    elements.autoAnalyzeNote.textContent = "仅对新上传的内容生效。";
  } catch {
    elements.autoAnalyze.disabled = true;
    elements.autoAnalyzeNote.textContent = "收件箱设置暂不可用。";
  }
}

async function saveSettings() {
  const value = elements.autoAnalyze.checked;
  elements.autoAnalyze.disabled = true;
  try {
    await request("/api/v1/intake/settings", { method: "PATCH", body: JSON.stringify({ auto_analyze: value }) });
    elements.autoAnalyzeNote.textContent = value ? "新上传的内容将自动分析。" : "新上传的内容将暂存，等待手动分析。";
    showToast("已保存收件箱设置。");
  } catch (error) {
    elements.autoAnalyze.checked = !value;
    showError(errorMessage(error));
  } finally {
    elements.autoAnalyze.disabled = false;
  }
}

function addFiles(fileList) {
  if (state.saving) return;
  const candidate = [...state.files, ...fileList];
  if (candidate.length > MAX_BATCH_FILES) {
    showError(`每次最多选择 ${MAX_BATCH_FILES} 个文件。`);
    return;
  }
  let total = 0;
  for (const file of candidate) {
    if (!ALLOWED_TYPES.has(file.type)) {
      showError(`${file.name} 的文件格式不受支持。`);
      return;
    }
    if (file.size === 0 || file.size > MAX_FILE_BYTES) {
      showError(`${file.name} 的大小须在 1 字节至 100 MB 之间。`);
      return;
    }
    total += file.size;
  }
  if (total > MAX_BATCH_BYTES) {
    showError("每批文件总大小不能超过 250 MB。");
    return;
  }
  hideError();
  state.files = candidate;
  renderFiles();
  elements.captureStatus.textContent = `已选择 ${candidate.length} 个文件，共用一份说明。`;
}

function handleDragOver(event) {
  event.preventDefault();
  if (!state.saving) elements.uploadZone.classList.add("is-dragging");
}

function handleDrop(event) {
  event.preventDefault();
  elements.uploadZone.classList.remove("is-dragging");
  if (!state.saving && event.dataTransfer?.files) addFiles(event.dataTransfer.files);
}

function renderFiles() {
  elements.fileList.replaceChildren();
  state.files.forEach((file, index) => {
    const chip = document.createElement("span");
    chip.className = "inbox-file-chip";
    const icon = document.createElement("span");
    icon.className = "material-symbols";
    icon.textContent = sourceIconForFile(file);
    const label = document.createElement("span");
    label.className = "file-label";
    label.textContent = `${file.name} · ${formatFileSize(file.size)}`;
    const remove = document.createElement("button");
    remove.type = "button";
    remove.dataset.fileIndex = index;
    remove.setAttribute("aria-label", `移除 ${file.name}`);
    const removeIcon = document.createElement("span");
    removeIcon.className = "material-symbols";
    removeIcon.textContent = "close";
    remove.append(removeIcon);
    chip.append(icon, label, remove);
    elements.fileList.append(chip);
  });
}

function removeFile(event) {
  const button = event.target.closest("[data-file-index]");
  if (!button || state.saving) return;
  state.files.splice(Number(button.dataset.fileIndex), 1);
  renderFiles();
}

async function submitCapture(event) {
  event.preventDefault();
  const text = elements.text.value.trim();
  if (state.saving) return;
  if (!text && state.files.length === 0) {
    elements.text.focus();
    showError("请输入说明或选择至少一个文件。");
    return;
  }
  setCaptureBusy(true);
  hideError();
  try {
    const body = new FormData();
    body.append("text", text);
    state.files.forEach((file) => body.append("files", file, file.name));
    const batch = await request("/api/v1/intake/batches", { method: "POST", body });
    state.files = [];
    elements.text.value = "";
    renderFiles();
    selectBatch(batch.batch_id);
    elements.captureStatus.textContent = batch.analysis_scheduled
      ? `正在分析 ${batch.entries.length} 条录入内容。`
      : batch.auto_analyze
        ? "已保存，本地 AI 暂不可用。"
        : "已保存，自动分析已关闭。";
    showToast("已添加到 AI 收件箱。");
    await refreshInbox();
  } catch (error) {
    showError(errorMessage(error));
  } finally {
    setCaptureBusy(false);
  }
}

function setCaptureBusy(busy) {
  state.saving = busy;
  elements.files.disabled = busy;
  elements.text.disabled = busy;
  elements.captureSubmit.disabled = busy;
  elements.uploadZone.classList.toggle("is-disabled", busy);
}

function selectBatch(batchID) {
  state.selectedBatch = batchID;
  const url = new URL(window.location.href);
  url.searchParams.set("batch", batchID);
  window.history.replaceState({}, "", url);
}

async function refreshInbox() {
  clearTimeout(state.pollTimer);
  state.loading = true;
  try {
    const result = await request("/api/v1/intake?status=pending&limit=100");
    state.entries = result.entries;
    renderInbox();
    if (visibleEntries().some((entry) => entry.workflow_state === "queued" || entry.workflow_state === "analyzing")) {
      state.pollTimer = setTimeout(refreshInbox, 1600);
    }
  } catch (error) {
    showError(errorMessage(error));
  } finally {
    state.loading = false;
  }
}

function visibleEntries() {
  if (!state.selectedBatch) return state.entries;
  return state.entries.filter((entry) => entry.batch_id === state.selectedBatch);
}

function renderInbox() {
  const entries = visibleEntries();
  renderFocus(entries);
  const processing = entries.filter((entry) => entry.workflow_state === "queued" || entry.workflow_state === "analyzing");
  const ready = entries.filter((entry) => entry.workflow_state === "ready");
  const review = entries.filter((entry) => entry.workflow_state === "needs_review" || entry.workflow_state === "failed");
  renderSection(elements.processing, elements.processingCount, elements.processingEmpty, processing, renderProcessingEntry);
  renderSection(elements.ready, elements.readyCount, elements.readyEmpty, ready, renderReadyEntry);
  renderSection(elements.review, elements.reviewCount, elements.reviewEmpty, review, renderReviewEntry);

  elements.applyReady.hidden = ready.length === 0;
  elements.applyReady.textContent = `应用 ${ready.length} 条就绪变更`;
  const canAnalyze = entries.some((entry) => entry.workflow_state === "queued" || entry.workflow_state === "failed");
  elements.analyzeSelected.hidden = !canAnalyze;
  elements.analyzeSelected.textContent = state.selectedBatch ? "分析本批次" : "分析待处理内容";
}

function renderFocus(entries) {
  if (!state.selectedBatch) {
    elements.focus.hidden = true;
    return;
  }
  elements.focus.replaceChildren();
  const label = document.createTextNode(entries.length ? "正在查看本批次。 " : "本批次没有待处理内容。 ");
  const all = document.createElement("a");
  all.href = "/inbox";
  all.textContent = "查看全部收件箱";
  elements.focus.append(label, all);
  elements.focus.hidden = false;
}

function renderSection(container, count, empty, entries, renderer) {
  count.textContent = entries.length;
  container.replaceChildren(...entries.map(renderer));
  empty.hidden = entries.length > 0;
}

function renderProcessingEntry(entry) {
  return baseEntry(entry, { state: entry.workflow_state === "analyzing" ? "正在分析" : "等待分析", tone: "processing" });
}

function renderReadyEntry(entry) {
  const card = baseEntry(entry, { state: "已就绪", tone: "ready" });
  const summary = document.createElement("p");
  summary.className = "inbox-suggestion-summary";
  summary.textContent = suggestionSummary(entry.analysis?.suggestions || []);
  card.append(summary);
  return card;
}

function renderReviewEntry(entry) {
  const card = baseEntry(entry, { state: entry.workflow_state === "failed" ? "分析失败" : "待审核", tone: entry.workflow_state === "failed" ? "failed" : "review" });
  card.classList.add("review-card");
  const reason = document.createElement("p");
  reason.className = "review-reason";
  reason.textContent = (entry.failure_reason ? WubuZh.error(entry.failure_reason) : "") || (entry.workflow_state === "failed" ? "无法完成分析。" : "请核对并修正建议后再应用。");
  card.append(reason);

  if (entry.workflow_state === "failed" || !entry.analysis) {
    const actions = document.createElement("div");
    actions.className = "review-actions";
    const retry = button("重新分析", "button button-secondary", "retry-analysis");
    retry.dataset.entryId = entry.id;
    const discard = button("丢弃", "button button-secondary review-discard", "discard-entry");
    discard.dataset.entryId = entry.id;
    actions.append(retry, discard);
    card.append(actions);
    return card;
  }

  const editor = document.createElement("div");
  editor.className = "review-editor";
  editor.dataset.entryId = entry.id;
  const suggestions = entry.analysis.suggestions.length ? entry.analysis.suggestions : [emptySuggestion()];
  suggestions.forEach((suggestion) => editor.append(reviewRow(suggestion)));
  card.append(editor);
  const actions = document.createElement("div");
  actions.className = "review-actions";
  const add = button("添加变更", "button button-secondary review-add", "add-suggestion");
  add.dataset.entryId = entry.id;
  const actionGroup = document.createElement("div");
  actionGroup.className = "review-action-group";
  const discard = button("丢弃", "button button-secondary review-discard", "discard-entry");
  discard.dataset.entryId = entry.id;
  const apply = button("修正并应用", "button button-primary", "resolve-entry");
  apply.dataset.entryId = entry.id;
  actionGroup.append(discard, apply);
  actions.append(add, actionGroup);
  card.append(actions);
  return card;
}

function baseEntry(entry, status) {
  const card = document.createElement("article");
  card.className = "inbox-entry";
  card.dataset.entryId = entry.id;
  const icon = document.createElement("span");
  icon.className = "inbox-entry-icon material-symbols";
  icon.textContent = sourceIcon(entry.source);
  const copy = document.createElement("div");
  copy.className = "inbox-entry-copy";
  const titleRow = document.createElement("div");
  titleRow.className = "inbox-entry-title-row";
  const title = document.createElement("strong");
  title.textContent = entryTitle(entry);
  const source = document.createElement("span");
  source.className = "inbox-entry-source";
  source.textContent = sourceLabel(entry.source);
  titleRow.append(title, source);
  const detail = document.createElement("p");
  detail.textContent = entry.analysis?.summary || entry.text || "原文件已就绪，等待分析。";
  const meta = document.createElement("div");
  meta.className = "inbox-entry-meta";
  meta.textContent = formatDate(entry.created_at);
  copy.append(titleRow, detail, meta);
  const stateLabel = document.createElement("span");
  stateLabel.className = `inbox-entry-state ${status.tone}`;
  const stateIcon = document.createElement("span");
  stateIcon.className = "state-glyph";
  stateIcon.textContent = status.tone === "ready" ? "✓" : status.tone === "review" ? "!" : status.tone === "failed" ? "×" : "•";
  stateLabel.append(stateIcon, document.createTextNode(status.state));
  card.append(icon, copy, stateLabel);
  const preview = attachmentPreview(entry.attachments || []);
  if (preview) card.append(preview);
  return card;
}

function attachmentPreview(attachments) {
  if (!attachments.length) return null;
  const preview = document.createElement("div");
  preview.className = "inbox-entry-preview";
  attachments.forEach((attachment) => {
    if (attachment.media_type.startsWith("image/")) {
      const image = document.createElement("img");
      image.src = attachment.url;
      image.alt = attachment.file_name;
      preview.append(image);
    } else if (attachment.media_type.startsWith("audio/")) {
      const audio = document.createElement("audio");
      audio.controls = true;
      audio.src = attachment.url;
      preview.append(audio);
    } else if (attachment.media_type.startsWith("video/")) {
      const video = document.createElement("video");
      video.controls = true;
      video.preload = "metadata";
      video.src = attachment.url;
      preview.append(video);
    } else {
      const link = document.createElement("a");
      link.href = attachment.url;
      link.target = "_blank";
      link.rel = "noreferrer";
      link.append(document.createTextNode(`打开 ${attachment.file_name}`));
      preview.append(link);
    }
  });
  return preview;
}

function reviewRow(suggestion) {
  const row = document.createElement("div");
  row.className = "review-row";
  row.append(
    reviewSelect("操作", "intent", suggestion.intent || "increase", [["increase", "增加"], ["decrease", "减少"], ["create", "新增"]]),
    reviewInput("物品", "item_name", suggestion.item_name || "", "text"),
    reviewInput("数量", "amount", suggestion.amount || "", "number", "any"),
    reviewInput("单位", "unit", suggestion.unit || "个", "text"),
    reviewSelect("分类", "category", suggestion.category || "other", state.meta.categories.map((category) => [category.value, WubuZh.category(category.value)])),
  );
  const remove = button("", "review-remove", "remove-suggestion");
  remove.setAttribute("aria-label", "删除变更");
  remove.textContent = "×";
  row.append(remove);
  return row;
}

function reviewInput(label, field, value, type, step = "") {
  const wrapper = document.createElement("label");
  wrapper.className = "review-field";
  wrapper.append(document.createTextNode(label));
  const input = document.createElement("input");
  input.name = field;
  input.type = type;
  if (step) input.step = step;
  if (field === "unit") WubuZh.setEditable(input, value, "unit");
  else input.value = value;
  if (field === "amount") input.min = "0.001";
  wrapper.append(input);
  return wrapper;
}

function reviewSelect(label, field, value, options) {
  const wrapper = document.createElement("label");
  wrapper.className = "review-field";
  wrapper.append(document.createTextNode(label));
  const select = document.createElement("select");
  select.name = field;
  options.forEach(([optionValue, optionLabel]) => select.append(new Option(optionLabel, optionValue, false, optionValue === value)));
  wrapper.append(select);
  return wrapper;
}

async function handleReviewAction(event) {
  const buttonElement = event.target.closest("[data-action]");
  if (!buttonElement || buttonElement.disabled) return;
  const action = buttonElement.dataset.action;
  const entryID = buttonElement.dataset.entryId || buttonElement.closest(".review-editor")?.dataset.entryId;
  if (action === "remove-suggestion") {
    const row = buttonElement.closest(".review-row");
    const editor = buttonElement.closest(".review-editor");
    if (editor.querySelectorAll(".review-row").length === 1) {
      showError("请保留至少一条变更，或丢弃此条内容。");
      return;
    }
    row.remove();
    return;
  }
  if (action === "add-suggestion") {
    const editor = document.querySelector(`.review-editor[data-entry-id="${CSS.escape(entryID)}"]`);
    editor?.append(reviewRow(emptySuggestion()));
    return;
  }
  if (action === "retry-analysis") {
    await retryAnalysis(entryID);
    return;
  }
  if (action === "discard-entry") {
    await discardEntry(entryID);
    return;
  }
  if (action === "resolve-entry") await resolveEntry(entryID);
}

async function retryAnalysis(entryID) {
  try {
    await request(`/api/v1/intake/${encodeURIComponent(entryID)}/analysis`, { method: "POST" });
    showToast("已重新开始分析。");
    await refreshInbox();
  } catch (error) {
    showError(errorMessage(error));
  }
}

async function discardEntry(entryID) {
  const entry = state.entries.find((candidate) => candidate.id === entryID);
  if (!entry || !window.confirm("丢弃此条内容并删除原文件？")) return;
  try {
    if (entry.workflow_state === "needs_review") {
      await request(`/api/v1/intake/${encodeURIComponent(entryID)}/resolve`, { method: "POST", body: JSON.stringify({ action: "discard" }) });
    } else {
      await request(`/api/v1/intake/${encodeURIComponent(entryID)}`, { method: "PATCH", body: JSON.stringify({ status: "reviewed" }) });
    }
    showToast("已丢弃此条内容。");
    await refreshInbox();
  } catch (error) {
    showError(errorMessage(error));
  }
}

async function resolveEntry(entryID) {
  const editor = document.querySelector(`.review-editor[data-entry-id="${CSS.escape(entryID)}"]`);
  if (!editor) return;
  const suggestions = [...editor.querySelectorAll(".review-row")].map((row) => ({
    intent: row.querySelector('[name="intent"]').value,
    item_name: row.querySelector('[name="item_name"]').value.trim(),
    amount: Number(row.querySelector('[name="amount"]').value),
    unit: WubuZh.originalValue(row.querySelector('[name="unit"]')).trim(),
    category: row.querySelector('[name="category"]').value,
    confidence: 1,
  }));
  try {
    await request(`/api/v1/intake/${encodeURIComponent(entryID)}/resolve`, { method: "POST", body: JSON.stringify({ action: "apply", suggestions }) });
    showToast("已应用修正后的变更。");
    await refreshInbox();
  } catch (error) {
    showError(errorMessage(error));
  }
}

async function analyzeSelected() {
  try {
    const path = state.selectedBatch
      ? `/api/v1/intake/batches/${encodeURIComponent(state.selectedBatch)}/analyze`
      : "/api/v1/intake/analyze-all";
    await request(path, { method: "POST" });
    showToast("已开始分析。");
    await refreshInbox();
  } catch (error) {
    showError(errorMessage(error));
  }
}

async function applyReady() {
  const entries = visibleEntries().filter((entry) => entry.workflow_state === "ready");
  if (!entries.length || !window.confirm(`将 ${entries.length} 条就绪变更应用到库存？`)) return;
  try {
    const path = state.selectedBatch
      ? `/api/v1/intake/batches/${encodeURIComponent(state.selectedBatch)}/apply-ready`
      : "/api/v1/intake/apply-ready";
    const result = await request(path, { method: "POST" });
    const failed = result.failed?.length || 0;
    showToast(failed ? `已应用 ${result.added} 条变更，${failed} 条需要审核。` : `已应用 ${result.added} 条变更。`, failed > 0);
    await refreshInbox();
  } catch (error) {
    showError(errorMessage(error));
  }
}

function entryTitle(entry) {
  if (entry.attachments?.length) return entry.attachments.map((attachment) => attachment.file_name).join(", ");
  return entry.text.length > 72 ? `${entry.text.slice(0, 72)}…` : entry.text || "文字输入";
}

function sourceLabel(source) {
  return ({ command: "文字", receipt: "文档", purchase_photo: "图片", voice: "语音" })[source] || "录入内容";
}

function sourceIcon(source) {
  return source === "command" ? "inventory_2" : "photo_camera";
}

function sourceIconForFile(file) {
  return "photo_camera";
}

function suggestionSummary(suggestions) {
  if (!suggestions.length) return "未发现可应用的库存变更。";
  return suggestions.map((suggestion) => `${WubuZh.intent(suggestion.intent)} ${suggestion.amount} ${WubuZh.unit(suggestion.unit || "pcs")} ${suggestion.item_name}`).join(" · ");
}

function emptySuggestion() {
  return { intent: "increase", item_name: "", amount: "", unit: "个", category: "other", confidence: 1 };
}

function button(label, className, action) {
  const result = document.createElement("button");
  result.type = "button";
  result.className = className;
  result.dataset.action = action;
  result.textContent = label;
  return result;
}

function formatFileSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatDate(value) {
  return new Intl.DateTimeFormat("zh-CN", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(value));
}

function errorMessage(error) {
  const fields = error?.detail?.fields;
  if (fields && Object.keys(fields).length) return WubuZh.error(error.detail);
  return WubuZh.error(error);
}

function showError(message) {
  elements.error.textContent = message;
  elements.error.hidden = false;
}

function hideError() {
  elements.error.hidden = true;
  elements.error.textContent = "";
}

function showToast(message, isError = false) {
  const toast = document.createElement("div");
  toast.className = `toast${isError ? " is-error" : ""}`;
  toast.textContent = message;
  elements.toastRegion.append(toast);
  window.setTimeout(() => toast.remove(), 4200);
}

initialize();
