const PD = typeof pageData !== "undefined" ? pageData : window.pageData;
const API = `${PD.baseURL}/api/editor`;

const state = {
    active: false,
    schemas: [],
    schemaByType: {},
    config: null,
    pageIndex: -1,
    drag: null,
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
    setupStylingTrigger();

    const ready = await waitForPageReady();
    if (!ready) {
        toast("Page content is still loading, try again in a moment", "negative");
        return;
    }
    renderCanvas();
    guardCanvas();
}

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

function guardCanvas() {
    const content = document.getElementById("page-content");
    if (!content) return;
    new MutationObserver(() => {
        const columns = document.querySelector(".page-columns");
        if (state.active && columns && !columns.dataset.editorCanvas) renderCanvas();
    }).observe(content, { childList: true, subtree: true });
}

function exitEditor() {
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

async function apiPost(path, body) {
    const res = await fetch(API + path, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || "Request failed");
    return data;
}

const convertToYAML = async (value) => (await apiPost("/convert", { to: "yaml", value })).text;
const convertToValue = async (text) => (await apiPost("/convert", { to: "value", text })).value;

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

async function save(mutation) {
    if (!(await commit(mutation))) return false;
    state.config = await apiGet("/config");
    renderCanvas();
    return true;
}

async function commitAndNavigate(mutation, navigate) {
    const before = await serverGeneration();
    if (!(await commit(mutation))) return false;
    await waitForServerReload(before);
    await navigate();
    return true;
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
    dock.addEventListener("wheel", (e) => {
        if (!e.deltaY) return;
        e.preventDefault();
        dock.scrollLeft += e.deltaY;
    }, { passive: false });
    const sorted = [...state.schemas].sort((a, b) => a.label.localeCompare(b.label));
    for (const schema of sorted) {
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
    container.dataset.editorCanvas = "1";
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

    if (CONTAINER_TYPES.includes(w.type)) {
        card.classList.add("editor-container");
        const horizontal = w.type === "split-column";
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
        e.stopPropagation();
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

function positionMarker(element, marker, index, horizontal) {
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

    const group = div("editor-ui editor-page-tools");
    group.append(
        pageToolButton("editor-add-page", iconPlus, "Add page", "Create a new page", openLayoutModal),
        pageToolButton("editor-edit-page", iconCog, "Edit page", "Edit this page's name, icon and options", openEditPageModal),
        pageToolButton("editor-remove-page", iconTrash, "Remove page", "Delete this page", removeCurrentPage),
    );
    nav.append(group);
}

function pageToolButton(className, icon, label, title, onClick) {
    const btn = div(`editor-page-btn ${className}`);
    btn.title = title;
    btn.append(iconSpan(icon), div("editor-page-btn-label", label));
    btn.addEventListener("click", onClick);
    return btn;
}

function setupStylingTrigger() {
    const picker = document.querySelector(".header .theme-picker");
    if (!picker) return;
    picker.classList.add("editor-styling-trigger");
    picker.addEventListener("click", (e) => {
        if (!state.active) return;
        e.preventDefault();
        e.stopPropagation();
        openStylingModal();
    }, true);
}

function removeCurrentPage() {
    const page = state.config.pages[state.pageIndex];
    confirmAction(`Remove the page "${page.title}"? This cannot be undone.`, () => {
        commitAndNavigate({ op: "removePage", page: state.pageIndex }, () => (location.href = `${PD.baseURL}/`));
    });
}

function openEditPageModal() {
    const page = state.config.pages[state.pageIndex];
    if (!page) return;
    if (page.writable === false) {
        toast("This page's file is read only, it cannot be edited", "negative");
        return;
    }

    const opt = page.options || {};
    const widths = ["", "default", "wide", "slim"];
    const controls = [];
    const reg = (key, ctl) => {
        controls.push({ key, read: ctl.read });
        return ctl.wrapper;
    };

    const basic = div("editor-fields");
    basic.append(reg("name", textField("Name", opt["name"] || page.title, "Page name")));
    basic.append(reg("name-icon", iconField("Icon", opt["name-icon"], "e.g. mdi:home or /assets/icon.svg")));
    basic.append(reg("slug", textField("Slug (URL)", opt["slug"], "auto from name")));

    const layout = div("editor-fields");
    layout.append(reg("width", selectField("Width", opt["width"], widths)));
    layout.append(reg("key-bind", textField("Key bind", opt["key-bind"], "e.g. d h")));
    layout.append(reg("center-vertically", checkField("Center vertically", opt["center-vertically"])));

    const nav = div("editor-fields");
    nav.append(reg("hide-from-navigation", checkField("Hide from navigation", opt["hide-from-navigation"])));
    nav.append(reg("hide-desktop-navigation", checkField("Hide desktop navigation", opt["hide-desktop-navigation"])));
    nav.append(reg("show-mobile-header", checkField("Show mobile header", opt["show-mobile-header"])));
    nav.append(reg("desktop-navigation-width", selectField("Desktop navigation width", opt["desktop-navigation-width"], widths)));

    const sections = [
        sectionTitle("Basics"),
        basic,
        sectionTitle("Layout"),
        layout,
        collapsible("Navigation", nav),
    ];

    openModal("Page options", sections, () => {
        const fields = {};
        for (const c of controls) fields[c.key] = c.read();
        return savePageOptions(fields);
    });
}

async function savePageOptions(fields) {
    const before = await serverGeneration();
    if (!(await commit({ op: "editPage", page: state.pageIndex, fields }))) return false;
    await waitForServerReload(before);
    const cfg = await apiGet("/config").catch(() => null);
    const updated = cfg && cfg.pages[state.pageIndex];
    location.href = updated ? `${PD.baseURL}/${updated.slug}` : `${PD.baseURL}/`;
    return true;
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
        const name = title.value.trim() || "New Page";
        return commitAndNavigate({ op: "addPage", title: name, layout: chosen }, async () => {
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
    const initial = path.length >= 3 ? { frameless: "true" } : {};
    buildWidgetModal(type, initial, {}, (fields, rawFields) =>
        save({ op: "addWidget", page: state.pageIndex, path, widgetType: type, fields, rawFields })
    );
}

function openWidgetModal(path) {
    const widget = widgetAtPath(path);
    if (!widget) return;
    buildWidgetModal(widget.type, widget.values || {}, widget.structured || {}, (fields, rawFields) =>
        save({ op: "editWidget", page: state.pageIndex, path, fields, rawFields })
    );
}

function buildWidgetModal(type, values, structured, onSave) {
    const schema = state.schemaByType[type];
    if (!schema) return toast(`Unknown widget: ${type}`, "negative");

    const basic = div("editor-fields");
    const advanced = div("editor-fields");
    const controls = [];

    for (const field of schema.fields) {
        if (field.name === "widgets") continue;
        if (field.name === "autocomplete") continue;

        const control = field.name === "autocomplete-provider"
            ? renderAutocompleteProviderField(field, values["autocomplete-provider"], values["autocomplete"])
            : renderField(field, values[field.name], structured[field.name]);

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
            // An unchecked box is only written when the key is already in the config,
            // so unchecking persists as `false` without adding noise everywhere else.
            if (value === false && !(c.field.name in values)) continue;
            if (c.field.kind === "yaml" || (c.isYAML && c.isYAML())) rawFields[c.field.name] = value;
            else fields[c.field.name] = value;
        }
        return onSave(fields, rawFields);
    });
}

function renderField(field, value, structured) {
    if (field.kind === "list") return renderListField(field, structured, typeof value === "string" ? value : "");

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
        input.append(new Option("-", ""));
        for (const opt of field.options || []) input.append(new Option(opt, opt));
        input.value = value || "";
        read = () => input.value || undefined;
    } else if (field.kind === "yaml") {
        input = document.createElement("textarea");
        input.className = "editor-input editor-textarea";
        input.value = yamlSeed(value);
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

//
// List field (repeated entries)
//

const SUMMARY_KEYS = ["title", "name", "url", "repository", "symbol", "timezone", "shortcut", "label"];

function renderListField(field, structured, rawValue) {
    const entries = Array.isArray(structured) ? structured : [];
    let openIndex = -1;
    let dirty = false;
    let converting = false;
    // Entries we can't map onto the schema (a map such as $include, or shorthand
    // strings) can only be edited as YAML.
    const listable = (v) => Array.isArray(v) &&
        (!!field.itemKind || v.every((i) => i !== null && typeof i === "object" && !Array.isArray(i)));
    let yamlMode = !listable(structured) && (!!rawValue.trim() || Array.isArray(structured));

    // Entries are edited on a copy; the pristine one tells us which keys already
    // existed, so an explicit `false` survives while an untouched one stays out.
    const originals = new WeakMap();
    const adopt = (list) => list.map((i) => {
        if (i === null || typeof i !== "object") return i;
        const copy = { ...i };
        originals.set(copy, i);
        return copy;
    });
    let items = adopt(entries);

    const wrapper = div("editor-field");
    const labelRow = div("editor-list-label");
    const label = document.createElement("label");
    label.className = "editor-field-label";
    label.append(field.label);
    if (field.required) label.append(requiredStar());
    const toggle = button("", "editor-list-toggle");
    labelRow.append(label, toggle);

    const listEl = div("editor-list");
    const addBtn = button(`Add ${singularize(field.label)}`, "editor-list-add");
    addBtn.prepend(iconSpan(iconPlus));

    const textarea = document.createElement("textarea");
    textarea.className = "editor-input editor-textarea";
    textarea.value = rawValue;
    textarea.addEventListener("input", () => (dirty = true));

    const syncMode = () => {
        listEl.hidden = addBtn.hidden = yamlMode;
        textarea.hidden = !yamlMode;
        toggle.textContent = converting ? "Converting..." : yamlMode ? "Edit as list" : "Edit as YAML";
        toggle.disabled = converting;
    };

    toggle.addEventListener("click", async () => {
        if (converting) return;
        converting = true;
        syncMode();
        try {
            if (!yamlMode) {
                textarea.value = await convertToYAML(items);
                yamlMode = true;
            } else if (!textarea.value.trim()) {
                items = [];
                openIndex = -1;
                yamlMode = false;
            } else {
                const value = await convertToValue(textarea.value);
                if (!listable(value)) {
                    toast(`${field.label} can only be edited as YAML`, "negative");
                } else {
                    items = adopt(value);
                    openIndex = -1;
                    yamlMode = false;
                }
            }
        } catch (e) {
            toast(e.message, "negative");
        } finally {
            converting = false;
            render();
        }
    });

    const render = () => {
        listEl.textContent = "";
        items.forEach((item, i) => listEl.append(field.itemKind ? scalarRow(item, i) : itemCard(item, i)));
        syncMode();
    };

    const touch = () => {
        dirty = true;
        syncMode();
    };

    const moveItem = (from, to) => {
        if (from === to || to < 0 || to >= items.length) return;
        items.splice(to, 0, items.splice(from, 1)[0]);
        openIndex = -1;
        touch();
        render();
    };

    function removeButton(i) {
        const b = toolButton("Remove", iconTrash, () => {
            items.splice(i, 1);
            openIndex = -1;
            touch();
            render();
        });
        b.classList.add("editor-list-remove");
        return b;
    }

    function scalarRow(item, i) {
        const row = div("editor-list-row");
        const control = renderField({ ...field, kind: field.itemKind, label: "" }, item);
        control.wrapper.classList.add("editor-list-row-field");
        row.addEventListener("input", () => {
            items[i] = control.read();
            touch();
        });
        row.append(control.wrapper, removeButton(i));
        return row;
    }

    function itemCard(item, i) {
        const card = div("editor-list-card");
        const head = div("editor-list-head");
        const grip = div("editor-list-grip");
        grip.innerHTML = iconGrip;
        const thumb = img("editor-list-thumb", "");
        const summary = div("editor-list-summary");
        const chevron = div("editor-list-chevron");
        chevron.innerHTML = iconChevron;
        head.append(grip, thumb, summary, chevron, removeButton(i));

        const refreshHead = () => {
            summary.textContent = itemSummary(item, i);
            applyIcon(thumb, item.icon || "");
            thumb.style.display = item.icon ? "" : "none";
        };
        refreshHead();

        head.addEventListener("click", () => {
            openIndex = openIndex === i ? -1 : i;
            render();
        });
        card.append(head);
        setupItemDrag(card, grip, i);

        if (openIndex !== i) return card;

        card.classList.add("editor-list-card-open");
        const body = div("editor-list-body");
        const advanced = div("editor-fields");
        const controls = [];

        for (const f of field.item) {
            const control = renderField(f, item[f.name], item[f.name]);
            controls.push(control);
            (f.advanced ? advanced : body).append(control.wrapper);
        }
        if (advanced.children.length) body.append(collapsible("Advanced", advanced));

        const original = originals.get(item) || {};

        const sync = () => {
            for (const c of controls) {
                const value = c.read();
                if (c.field.kind === "list" && value === undefined) continue;
                const drop = value === undefined || value === "" ||
                    (value === false && !(c.field.name in original));
                if (drop) delete item[c.field.name];
                else item[c.field.name] = c.field.kind === "yaml" ? { $yaml: value } : value;
            }
            refreshHead();
            touch();
        };
        body.addEventListener("input", sync);
        body.addEventListener("change", sync);

        autofillTitleFromURL(controls);
        card.append(body);
        return card;
    }

    function setupItemDrag(card, grip, i) {
        grip.addEventListener("click", (e) => e.stopPropagation());
        grip.addEventListener("mousedown", () => (card.draggable = true));
        card.addEventListener("dragstart", (e) => {
            e.stopPropagation();
            e.dataTransfer.effectAllowed = "move";
            e.dataTransfer.setData("text/plain", "");
            card.classList.add("editor-list-dragging");
            state.listDrag = { list: listEl, index: i };
        });
        card.addEventListener("dragend", () => {
            card.draggable = false;
            card.classList.remove("editor-list-dragging");
            marker.remove();
            state.listDrag = null;
        });
    }

    // The whole list is the drop target so the pointer never leaves it between cards.
    const marker = div("editor-list-marker");
    const isOwnDrag = () => state.listDrag && state.listDrag.list === listEl;

    const listCards = () => [...listEl.children].filter((c) => c.classList.contains("editor-list-card"));

    const dropIndexAt = (y) => {
        const cards = listCards();
        for (let i = 0; i < cards.length; i++) {
            const box = cards[i].getBoundingClientRect();
            if (y < box.top + box.height / 2) return i;
        }
        return cards.length;
    };

    listEl.addEventListener("dragover", (e) => {
        if (!isOwnDrag()) return;
        e.preventDefault();
        e.stopPropagation();
        e.dataTransfer.dropEffect = "move";
        const before = listCards()[dropIndexAt(e.clientY)];
        before ? listEl.insertBefore(marker, before) : listEl.append(marker);
    });

    listEl.addEventListener("drop", (e) => {
        if (!isOwnDrag()) return;
        e.preventDefault();
        e.stopPropagation();
        const from = state.listDrag.index;
        const at = dropIndexAt(e.clientY);
        marker.remove();
        state.listDrag = null;
        moveItem(from, at > from ? at - 1 : at);
    });

    addBtn.addEventListener("click", () => {
        items.push(field.itemKind ? "" : {});
        openIndex = items.length - 1;
        touch();
        render();
    });

    wrapper.append(labelRow, listEl, addBtn, textarea);
    render();

    return {
        field,
        wrapper,
        isYAML: () => yamlMode,
        // Untouched fields are left out entirely so their YAML keeps its comments and formatting.
        read: () => {
            if (!dirty) return undefined;
            if (!yamlMode) return items;
            return textarea.value.trim() === rawValue.trim() ? undefined : textarea.value.trim() || undefined;
        },
    };
}

function autofillTitleFromURL(controls) {
    const titleInput = controls.find((c) => c.field.name === "title" || c.field.name === "name")?.wrapper.querySelector("input");
    const urlInput = controls.find((c) => c.field.name === "url")?.wrapper.querySelector("input");
    if (!titleInput || !urlInput) return;

    titleInput.addEventListener("input", () => (titleInput.dataset.touched = "1"));
    urlInput.addEventListener("input", () => {
        if (titleInput.dataset.touched || titleInput.value.trim()) return;
        titleInput.value = titleFromURL(urlInput.value);
    });
}

function titleFromURL(url) {
    const host = (url.match(/^(?:[a-z][a-z0-9+.-]*:\/\/)?([^/?#]+)/i) || [])[1] || "";
    const labels = host.split(":")[0].split(".").filter((l) => l && l !== "www");
    const name = labels[0] || "";
    return name ? name[0].toUpperCase() + name.slice(1) : "";
}

function itemSummary(item, i) {
    const key = SUMMARY_KEYS.find((k) => typeof item[k] === "string" && item[k].trim());
    return key ? item[key] : `Entry ${i + 1}`;
}

function singularize(label) {
    return label.replace(/ies$/, "y").replace(/s$/, "").toLowerCase();
}

function yamlSeed(value) {
    if (value === undefined || value === null) return "";
    if (typeof value === "string") return value;
    if (value.$yaml !== undefined) return value.$yaml;
    return JSON.stringify(value);
}

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
        preview.style.display = applyIcon(preview, input.value.trim()) ? "" : "none";
    };
    input.addEventListener("input", update);
    update();
    return preview;
}

// Sets src plus the same flat-icon class the widget templates use for auto-invert icons.
function applyIcon(el, value) {
    const { url, autoInvert } = resolveIcon(value);
    el.classList.toggle("flat-icon", autoInvert);
    el.onerror = /cdn\.jsdelivr\.net\/gh\/(selfhst\/icons|homarr-labs\/dashboard-icons)\/svg\//.test(url)
        ? () => { el.onerror = null; el.src = url.replace("/svg/", "/webp/").replace(/\.svg$/, ".webp"); }
        : null;
    el.src = url;
    return url;
}

function resolveIconURL(value) {
    return resolveIcon(value).url;
}

// Mirrors newCustomIconField in config-fields.go, keep both in sync.
function resolveIcon(value) {
    let autoInvert = false;
    if (!value) return { url: "", autoInvert };
    if (value.startsWith("auto-invert ")) {
        autoInvert = true;
        value = value.slice("auto-invert ".length);
    }

    const colon = value.indexOf(":");
    if (colon < 0) return { url: /^\/?assets\//.test(value) ? `${PD.baseURL}/${value.replace(/^\//, "")}` : value, autoInvert };
    const prefix = value.slice(0, colon);
    const icon = value.slice(colon + 1);

    const dot = icon.indexOf(".");
    const base = dot < 0 ? icon : icon.slice(0, dot);
    let ext = dot < 0 ? "svg" : icon.slice(dot + 1);
    if (ext !== "svg" && ext !== "png") ext = "svg";

    switch (prefix) {
        case "si": return { url: `https://cdn.jsdelivr.net/npm/simple-icons@latest/icons/${base}.svg`, autoInvert: true };
        case "di": return { url: `https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/${ext}/${base}.${ext}`, autoInvert };
        case "mdi": return { url: `https://cdn.jsdelivr.net/npm/@mdi/svg@latest/svg/${base}.svg`, autoInvert: true };
        case "sh": return { url: `https://cdn.jsdelivr.net/gh/selfhst/icons/${ext}/${base}.${ext}`, autoInvert };
        default: return { url: value, autoInvert };
    }
}

//
// Styling modal
//

function openStylingModal() {
    if (state.config && state.config.mainWritable === false) {
        toast("The main config file is read only, styling cannot be saved", "negative");
        return;
    }
    const branding = (state.config && state.config.branding) || {};
    const presets = (state.config && state.config.themePresets) || [];
    let selectedKey = "";
    let controls = [];

    const overlay = div("editor-ui editor-modal-overlay");
    const modal = div("editor-modal");

    const themeEntries = [{ key: "", values: (state.config && state.config.theme) || {} }, ...presets.map((p) => ({ key: p.key, values: p.values || {} }))];
    const picker = div("editor-theme-picker");
    const choices = div("theme-choices");
    const swatchButtons = [];
    for (const entry of themeEntries) {
        const btn = themeSwatch(entry.values);
        btn.title = entry.key === "" ? "Default" : entry.key;
        btn.addEventListener("click", () => {
            selectedKey = entry.key;
            highlightCurrent();
            renderBody(selectedKey);
            syncActions();
        });
        swatchButtons.push({ key: entry.key, btn });
        choices.append(btn);
    }
    const highlightCurrent = () => {
        for (const s of swatchButtons) s.btn.classList.toggle("current", s.key === selectedKey);
    };
    picker.append(choices);
    modal.append(picker);

    const bodyEl = div("editor-modal-body");
    modal.append(bodyEl);

    const themeValuesFor = (key) =>
        key === "" ? (state.config && state.config.theme) || {} : (presets.find((p) => p.key === key) || {}).values || {};

    function renderBody(key) {
        bodyEl.innerHTML = "";
        controls = [];
        const isDefault = key === "";
        const theme = themeValuesFor(key);

        const reg = (section, k, ctl) => {
            controls.push({ section, key: k, read: ctl.read });
            return ctl.wrapper;
        };

        const eff = isDefault
            ? {
                  "background-color": resolveCssColor("--color-background"),
                  "primary-color": resolveCssColor("--color-primary"),
                  "positive-color": resolveCssColor("--color-positive"),
                  "negative-color": resolveCssColor("--color-negative"),
              }
            : {};

        const colors = div("editor-fields");
        colors.append(reg("theme", "background-color", colorField("Background", theme["background-color"], eff["background-color"], "hsl")));
        colors.append(reg("theme", "primary-color", colorField("Primary", theme["primary-color"], eff["primary-color"], "hsl")));
        colors.append(reg("theme", "positive-color", colorField("Positive", theme["positive-color"], eff["positive-color"], "hsl")));
        colors.append(reg("theme", "negative-color", colorField("Negative", theme["negative-color"], eff["negative-color"], "hsl")));

        const options = div("editor-fields");
        options.append(reg("theme", "light", checkField("Light scheme", theme["light"])));
        options.append(reg("theme", "contrast-multiplier", numField("Contrast multiplier", theme["contrast-multiplier"], "e.g. 1.1")));
        options.append(reg("theme", "text-saturation-multiplier", numField("Text saturation multiplier", theme["text-saturation-multiplier"], "e.g. 1")));

        bodyEl.append(sectionTitle("Colors"), colors, sectionTitle("Theme options"), options);

        if (!isDefault) return;

        const logo = div("editor-fields");
        const logoURL = textField("Logo URL (overwrites logo)", branding["logo-url"], "/assets/logo.png");
        logo.append(reg("branding", "logo-url", logoURL));

        const favSync = document.createElement("input");
        favSync.type = "checkbox";
        logo.append(checkRow("Also use this image as the favicon", favSync));

        const faviconURL = textField("Favicon URL", branding["favicon-url"], "/assets/logo.png");
        logo.append(reg("branding", "favicon-url", faviconURL));

        const mirrorFavicon = () => {
            if (favSync.checked) faviconURL.input.value = logoURL.input.value.trim();
            faviconURL.input.disabled = favSync.checked;
        };
        if (branding["logo-url"] && branding["favicon-url"] === branding["logo-url"]) favSync.checked = true;
        favSync.addEventListener("change", mirrorFavicon);
        logoURL.input.addEventListener("input", mirrorFavicon);
        mirrorFavicon();

        logo.append(reg("branding", "logo-text", textField("Logo text", branding["logo-text"], "G")));
        logo.append(reg("branding", "hide-logo", checkField("Hide logo", branding["hide-logo"])));

        const app = div("editor-fields");
        app.append(reg("branding", "app-name", textField("App name", branding["app-name"], "Dynacat")));
        app.append(reg("branding", "app-icon-url", textField("App icon URL", branding["app-icon-url"], "/assets/app-icon.svg")));
        app.append(reg("branding", "app-background-color", colorField("App background color", branding["app-background-color"], resolveCssColor("--color-background"), "hex")));

        const misc = div("editor-fields");
        misc.append(reg("branding", "hide-footer", checkField("Hide footer", branding["hide-footer"])));
        misc.append(reg("branding", "custom-footer", areaField("Custom footer HTML", branding["custom-footer"])));
        misc.append(reg("branding", "show-desktop-navigation-on-hover", checkField("Show desktop navigation on hover", branding["show-desktop-navigation-on-hover"])));
        misc.append(reg("branding", "center-desktop-navigation", checkField("Center desktop navigation", branding["center-desktop-navigation"])));

        bodyEl.append(collapsible("Logo & branding", logo), collapsible("App / PWA", app), collapsible("Footer & navigation", misc));
    }

    const collect = () => {
        const themeOut = {};
        const brandingOut = {};
        for (const c of controls) (c.section === "theme" ? themeOut : brandingOut)[c.key] = c.read();
        return { themeOut, brandingOut };
    };

    const actions = div("editor-modal-actions");
    const cancel = button("Cancel", "editor-btn");
    const deleteBtn = button("", "editor-btn editor-btn-icon-danger");
    deleteBtn.append(iconSpan(iconTrash));
    deleteBtn.title = "Delete theme";
    const saveAsNew = button("Save as new", "editor-btn");
    const saveBtn = button("Save", "editor-btn editor-btn-primary");

    cancel.addEventListener("click", () => overlay.remove());

    saveBtn.addEventListener("click", async () => {
        const { themeOut, brandingOut } = collect();
        saveBtn.disabled = true;
        try {
            const ok = selectedKey === "" ? await saveStyling(themeOut, brandingOut) : await savePreset(selectedKey, themeOut);
            if (ok) overlay.remove();
        } finally {
            saveBtn.disabled = false;
        }
    });

    saveAsNew.addEventListener("click", () => {
        promptModal("Name for the new theme", "Theme name", async (name) => {
            if (presets.some((p) => p.key === name)) {
                toast(`A theme named "${name}" already exists`, "negative");
                return false;
            }
            const { themeOut } = collect();
            if (await savePreset(name, themeOut)) overlay.remove();
            return true;
        });
    });

    deleteBtn.addEventListener("click", () => {
        const key = selectedKey;
        const message = key === "" ? "Reset the default theme to the built-in default?" : `Delete the theme "${key}"?`;
        confirmAction(message, async () => {
            if (await deletePreset(key)) overlay.remove();
        });
    });

    const syncActions = () => {};

    actions.append(deleteBtn, cancel, saveAsNew, saveBtn);
    modal.append(actions);

    highlightCurrent();
    renderBody(selectedKey);
    syncActions();

    overlay.append(modal);
    let pressedOnBackdrop = false;
    overlay.addEventListener("mousedown", (e) => (pressedOnBackdrop = e.target === overlay));
    overlay.addEventListener("click", (e) => {
        if (e.target === overlay && pressedOnBackdrop) overlay.remove();
    });
    document.body.append(overlay);
}

function themeSwatch(values) {
    const btn = button("", "theme-preset");
    if (values["light"] === "true" || values["light"] === true) btn.classList.add("theme-preset-light");
    btn.style.setProperty("--color", colorToHex(values["background-color"]) || colorToHex("240 8 9"));

    const primary = colorToHex(values["primary-color"]) || colorToHex("280 70 60");
    const positive = colorToHex(values["positive-color"]) || (values["primary-color"] ? primary : colorToHex("280 70 60"));
    const negative = colorToHex(values["negative-color"]) || colorToHex("0 70 70");
    for (const c of [primary, positive, negative]) {
        const dot = div("theme-color");
        dot.style.setProperty("--color", c);
        btn.append(dot);
    }
    return btn;
}

async function saveStyling(theme, branding) {
    const before = await serverGeneration();
    if (!(await commit({ op: "editStyling", theme, branding }))) return false;
    await waitForServerReload(before);
    location.reload();
    return true;
}

async function savePreset(key, theme) {
    const before = await serverGeneration();
    if (!(await commit({ op: "editStyling", presetKey: key, theme }))) return false;
    await waitForServerReload(before);
    location.reload();
    return true;
}

async function deletePreset(key) {
    const before = await serverGeneration();
    if (!(await commit({ op: "deleteThemePreset", presetKey: key }))) return false;
    await waitForServerReload(before);
    location.reload();
    return true;
}

function sectionTitle(text) {
    return div("editor-section-title", text);
}

function colorField(label, explicit, effective, mode) {
    const normalize = (v) => (mode === "hex" ? colorToHex(v) || String(v || "").trim() : normalizeHsl(v));

    const wrapper = div("editor-field");
    const l = document.createElement("label");
    l.className = "editor-field-label";
    l.textContent = label;
    wrapper.append(l);

    const row = div("editor-color-row");
    const swatch = document.createElement("input");
    swatch.type = "color";
    swatch.className = "editor-color-swatch";
    const text = inputEl("text");
    text.classList.add("editor-color-text");
    text.placeholder = mode === "hex" ? "#151519" : "43 50 70";

    const shown = explicit || (mode === "hex" ? colorToHex(effective) : normalizeHsl(effective)) || "";
    if (shown) text.value = shown;
    swatch.value = colorToHex(shown) || "#000000";
    const initial = normalize(shown);

    swatch.addEventListener("input", () => {
        text.value = mode === "hex" ? swatch.value : hexToHslString(swatch.value);
    });
    text.addEventListener("input", () => {
        const hex = colorToHex(text.value);
        if (hex) swatch.value = hex;
    });

    row.append(swatch, text);
    wrapper.append(row);

    return {
        wrapper,
        input: text,
        read: () => {
            const t = text.value.trim();
            if (!t) return "";
            const out = normalize(t);
            if (explicit === undefined && out === initial) return "";
            return out;
        },
    };
}

function normalizeHsl(value) {
    const t = String(value == null ? "" : value).trim();
    if (!t) return "";
    if (!/^#|^rgb/i.test(t)) {
        const tr = parseHslTriple(t);
        if (tr) return `${Math.round(tr.h)} ${Math.round(tr.s)} ${Math.round(tr.l)}`;
    }
    return colorToHslString(t) || t;
}

function resolveCssColor(varName) {
    const probe = document.createElement("span");
    probe.style.cssText = `color: var(${varName}); display: none`;
    document.body.appendChild(probe);
    const rgb = getComputedStyle(probe).color;
    probe.remove();
    return rgb;
}

function checkField(label, value) {
    const input = document.createElement("input");
    input.type = "checkbox";
    input.checked = value === true || value === "true";
    return { wrapper: checkRow(label, input), input, read: () => input.checked };
}

function checkRow(label, input) {
    const row = document.createElement("label");
    row.className = "editor-field editor-check";
    const text = document.createElement("span");
    text.className = "editor-check-label";
    text.textContent = label;
    row.append(input, text);
    return row;
}

function textField(label, value, placeholder) {
    const input = inputEl("text");
    if (value) input.value = value;
    if (placeholder) input.placeholder = placeholder;
    return { wrapper: labeled(label, input), input, read: () => input.value.trim() };
}

function numField(label, value, placeholder) {
    const input = inputEl("number");
    input.step = "any";
    if (value) input.value = value;
    if (placeholder) input.placeholder = placeholder;
    return { wrapper: labeled(label, input), input, read: () => (input.value === "" ? "" : Number(input.value)) };
}

function areaField(label, value) {
    const input = document.createElement("textarea");
    input.className = "editor-input editor-textarea";
    if (value) input.value = value;
    return { wrapper: labeled(label, input), input, read: () => input.value.trim() };
}

function iconField(label, value, placeholder) {
    const input = inputEl("text");
    if (value) input.value = value;
    if (placeholder) input.placeholder = placeholder;
    const wrapper = labeled(label, input);
    wrapper.append(iconPreview(input));
    return { wrapper, input, read: () => input.value.trim() };
}

function selectField(label, value, options) {
    const input = document.createElement("select");
    input.className = "editor-input";
    for (const opt of options) input.append(new Option(opt === "" ? "-" : opt, opt));
    input.value = value || "";
    return { wrapper: labeled(label, input), input, read: () => input.value };
}

//
// Color conversions
//

function colorToHex(value) {
    const str = String(value == null ? "" : value).trim();
    if (!str) return "";

    let m = str.match(/^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/);
    if (m) {
        let h = m[1];
        if (h.length === 3) h = h.split("").map((c) => c + c).join("");
        return "#" + h.toLowerCase();
    }

    m = str.match(/^rgba?\(\s*(\d+)[\s,]+(\d+)[\s,]+(\d+)/i);
    if (m) return rgbToHex(+m[1], +m[2], +m[3]);

    const hsl = parseHslTriple(str);
    if (hsl) return hslToHex(hsl.h, hsl.s, hsl.l);

    return "";
}

function colorToHslString(value) {
    const hex = colorToHex(value);
    if (hex) return hexToHslString(hex);
    const t = parseHslTriple(value);
    return t ? `${Math.round(t.h)} ${Math.round(t.s)} ${Math.round(t.l)}` : "";
}

function parseHslTriple(value) {
    const m = String(value == null ? "" : value).trim()
        .match(/^(?:hsla?\()?\s*([\d.]+)[\s,]+([\d.]+)%?[\s,]+([\d.]+)%?\s*\)?$/);
    if (!m) return null;
    const h = +m[1], s = +m[2], l = +m[3];
    if (h > 360 || s > 100 || l > 100) return null;
    return { h, s, l };
}

function hexToRgb(hex) {
    const h = hex.replace("#", "");
    return { r: parseInt(h.slice(0, 2), 16), g: parseInt(h.slice(2, 4), 16), b: parseInt(h.slice(4, 6), 16) };
}

function rgbToHex(r, g, b) {
    const to = (n) => Math.max(0, Math.min(255, Math.round(n))).toString(16).padStart(2, "0");
    return "#" + to(r) + to(g) + to(b);
}

function hexToHslString(hex) {
    const { r, g, b } = hexToRgb(hex);
    const { h, s, l } = rgbToHsl(r, g, b);
    return `${h} ${s} ${l}`;
}

function rgbToHsl(r, g, b) {
    r /= 255; g /= 255; b /= 255;
    const max = Math.max(r, g, b), min = Math.min(r, g, b);
    let h = 0, s = 0;
    const l = (max + min) / 2;
    const d = max - min;
    if (d !== 0) {
        s = d / (1 - Math.abs(2 * l - 1));
        switch (max) {
            case r: h = ((g - b) / d) % 6; break;
            case g: h = (b - r) / d + 2; break;
            default: h = (r - g) / d + 4;
        }
        h *= 60;
        if (h < 0) h += 360;
    }
    return { h: Math.round(h), s: Math.round(s * 100), l: Math.round(l * 100) };
}

function hslToHex(h, s, l) {
    s /= 100; l /= 100;
    const c = (1 - Math.abs(2 * l - 1)) * s;
    const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
    const mm = l - c / 2;
    let r = 0, g = 0, b = 0;
    if (h < 60) { r = c; g = x; }
    else if (h < 120) { r = x; g = c; }
    else if (h < 180) { g = c; b = x; }
    else if (h < 240) { g = x; b = c; }
    else if (h < 300) { r = x; b = c; }
    else { r = c; b = x; }
    return rgbToHex((r + mm) * 255, (g + mm) * 255, (b + mm) * 255);
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
    saveBtn.addEventListener("click", async () => {
        saveBtn.disabled = true;
        try {
            if (await onSave()) overlay.remove();
        } finally {
            saveBtn.disabled = false;
        }
    });
    actions.append(cancel, saveBtn);
    modal.append(actions);

    overlay.append(modal);
    let pressedOnBackdrop = false;
    overlay.addEventListener("mousedown", (e) => (pressedOnBackdrop = e.target === overlay));
    overlay.addEventListener("click", (e) => {
        if (e.target === overlay && pressedOnBackdrop) overlay.remove();
    });
    document.body.append(overlay);
}

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

function promptModal(title, placeholder, onSubmit) {
    const overlay = div("editor-ui editor-modal-overlay");
    const modal = div("editor-modal editor-confirm");
    modal.append(div("editor-confirm-message", title));

    const input = inputEl("text");
    input.classList.add("editor-input");
    if (placeholder) input.placeholder = placeholder;
    modal.append(input);

    const actions = div("editor-modal-actions");
    const cancel = button("Cancel", "editor-btn");
    const okBtn = button("Save", "editor-btn editor-btn-primary");
    const submit = async () => {
        const val = input.value.trim();
        if (!val) return;
        okBtn.disabled = true;
        try {
            if ((await onSubmit(val)) !== false) overlay.remove();
        } finally {
            okBtn.disabled = false;
        }
    };
    cancel.addEventListener("click", () => overlay.remove());
    okBtn.addEventListener("click", submit);
    input.addEventListener("keydown", (e) => {
        if (e.key === "Enter") submit();
    });
    actions.append(cancel, okBtn);
    modal.append(actions);

    overlay.append(modal);
    overlay.addEventListener("click", (e) => {
        if (e.target === overlay) overlay.remove();
    });
    document.body.append(overlay);
    input.focus();
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
const iconCog = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.324.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 0 1 1.37.49l1.296 2.247a1.125 1.125 0 0 1-.26 1.431l-1.003.827c-.293.241-.438.613-.43.992a7.723 7.723 0 0 1 0 .255c-.008.378.137.75.43.991l1.004.827c.424.35.534.955.26 1.43l-1.298 2.247a1.125 1.125 0 0 1-1.369.491l-1.217-.456c-.355-.133-.751-.072-1.076.124a6.47 6.47 0 0 1-.22.128c-.331.183-.581.495-.644.869l-.213 1.281c-.09.543-.56.94-1.11.94h-2.594c-.55 0-1.019-.397-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 0 1-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 0 1-1.369-.49l-1.297-2.247a1.125 1.125 0 0 1 .26-1.431l1.004-.827c.292-.241.437-.613.43-.992a6.932 6.932 0 0 1 0-.255c.007-.378-.138-.75-.43-.991l-1.004-.827a1.125 1.125 0 0 1-.26-1.43l1.297-2.247a1.125 1.125 0 0 1 1.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.086.22-.128.332-.183.582-.495.644-.869l.214-1.281Z"/><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z"/></svg>`;
const iconGrip = `<svg viewBox="0 0 24 24" fill="currentColor"><circle cx="9" cy="6" r="1.6"/><circle cx="15" cy="6" r="1.6"/><circle cx="9" cy="12" r="1.6"/><circle cx="15" cy="12" r="1.6"/><circle cx="9" cy="18" r="1.6"/><circle cx="15" cy="18" r="1.6"/></svg>`;
const iconChevron = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="m9 5 7 7-7 7"/></svg>`;
const iconTrash = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0"/></svg>`;
