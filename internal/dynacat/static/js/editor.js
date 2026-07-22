// Interactive page editor: palette, drag and drop, option modals and layout tools.
// Loaded lazily on demand so it adds no overhead to normal page rendering.
//
// In edit mode the page columns are re-rendered from the config as simple icon+title
// cards (not live widgets), so editing never fights expanded widget content. Each save
// re-fetches the config and re-renders in place; the page only reloads when editing ends.

const PD = typeof pageData !== "undefined" ? pageData : window.pageData;
const API = `${PD.baseURL}/api/editor`;

const state = {
    active: false,
    schemas: [],
    schemaByType: {},
    config: null,
    pageIndex: -1,
    drag: null, // {type} for a palette item, {fromPath} for a placed card
};

export async function toggleEditor() {
    if (state.active) return exitEditor();
    await enterEditor();
}

async function enterEditor() {
    try {
        const [schemas, config] = await Promise.all([apiGet("/schema"), apiGet("/config")]);
        state.schemas = schemas;
        state.schemaByType = Object.fromEntries(schemas.map((s) => [s.type, s]));
        state.config = config;
        state.pageIndex = config.pages.findIndex((p) => p.slug === PD.slug);
    } catch (err) {
        toast(err.status === 401 ? "Log in to edit this page" : "Could not load editor", "negative");
        return;
    }

    if (state.pageIndex < 0) {
        toast("This page is not editable here", "negative");
        return;
    }

    state.active = true;
    sessionStorage.setItem("dynacat-editing", "1");
    document.body.classList.add("editing");
    document.getElementById("editor-toggle")?.classList.add("editor-toggle-active");

    buildPalette();
    buildAddPageButton();

    // page.js injects content only once the cache finishes building (marking #page
    // content-ready), and may rebuild it. Wait for ready, then guard against rebuilds.
    const ready = await waitForPageReady();
    if (!ready) {
        toast("Page content is still loading, try again in a moment", "negative");
        return;
    }
    renderCanvas();
    guardCanvas();
}

// waitForPageReady resolves once page.js marks the page content-ready, or null on timeout.
function waitForPageReady(timeout = 30000) {
    const page = document.getElementById("page");
    if (page && page.classList.contains("content-ready")) return Promise.resolve(page);

    return new Promise((resolve) => {
        const observer = new MutationObserver(() => {
            if (page && page.classList.contains("content-ready")) {
                observer.disconnect();
                resolve(page);
            }
        });
        observer.observe(page || document.body, { attributes: true, attributeFilter: ["class"], childList: !page, subtree: !page });
        setTimeout(() => {
            observer.disconnect();
            resolve(page && page.classList.contains("content-ready") ? page : null);
        }, timeout);
    });
}

// guardCanvas re-renders our cards if page.js later replaces the whole content.
function guardCanvas() {
    const content = document.getElementById("page-content");
    if (!content) return;
    new MutationObserver(() => {
        const columns = document.querySelector(".page-columns");
        if (state.active && columns && !columns.dataset.editorCanvas) renderCanvas();
    }).observe(content, { childList: true, subtree: true });
}

function exitEditor() {
    // Reload to restore the live widgets we replaced with editor cards.
    sessionStorage.removeItem("dynacat-editing");
    location.reload();
}

//
// API
//

async function apiGet(path) {
    const res = await fetch(API + path);
    if (!res.ok) throw { status: res.status };
    return res.json();
}

// commit posts a mutation; returns true on success, shows an error toast otherwise.
async function commit(mutation) {
    const res = await fetch(`${API}/config`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(mutation),
    });
    if (res.status === 204) return true;
    const body = await res.json().catch(() => ({}));
    toast(body.error || "Save failed", "negative");
    return false;
}

// save commits then re-fetches the config and re-renders the canvas in place.
async function save(mutation) {
    if (!(await commit(mutation))) return;
    state.config = await apiGet("/config");
    renderCanvas();
}

// commitAndNavigate commits a page-level change, waits for the server to hot-reload
// (so the navigation reflects it), then runs navigate().
async function commitAndNavigate(mutation, navigate) {
    const before = await serverGeneration();
    if (!(await commit(mutation))) return;
    await waitForServerReload(before);
    navigate();
}

async function serverGeneration() {
    try {
        return (await apiGet("/status")).generation;
    } catch {
        return 0;
    }
}

function waitForServerReload(before, timeout = 6000) {
    const started = Date.now();
    return new Promise((resolve) => {
        const tick = async () => {
            if ((await serverGeneration()) !== before || Date.now() - started > timeout) return resolve();
            setTimeout(tick, 300);
        };
        setTimeout(tick, 300);
    });
}

//
// Palette dock
//

function buildPalette() {
    const dock = div("editor-ui editor-palette");
    // Let the mouse wheel scroll the horizontal list instead of needing the scrollbar.
    dock.addEventListener("wheel", (e) => {
        if (!e.deltaY) return;
        e.preventDefault();
        dock.scrollLeft += e.deltaY;
    }, { passive: false });
    for (const schema of state.schemas) {
        if (schema.hidden) continue;
        const item = div("editor-palette-item");
        item.draggable = true;
        item.title = schema.label;
        if (schema.icon) item.append(maskIcon("editor-palette-icon", schema.icon));
        item.append(div("editor-palette-name", schema.label));
        item.addEventListener("dragstart", () => (state.drag = { type: schema.type }));
        dock.append(item);
    }
    document.body.append(dock);
}

//
// Canvas: columns and widget cards rendered from the config
//

// slim pages allow 2 columns, standard ones 3.
function maxColumnsFor(page) {
    return page.width === "slim" ? 2 : 3;
}

function renderCanvas() {
    const container = document.querySelector(".page-columns");
    if (!container) return;
    container.innerHTML = "";

    const page = state.config.pages[state.pageIndex];
    const canAddColumn = page.columns.length < maxColumnsFor(page);

    page.columns.forEach((col, colIndex) => {
        if (canAddColumn) container.append(addColumnLine(colIndex));
        const colEl = div(`page-column page-column-${col.size}`);
        setupDropTarget(colEl, [colIndex]);
        (col.widgets || []).forEach((w, i) => colEl.append(buildWidgetCard(w, [colIndex, i])));
        colEl.append(buildColumnTools(colIndex));
        container.append(colEl);
    });
    if (canAddColumn) container.append(addColumnLine(page.columns.length));
    container.dataset.editorCanvas = "1"; // marks this as our render so the guard skips it
}

function buildColumnTools(colIndex) {
    const tools = div("editor-ui editor-column-tools");
    const btn = button("Remove column", "editor-column-remove");
    btn.prepend(iconSpan(iconTrash));
    btn.addEventListener("click", () => removeColumn(colIndex));
    tools.append(btn);
    return tools;
}

function iconSpan(svg) {
    const span = document.createElement("span");
    span.className = "editor-icon-inline";
    span.innerHTML = svg;
    return span;
}

function removeColumn(colIndex) {
    confirmAction("Remove this column and its widgets?", () =>
        save({ op: "removeColumn", page: state.pageIndex, column: colIndex })
    );
}

const CONTAINER_TYPES = ["group", "split-column"];

function buildWidgetCard(w, path) {
    const schema = state.schemaByType[w.type];
    const card = div("widget editor-widget");
    card.draggable = true;
    card.addEventListener("dragstart", (e) => {
        e.stopPropagation();
        state.drag = { fromPath: path };
        card.classList.add("editor-dragging");
    });
    card.addEventListener("dragend", () => card.classList.remove("editor-dragging"));

    const header = div("editor-widget-header");
    const body = div("editor-widget-card");
    if (schema?.icon) body.append(maskIcon("editor-widget-icon", schema.icon));
    body.append(div("editor-widget-title", w.title || schema?.label || w.type));
    header.append(body);

    const tools = div("editor-ui editor-widget-tools");
    tools.append(toolButton("Edit", iconPencil, () => openWidgetModal(path)));
    tools.append(toolButton("Delete", iconTrash, () => removeWidget(path)));
    header.append(tools);
    card.append(header);

    // Container widgets get a nested drop zone showing their child cards.
    if (CONTAINER_TYPES.includes(w.type)) {
        card.classList.add("editor-container");
        const horizontal = w.type === "split-column"; // split lays children out in columns
        const nested = div(`editor-nested${horizontal ? " editor-nested-split" : ""}`);
        setupDropTarget(nested, path, horizontal);
        (w.widgets || []).forEach((child, j) => nested.append(buildWidgetCard(child, [...path, j])));
        card.append(nested);
    }

    return card;
}

function directWidgets(column) {
    return Array.from(column.children).filter((el) => el.classList.contains("widget"));
}

function setupDropTarget(element, basePath, horizontal = false) {
    const marker = div(`editor-ui editor-drop-marker${horizontal ? " editor-drop-marker-v" : ""}`);
    const pointer = (e) => (horizontal ? e.clientX : e.clientY);

    element.addEventListener("dragover", (e) => {
        e.preventDefault();
        e.stopPropagation(); // nested targets take precedence over the parent column
        positionMarker(element, marker, dropIndex(element, pointer(e), horizontal), horizontal);
    });
    element.addEventListener("dragleave", (e) => {
        if (!element.contains(e.relatedTarget)) marker.remove();
    });
    element.addEventListener("drop", (e) => {
        e.preventDefault();
        e.stopPropagation();
        const index = dropIndex(element, pointer(e), horizontal);
        marker.remove();
        handleDrop([...basePath, index]);
    });
}

// positionMarker places the insertion line as an absolute overlay so it never
// reflows the widgets (which would make the drop position jitter).
function positionMarker(element, marker, index, horizontal) {
    // Only one insertion line at a time (a parent's marker isn't cleared when the
    // pointer moves into a nested drop zone, since dragleave doesn't fire).
    document.querySelectorAll(".editor-drop-marker").forEach((m) => m !== marker && m.remove());

    const widgets = directWidgets(element);
    const edge = (el) => (horizontal ? el.offsetLeft : el.offsetTop);
    const size = (el) => (horizontal ? el.offsetWidth : el.offsetHeight);

    let pos;
    if (widgets.length === 0) {
        pos = 8;
    } else if (index < widgets.length) {
        pos = edge(widgets[index]) - 4;
    } else {
        const last = widgets[widgets.length - 1];
        pos = edge(last) + size(last) + 3;
    }

    if (horizontal) {
        marker.style.left = `${pos}px`;
    } else {
        marker.style.top = `${pos}px`;
    }
    if (!marker.isConnected) element.appendChild(marker);
}

// dropIndex returns the insertion position based on the pointer against card midpoints.
function dropIndex(element, pointer, horizontal) {
    const widgets = directWidgets(element);
    for (let i = 0; i < widgets.length; i++) {
        const box = widgets[i].getBoundingClientRect();
        const mid = horizontal ? box.left + box.width / 2 : box.top + box.height / 2;
        if (pointer < mid) return i;
    }
    return widgets.length;
}

function handleDrop(path) {
    const drag = state.drag;
    state.drag = null;
    if (!drag) return;

    if (drag.type) {
        openNewWidgetModal(drag.type, path);
    } else {
        save({ op: "moveWidget", page: state.pageIndex, path: drag.fromPath, toPath: path });
    }
}

function removeWidget(path) {
    save({ op: "removeWidget", page: state.pageIndex, path });
}

// widgetAtPath walks the config tree (through nested containers) to a widget.
function widgetAtPath(path) {
    const col = state.config.pages[state.pageIndex].columns[path[0]];
    let widgets = (col && col.widgets) || [];
    let w = null;
    for (let i = 1; i < path.length; i++) {
        w = widgets[path[i]];
        if (!w) return null;
        widgets = w.widgets || [];
    }
    return w;
}

//
// Inter-column add lines
//

function addColumnLine(insertIndex) {
    const line = div("editor-ui editor-add-column");
    line.title = "Add column here";
    const plus = div("editor-add-column-plus");
    plus.innerHTML = iconPlus;
    line.append(plus);
    line.addEventListener("click", () => addColumn(insertIndex));
    return line;
}

function addColumn(insertIndex) {
    const page = state.config.pages[state.pageIndex];
    const max = maxColumnsFor(page);
    if (page.columns.length >= max) {
        toast(`This layout is full (max ${max} columns)`, "negative");
        return;
    }
    // Keep the layout valid: at most 2 full columns, so fall back to small.
    const fullCount = page.columns.filter((c) => c.size === "full").length;
    const size = fullCount < 2 ? "full" : "small";
    save({ op: "addColumn", page: state.pageIndex, index: insertIndex, size });
}

//
// Add page
//

const PAGE_LAYOUTS = [
    ["full"],
    ["small", "full"],
    ["full", "small"],
    ["full", "full"],
    ["small", "full", "small"],
    ["full", "small", "full"],
];

function buildAddPageButton() {
    const nav = document.querySelector(".header .nav");
    if (!nav) return;
    const add = div("editor-ui editor-add-page nav-item", "Add +");
    add.addEventListener("click", openLayoutModal);
    nav.append(add);

    const remove = div("editor-ui editor-remove-page nav-item", "Remove page");
    remove.addEventListener("click", removeCurrentPage);
    nav.append(remove);
}

function removeCurrentPage() {
    const page = state.config.pages[state.pageIndex];
    confirmAction(`Remove the page "${page.title}"? This cannot be undone.`, () => {
        // Keep editing (flag stays set) and land on home once the server reloads.
        commitAndNavigate({ op: "removePage", page: state.pageIndex }, () => (location.href = `${PD.baseURL}/`));
    });
}

function openLayoutModal() {
    const body = div("editor-layout-grid");
    let chosen = PAGE_LAYOUTS[0];

    for (const layout of PAGE_LAYOUTS) {
        const option = div("editor-layout-option");
        for (const size of layout) option.append(div(`editor-layout-cell editor-layout-cell-${size}`));
        option.addEventListener("click", () => {
            body.querySelectorAll(".editor-layout-option").forEach((o) => o.classList.remove("selected"));
            option.classList.add("selected");
            chosen = layout;
        });
        body.append(option);
    }
    body.firstChild.classList.add("selected");

    const title = document.createElement("input");
    title.className = "editor-input";
    title.placeholder = "Page name";

    openModal("New page", [labeled("Name", title), body], () => {
        // Navigate onto the new page in edit mode once the server has picked it up.
        const name = title.value.trim() || "New Page";
        commitAndNavigate({ op: "addPage", title: name, layout: chosen }, async () => {
            const cfg = await apiGet("/config").catch(() => null);
            const created = cfg?.pages.find((p) => p.title === name);
            location.href = created ? `${PD.baseURL}/${created.slug}` : `${PD.baseURL}/`;
        });
    });
}

//
// Widget option modal
//

function openNewWidgetModal(type, path) {
    // Widgets dropped inside a container default to frameless (user can uncheck it).
    const initial = path.length >= 3 ? { frameless: "true" } : {};
    buildWidgetModal(type, initial, (fields, rawFields) =>
        save({ op: "addWidget", page: state.pageIndex, path, widgetType: type, fields, rawFields })
    );
}

function openWidgetModal(path) {
    const widget = widgetAtPath(path);
    if (!widget) return;
    buildWidgetModal(widget.type, widget.values || {}, (fields, rawFields) =>
        save({ op: "editWidget", page: state.pageIndex, path, fields, rawFields })
    );
}

function buildWidgetModal(type, values, onSave) {
    const schema = state.schemaByType[type];
    if (!schema) return toast(`Unknown widget: ${type}`, "negative");

    const basic = div("editor-fields");
    const advanced = div("editor-fields");
    const controls = [];

    for (const field of schema.fields) {
        if (field.name === "widgets") continue; // container children are edited via nesting
        if (field.name === "autocomplete") continue; // folded into autocomplete-provider below

        const control = field.name === "autocomplete-provider"
            ? renderAutocompleteProviderField(field, values["autocomplete-provider"], values["autocomplete"])
            : renderField(field, values[field.name]);

        controls.push(control);
        (field.advanced ? advanced : basic).append(control.wrapper);
    }

    const sections = [basic];
    if (advanced.children.length) sections.push(collapsible("Advanced", advanced));

    openModal(schema.label, sections, () => {
        const fields = {};
        const rawFields = {};
        for (const c of controls) {
            const value = c.read();
            if (c.extra) Object.assign(fields, c.extra());
            if (value === undefined) continue;
            if (c.field.kind === "yaml") rawFields[c.field.name] = value;
            else fields[c.field.name] = value;
        }
        onSave(fields, rawFields);
    });
}

// renderField builds a labeled input for a schema field and a reader for its value.
function renderField(field, value) {
    if (field.kind === "checkbox") {
        const input = document.createElement("input");
        input.type = "checkbox";
        input.checked = value === "true" || value === true;
        const row = document.createElement("label");
        row.className = "editor-field editor-check";
        const text = document.createElement("span");
        text.className = "editor-check-label";
        text.append(field.label);
        if (field.required) text.append(requiredStar());
        row.append(input, text);
        return { field, wrapper: row, read: () => input.checked };
    }

    let input;
    let read;

    if (field.kind === "select" && field.name === "search-engine") {
        return renderSearchEngineField(field, value);
    } else if (field.kind === "select") {
        input = document.createElement("select");
        input.className = "editor-input";
        input.append(new Option("-", "")); // allow leaving the field unset
        for (const opt of field.options || []) input.append(new Option(opt, opt));
        input.value = value || "";
        read = () => input.value || undefined;
    } else if (field.kind === "yaml") {
        input = document.createElement("textarea");
        input.className = "editor-input editor-textarea";
        input.value = value || "";
        read = () => input.value.trim() || undefined;
    } else if (field.kind === "number") {
        input = inputEl("number");
        if (value) input.value = value;
        read = () => (input.value === "" ? undefined : Number(input.value));
    } else {
        input = inputEl("text");
        if (value) input.value = value;
        if (field.kind === "duration") input.placeholder = "e.g. 30s, 5m, 1h";
        read = () => input.value.trim() || undefined;
    }

    const wrapper = labeled(field.label, input, field.required);
    if (field.kind === "icon") wrapper.append(iconPreview(input));

    return { field, wrapper, read };
}

// renderSearchEngineField adds a custom-URL input that shows only when "custom" is picked.
function renderSearchEngineField(field, value) {
    const known = (field.options || []).filter((o) => o !== "custom");
    const isCustom = value && !known.includes(value);

    const select = document.createElement("select");
    select.className = "editor-input";
    select.append(new Option("-", ""));
    for (const opt of field.options || []) select.append(new Option(opt, opt));
    select.value = isCustom ? "custom" : value || "";

    const customInput = inputEl("text");
    customInput.placeholder = "https://example.com/search?q={QUERY}";
    customInput.style.display = select.value === "custom" ? "" : "none";
    if (isCustom) customInput.value = value;
    select.addEventListener("change", () => {
        customInput.style.display = select.value === "custom" ? "" : "none";
    });

    const wrapper = labeled(field.label, select, field.required);
    wrapper.append(customInput);
    return { field, wrapper, read: () => (select.value === "custom" ? customInput.value.trim() || undefined : select.value || undefined) };
}

// renderAutocompleteProviderField is renderSearchEngineField plus the "autocomplete"
// bool: the "Off" option disables autocomplete rather than just clearing the provider.
function renderAutocompleteProviderField(field, value, autocompleteValue) {
    const known = (field.options || []).filter((o) => o !== "custom");
    const isOff = autocompleteValue === "false" || autocompleteValue === false;
    const isCustom = value && !known.includes(value);

    const select = document.createElement("select");
    select.className = "editor-input";
    select.append(new Option("Off", ""));
    for (const opt of known) select.append(new Option(opt, opt));
    select.append(new Option("custom", "custom"));
    select.value = isOff ? "" : (isCustom ? "custom" : value || known[0] || "");

    const customInput = inputEl("text");
    customInput.placeholder = "https://example.com/suggest?q={QUERY}";
    customInput.style.display = select.value === "custom" ? "" : "none";
    if (isCustom) customInput.value = value;
    select.addEventListener("change", () => {
        customInput.style.display = select.value === "custom" ? "" : "none";
    });

    const wrapper = labeled(field.label, select, field.required);
    wrapper.append(customInput);

    return {
        field,
        wrapper,
        read: () => (select.value === "" ? undefined : (select.value === "custom" ? customInput.value.trim() || undefined : select.value)),
        extra: () => ({ autocomplete: select.value !== "" }),
    };
}

function iconPreview(input) {
    const preview = img("editor-icon-preview", "");
    const update = () => {
        const url = resolveIconURL(input.value.trim());
        preview.src = url;
        preview.style.display = url ? "" : "none";
    };
    input.addEventListener("input", update);
    update();
    return preview;
}

// resolveIconURL mirrors the server's icon shorthand (si/di/mdi/sh) for a live preview.
function resolveIconURL(value) {
    if (!value) return "";
    value = value.replace(/^auto-invert /, "");
    const [prefix, name] = value.split(":");
    if (!name) return value;
    const base = name.split(".")[0];
    switch (prefix) {
        case "si": return `https://cdn.jsdelivr.net/npm/simple-icons@latest/icons/${base}.svg`;
        case "di": return `https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/svg/${base}.svg`;
        case "mdi": return `https://cdn.jsdelivr.net/npm/@mdi/svg@latest/svg/${base}.svg`;
        case "sh": return `https://cdn.jsdelivr.net/gh/selfhst/icons/svg/${base}.svg`;
        default: return value;
    }
}

//
// Generic modal
//

function openModal(title, sections, onSave) {
    const overlay = div("editor-ui editor-modal-overlay");
    const modal = div("editor-modal");
    modal.append(div("editor-modal-title", title));

    const bodyEl = div("editor-modal-body");
    for (const s of sections) bodyEl.append(s);
    modal.append(bodyEl);

    const actions = div("editor-modal-actions");
    const cancel = button("Cancel", "editor-btn");
    const saveBtn = button("Save", "editor-btn editor-btn-primary");
    cancel.addEventListener("click", () => overlay.remove());
    saveBtn.addEventListener("click", () => {
        overlay.remove();
        onSave();
    });
    actions.append(cancel, saveBtn);
    modal.append(actions);

    overlay.append(modal);
    // Only close on a real backdrop click, not when a text selection drag ends here.
    let pressedOnBackdrop = false;
    overlay.addEventListener("mousedown", (e) => (pressedOnBackdrop = e.target === overlay));
    overlay.addEventListener("click", (e) => {
        if (e.target === overlay && pressedOnBackdrop) overlay.remove();
    });
    document.body.append(overlay);
}

// confirmAction shows a custom confirmation, falling back to the native dialog on mobile.
function confirmAction(message, onConfirm) {
    if (window.matchMedia("(max-width: 768px)").matches) {
        if (confirm(message)) onConfirm();
        return;
    }

    const overlay = div("editor-ui editor-modal-overlay");
    const modal = div("editor-modal editor-confirm");
    modal.append(div("editor-confirm-message", message));

    const actions = div("editor-modal-actions");
    const cancel = button("Cancel", "editor-btn");
    const confirmBtn = button("Remove", "editor-btn editor-btn-danger");
    confirmBtn.prepend(iconSpan(iconTrash));
    cancel.addEventListener("click", () => overlay.remove());
    confirmBtn.addEventListener("click", () => {
        overlay.remove();
        onConfirm();
    });
    actions.append(cancel, confirmBtn);
    modal.append(actions);

    overlay.append(modal);
    overlay.addEventListener("click", (e) => {
        if (e.target === overlay) overlay.remove();
    });
    document.body.append(overlay);
}

function collapsible(title, content) {
    const details = document.createElement("details");
    details.className = "editor-collapsible";
    const summary = document.createElement("summary");
    summary.textContent = title;
    details.append(summary, content);
    return details;
}

//
// Toast
//

function toast(message, type) {
    let container = document.querySelector(".editor-toasts");
    if (!container) {
        container = div("editor-ui editor-toasts");
        document.body.append(container);
    }
    const el = div(`editor-toast color-${type}`, message);
    container.append(el);
    setTimeout(() => el.remove(), 4000);
}

//
// Small DOM helpers
//

function div(className, text) {
    const el = document.createElement("div");
    el.className = className;
    if (text) el.textContent = text;
    return el;
}

function img(className, src) {
    const el = document.createElement("img");
    el.className = className;
    if (src) el.src = src;
    return el;
}

// maskIcon renders a monochrome icon as a CSS mask so it tints to the theme color.
function maskIcon(className, url) {
    const el = div(className);
    el.style.setProperty("--icon-url", `url("${url}")`);
    return el;
}

function button(text, className) {
    const el = document.createElement("button");
    el.type = "button";
    el.className = className;
    el.textContent = text;
    return el;
}

function inputEl(type) {
    const el = document.createElement("input");
    el.type = type;
    el.className = "editor-input";
    return el;
}

function labeled(label, control, required) {
    const wrapper = div("editor-field");
    const l = document.createElement("label");
    l.className = "editor-field-label";
    l.append(label);
    if (required) l.append(requiredStar());
    wrapper.append(l, control);
    return wrapper;
}

function requiredStar() {
    const star = document.createElement("span");
    star.className = "editor-required";
    star.textContent = "*";
    return star;
}

function toolButton(title, svg, onClick) {
    const b = button("", "editor-tool");
    b.title = title;
    b.innerHTML = svg;
    b.addEventListener("click", (e) => {
        e.stopPropagation();
        onClick();
    });
    return b;
}

const iconPlus = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" d="M12 5v14M5 12h14"/></svg>`;
const iconPencil = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="m16.862 4.487 1.687-1.688a1.875 1.875 0 1 1 2.652 2.652L6.832 19.82a4.5 4.5 0 0 1-1.897 1.13l-2.685.8.8-2.685a4.5 4.5 0 0 1 1.13-1.897L16.863 4.487Z"/></svg>`;
const iconTrash = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0"/></svg>`;
