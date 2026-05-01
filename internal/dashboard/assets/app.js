(function () {
  const payload = JSON.parse(document.getElementById("wirelog-payload").textContent);
  const state = {
    dashboard: payload.dashboard,
    dashboardID: payload.dashboard_id || "",
    dashboards: payload.dashboards || [],
    results: payload.results || [],
    variables: Object.assign({}, defaults(payload.dashboard), payload.variables || {}),
    timezone: initialTimezone(payload.dashboard),
    visibleCardIDs: new Set(),
    loadingCardIDs: new Set(),
    pendingCardIDs: new Set(),
    visibleTimers: new Map(),
    batchTimer: null,
    batchForce: false,
    observer: null,
    raw: "",
    etag: "",
  };

  const el = {
    title: document.getElementById("title"),
    sidebar: document.getElementById("sidebar"),
    vars: document.getElementById("vars"),
    sections: document.getElementById("sections"),
    status: document.getElementById("status"),
    refresh: document.getElementById("refresh"),
    edit: document.getElementById("edit"),
    drawer: document.getElementById("drawer"),
    raw: document.getElementById("raw-editor"),
    closeEditor: document.getElementById("close-editor"),
    saveEditor: document.getElementById("save-editor"),
    saveStatus: document.getElementById("save-status"),
    timezone: document.getElementById("timezone"),
  };

  boot();

  function boot() {
    el.edit.hidden = payload.mode !== "local";
    el.refresh.addEventListener("click", () => runVisible({force: true}));
    el.edit.addEventListener("click", openEditor);
    el.closeEditor.addEventListener("click", () => el.drawer.hidden = true);
    el.saveEditor.addEventListener("click", saveEditor);
    el.timezone.addEventListener("change", () => {
      state.timezone = el.timezone.value;
      localStorage.setItem("wirelog.dashboard.timezone", state.timezone);
      refreshCards(runnableCardIDs());
    });
    if (payload.mode === "local") {
      loadLocal(state.dashboardID);
    } else {
      render();
      if (payload.mode === "interactive") {
        setStatus("scroll to load charts");
      } else {
        setStatus("report loaded");
      }
    }
  }

  function defaults(dashboard) {
    const out = {};
    for (const [name, variable] of Object.entries((dashboard && dashboard.variables) || {})) {
      out[name] = variable.default;
    }
    return out;
  }

  async function loadLocal(id) {
    const url = id ? "/api/dashboard?id=" + encodeURIComponent(id) : "/api/dashboard";
    const res = await fetch(url, {
      headers: {"X-WireLog-Dashboard-Token": payload.session_token},
    });
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    state.dashboard = data.dashboard;
    state.dashboardID = data.dashboard_id;
    state.dashboards = data.dashboards || [];
    state.raw = data.raw;
    state.etag = data.etag;
    state.variables = defaults(state.dashboard);
    state.timezone = initialTimezone(state.dashboard);
    state.visibleCardIDs.clear();
    resetResults();
    render();
    setStatus("dashboard loaded");
  }

  function render() {
    el.title.textContent = state.dashboard.title || "WireLog Dashboard";
    renderSidebar();
    renderVariables();
    renderTimezone();
    renderSections();
  }

  function renderSidebar() {
    el.sidebar.innerHTML = "";
    if (payload.mode !== "local" || state.dashboards.length < 2) {
      el.sidebar.hidden = true;
      return;
    }
    el.sidebar.hidden = false;
    const title = document.createElement("div");
    title.className = "sidebar-title";
    title.textContent = "dashboards";
    el.sidebar.appendChild(title);
    for (const dashboard of state.dashboards) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "dashboard-link" + (dashboard.id === state.dashboardID ? " active" : "");
      button.textContent = dashboard.title || dashboard.id;
      button.addEventListener("click", async () => {
        if (dashboard.id === state.dashboardID) return;
        await loadLocal(dashboard.id);
      });
      el.sidebar.appendChild(button);
    }
  }

  function renderVariables() {
    el.vars.innerHTML = "";
    for (const [name, variable] of Object.entries(state.dashboard.variables || {})) {
      const wrap = document.createElement("div");
      wrap.className = "var";
      const label = document.createElement("label");
      label.textContent = variable.label || name;
      if (variable.type === "input") {
        const form = document.createElement("form");
        form.className = "input-var";
        const input = document.createElement("input");
        input.type = variable.input === "email" ? "text" : "text";
        input.placeholder = variable.placeholder || "";
        input.value = state.variables[name] || variable.default || "";
        input.disabled = payload.mode === "report";
        const submit = document.createElement("button");
        submit.type = "submit";
        submit.textContent = "submit";
        submit.disabled = payload.mode === "report";
        form.addEventListener("submit", event => {
          event.preventDefault();
          state.variables[name] = input.value.trim();
          state.visibleCardIDs.clear();
          resetResults();
          renderSections();
        });
        form.append(input, submit);
        wrap.append(label, form);
        el.vars.appendChild(wrap);
        continue;
      }
      const select = document.createElement("select");
      for (const opt of variable.options || []) {
        const option = document.createElement("option");
        option.value = opt.value;
        option.textContent = opt.label || opt.value;
        select.appendChild(option);
      }
      select.value = state.variables[name] || variable.default;
      select.addEventListener("change", () => {
        state.variables[name] = select.value;
        state.visibleCardIDs.clear();
        resetResults();
        renderSections();
      });
      if (payload.mode === "report") select.disabled = true;
      wrap.append(label, select);
      el.vars.appendChild(wrap);
    }
  }

  function renderTimezone() {
    const zones = timezoneOptions();
    el.timezone.innerHTML = "";
    if (!zones.includes(state.timezone)) zones.unshift(state.timezone);
    for (const zone of zones) {
      const option = document.createElement("option");
      option.value = zone;
      option.textContent = zone === browserTimezone() ? zone + " (local)" : zone;
      el.timezone.appendChild(option);
    }
    el.timezone.value = state.timezone;
  }

  function renderSections() {
    el.sections.innerHTML = "";
    for (const section of state.dashboard.sections || []) {
      const h = document.createElement("h2");
      h.textContent = section.title;
      el.sections.appendChild(h);
      const grid = document.createElement("div");
      grid.className = "section-grid";
      for (const card of section.cards || []) {
        grid.appendChild(renderCard(card));
      }
      el.sections.appendChild(grid);
    }
    observePanels();
  }

  function renderCard(card) {
    const panel = document.createElement("article");
    panel.className = "panel";
    panel.id = "card-" + card.id;
    panel.dataset.cardId = card.id;
    panel.style.setProperty("--w", String((card.layout && card.layout.w) || 12));
    panel.style.setProperty("--h", String((card.layout && card.layout.h) || 4));
    const title = document.createElement("h3");
    title.textContent = card.title;
    const body = document.createElement("div");
    body.className = "body";
    panel.append(title, body);
    renderCardBody(card, body);
    return panel;
  }

  function renderCardBody(card, body) {
    body.innerHTML = "";
    if (card.kind === "markdown") {
      const note = document.createElement("div");
      note.className = "note";
      note.textContent = card.markdown || "";
      body.appendChild(note);
      return;
    }
    const result = resultByID(card.id);
    if (!result) {
      body.appendChild(meta(dynamicMode() ? (state.visibleCardIDs.has(card.id) ? "loading" : "scroll to load") : "waiting"));
      return;
    }
    if (result.error) {
      body.appendChild(error(result.error));
      return;
    }
    const failed = (result.series || []).find(s => s.error);
    if (failed) {
      body.appendChild(error(failed.error));
      body.appendChild(renderDebug(card, result.series || []));
      return;
    }
    const series = result.series || [];
    if (card.viz === "table" || card.kind === "table" || card.viz === "event-stream") {
      body.appendChild(renderTable(firstResponse(series)));
    } else if (card.viz === "number" || card.kind === "metric") {
      body.appendChild(renderMetric(card, series));
    } else if (card.viz === "json") {
      const pre = document.createElement("pre");
      pre.textContent = JSON.stringify(series, null, 2);
      body.appendChild(pre);
    } else {
      const chart = document.createElement("div");
      chart.className = "chart";
      body.appendChild(chart);
      drawChart(chart, card, series);
    }
    body.appendChild(renderDebug(card, series));
  }

  function dynamicMode() {
    return payload.mode === "local" || payload.mode === "interactive";
  }

  function resetResults() {
    state.results = [];
    state.loadingCardIDs.clear();
    clearVisibleTimers();
    clearPendingBatch();
  }

  function runnableCardIDs() {
    const ids = [];
    for (const section of state.dashboard.sections || []) {
      for (const card of section.cards || []) {
        if (card.kind !== "markdown") ids.push(card.id);
      }
    }
    return ids;
  }

  function visibleRunnableCardIDs() {
    const allowed = new Set(runnableCardIDs());
    return [...state.visibleCardIDs].filter(id => allowed.has(id));
  }

  function cardByID(id) {
    for (const section of state.dashboard.sections || []) {
      for (const card of section.cards || []) {
        if (card.id === id) return card;
      }
    }
    return null;
  }

  function refreshCards(ids) {
    for (const id of ids) {
      const card = cardByID(id);
      const panel = document.getElementById("card-" + id);
      const body = panel && panel.querySelector(".body");
      if (card && body) renderCardBody(card, body);
    }
  }

  function clearVisibleTimers() {
    for (const timer of state.visibleTimers.values()) {
      clearTimeout(timer);
    }
    state.visibleTimers.clear();
  }

  function cancelVisibleTimer(id) {
    const timer = state.visibleTimers.get(id);
    if (!timer) return;
    clearTimeout(timer);
    state.visibleTimers.delete(id);
  }

  function scheduleVisibleLoad(id) {
    if (state.visibleTimers.has(id) || state.loadingCardIDs.has(id) || resultByID(id)) return;
    const timer = setTimeout(() => {
      state.visibleTimers.delete(id);
      if (state.visibleCardIDs.has(id)) {
        queueCards([id], {force: false});
      }
    }, 2000);
    state.visibleTimers.set(id, timer);
  }

  function observePanels() {
    if (!dynamicMode()) return;
    if (state.observer) {
      state.observer.disconnect();
      state.observer = null;
    }
    clearVisibleTimers();
    if (!("IntersectionObserver" in window)) {
      state.visibleCardIDs = new Set(runnableCardIDs());
      setTimeout(() => runVisible({force: false}), 2000);
      return;
    }
    state.observer = new IntersectionObserver(entries => {
      for (const entry of entries) {
        const id = entry.target.dataset.cardId;
        if (!id) continue;
        if (entry.isIntersecting) {
          state.visibleCardIDs.add(id);
          scheduleVisibleLoad(id);
        } else {
          state.visibleCardIDs.delete(id);
          cancelVisibleTimer(id);
        }
      }
    }, {rootMargin: "180px 0px"});
    for (const panel of el.sections.querySelectorAll(".panel")) {
      state.observer.observe(panel);
    }
  }

  async function runVisible(options) {
    const ids = visibleRunnableCardIDs();
    if (ids.length === 0) {
      setStatus("scroll to load charts");
      return;
    }
    queueCards(ids, options || {});
  }

  function queueCards(cardIDs, options) {
    for (const id of cardIDs || []) {
      state.pendingCardIDs.add(id);
    }
    state.batchForce = state.batchForce || !!(options && options.force);
    if (state.batchTimer) return;
    state.batchTimer = setTimeout(() => {
      const ids = [...state.pendingCardIDs];
      const force = state.batchForce;
      state.pendingCardIDs.clear();
      state.batchTimer = null;
      state.batchForce = false;
      runCards(ids, {force: force});
    }, 75);
  }

  function clearPendingBatch() {
    if (state.batchTimer) {
      clearTimeout(state.batchTimer);
      state.batchTimer = null;
    }
    state.pendingCardIDs.clear();
    state.batchForce = false;
  }

  async function runCards(cardIDs, options) {
    if (!state.dashboard) return;
    const missing = missingRequiredInput();
    if (missing) {
      setStatus(missing + " is required");
      return;
    }
    const force = !!(options && options.force);
    const runnable = new Set(runnableCardIDs());
    const ids = cardIDs.filter(id => {
      if (!runnable.has(id)) return false;
      if (state.loadingCardIDs.has(id)) return false;
      if (!force && resultByID(id)) return false;
      return true;
    });
    if (ids.length === 0) return;
    for (const id of ids) state.loadingCardIDs.add(id);
    refreshCards(ids);
    setStatus("querying " + ids.length + " card" + (ids.length === 1 ? "" : "s"));
    try {
      let results = [];
      if (payload.mode === "local") {
        const res = await fetch("/api/run", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-WireLog-Dashboard-Token": payload.session_token,
          },
          body: JSON.stringify({dashboard_id: state.dashboardID, variables: state.variables, card_ids: ids}),
        });
        if (!res.ok) throw new Error(await res.text());
        results = (await res.json()).results || [];
      } else if (payload.mode === "interactive") {
        results = await runInteractive(ids);
      }
      mergeResults(ids, results);
      setStatus("updated " + new Date().toLocaleTimeString());
    } catch (err) {
      setStatus(err.message || String(err));
    } finally {
      for (const id of ids) state.loadingCardIDs.delete(id);
      refreshCards(ids);
    }
  }

  function missingRequiredInput() {
    for (const [name, variable] of Object.entries(state.dashboard.variables || {})) {
      if (variable.type === "input" && variable.required && !String(state.variables[name] || "").trim()) {
        return variable.label || name;
      }
    }
    return "";
  }

  function mergeResults(ids, results) {
    const replace = new Set(ids);
    state.results = (state.results || []).filter(result => !replace.has(result.id)).concat(results || []);
  }

  async function runInteractive(cardIDs) {
    const wanted = new Set(cardIDs);
    const results = [];
    const cache = new Map();
    let rateLimited = null;
    for (const section of state.dashboard.sections || []) {
      for (const card of section.cards || []) {
        if (card.kind === "markdown") continue;
        if (!wanted.has(card.id)) continue;
        const result = {id: card.id, title: card.title, kind: card.kind, viz: card.viz, series: []};
        for (const q of cardQueries(card)) {
          const rendered = renderTemplate(q.query);
          let outcome = cache.get(rendered);
          if (!outcome) {
            if (rateLimited) {
              outcome = {error: rateLimited};
            } else {
              outcome = await fetchInteractiveQuery(rendered);
              if (outcome.status === 429) {
                rateLimited = outcome.error;
              }
            }
            cache.set(rendered, outcome);
          }
          result.series.push(Object.assign({name: q.name, query: rendered}, outcome));
        }
        results.push(result);
      }
    }
    return results;
  }

  async function fetchInteractiveQuery(query) {
    try {
      const res = await fetch(trimSlash(payload.host) + "/query", {
        method: "POST",
        headers: {"Content-Type": "application/json", "X-API-Key": payload.token},
        body: JSON.stringify({q: query, format: "json", limit: 1000}),
      });
      if (res.status === 429) {
        const retryAfter = res.headers.get("Retry-After") || "";
        const suffix = retryAfter ? " retry after " + retryAfter + "s" : "";
        return {status: 429, retry_after: retryAfter, error: "query rate limit exceeded;" + suffix};
      }
      if (!res.ok) return {status: res.status, error: await res.text()};
      return {response: await res.json()};
    } catch (err) {
      return {error: err.message || String(err)};
    }
  }

  function renderTemplate(query) {
    return query.replace(/\{\{\s*([A-Za-z_][A-Za-z0-9_]*)(?:\.([A-Za-z_][A-Za-z0-9_]*))?\s*\}\}/g, function (_, name, attr) {
      const variable = state.dashboard.variables[name];
      if (!variable) return "";
      const selected = state.variables[name] || variable.default;
      if (variable.type === "input") {
        if (variable.required && !(selected || "").trim()) throw new Error((variable.label || name) + " is required");
        if (!attr) return selected || "";
        if (attr.endsWith("_fragment")) {
          const fragmentName = attr.slice(0, -"_fragment".length);
          return inputFragment(variable, fragmentName, selected || "");
        }
        return "";
      }
      const opt = (variable.options || []).find(o => o.value === selected);
      if (!opt) return "";
      return attr === "fragment" ? (opt.fragment || "") : opt.value;
    }).replace(/\s+/g, " ").trim();
  }

  function inputFragment(variable, fragmentName, value) {
    const fragments = variable.fragments || {};
    const fragment = fragments[fragmentName];
    if (!fragment) return "";
    const parsed = parseEmailInput(value, !!variable.allow_domain_wildcard);
    if (!parsed) return "";
    if (parsed.kind === "exact") {
      return '| where ' + fragment.exact_field + ' = "' + escapeQueryString(parsed.value) + '"';
    }
    if (parsed.kind === "domain") {
      return '| where ' + fragment.domain_field + ' = "' + escapeQueryString(parsed.domain) + '"';
    }
    return "";
  }

  function parseEmailInput(value, allowDomain) {
    const text = String(value || "").trim().toLowerCase();
    if (!text) return {kind: "empty"};
    if (/^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$/.test(text)) {
      return {kind: "exact", value: text, domain: text.split("@")[1]};
    }
    if (allowDomain && /^\*@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$/.test(text)) {
      return {kind: "domain", value: text, domain: text.slice(2)};
    }
    return null;
  }

  function escapeQueryString(value) {
    return String(value).replace(/\\/g, "\\\\").replace(/"/g, '\\"');
  }

  function cardQueries(card) {
    if (card.query) return [{name: card.title, query: card.query}];
    return card.queries || [];
  }

  function resultByID(id) {
    return (state.results || []).find(r => r.id === id);
  }

  function firstResponse(series) {
    return (series[0] && series[0].response) || {columns: [], rows: []};
  }

  function optionString(card, name) {
    const value = card && card.options && card.options[name];
    return typeof value === "string" ? value : "";
  }

  function resolveColumn(resp, requested) {
    if (!requested) return "";
    const columns = resp.columns || [];
    return columns.find(col => col === requested)
      || columns.find(col => displayColumnName(col) === requested)
      || "";
  }

  function axisColumn(resp, preferred) {
    const columns = resp.columns || [];
    const selected = resolveColumn(resp, preferred);
    if (selected) return selected;
    return columns.find(col => !isMetricColumn(col)) || columns[0];
  }

  function valueColumn(resp, exclude, preferred) {
    const columns = resp.columns || [];
    const rows = resp.rows || [];
    const selected = resolveColumn(resp, preferred);
    if (selected && selected !== exclude && numericColumn(rows, selected)) return selected;
    const preferredMetrics = ["value", "count", "unique", "unique_count", "total", "sum", "average", "avg", "median_value", "min_value", "max_value", "p90", "p95", "p99", "users", "sessions"];
    for (const col of preferredMetrics) {
      if (col !== exclude && columns.includes(col) && numericColumn(rows, col)) return col;
    }
    return columns.find(col => col !== exclude && numericColumn(rows, col)) || columns.find(col => numericColumn(rows, col)) || columns[0];
  }

  function isMetricColumn(column) {
    return /^(m\d+|value|count|unique|unique_count|total|sum|average|avg|median_value|min_value|max_value|p90|p95|p99|users|sessions|avg_duration|avg_events)$/i.test(column || "");
  }

  function numericColumn(rows, column) {
    return rows.some(row => typeof row[column] === "number");
  }

  function renderTable(resp) {
    const table = document.createElement("table");
    const columns = resp.columns || [];
    const rows = resp.rows || [];
    const thead = document.createElement("thead");
    const tr = document.createElement("tr");
    for (const col of columns) {
      const th = document.createElement("th");
      th.textContent = displayColumnName(col);
      tr.appendChild(th);
    }
    thead.appendChild(tr);
    table.appendChild(thead);
    const tbody = document.createElement("tbody");
    for (const row of rows.slice(0, 200)) {
      const r = document.createElement("tr");
      for (const col of columns) {
        const td = document.createElement("td");
        td.textContent = formatValue(row[col], col);
        r.appendChild(td);
      }
      tbody.appendChild(r);
    }
    table.appendChild(tbody);
    return table;
  }

  function renderMetric(card, series) {
    const div = document.createElement("div");
    div.className = "metric";
    const resp = calculatedResponse(card, series) || firstResponse(series);
    const row = (resp.rows || [])[0] || {};
    const col = valueColumn(resp, "", optionString(card, "y"));
    div.textContent = formatValue(row[col], col);
    return div;
  }

  function drawChart(node, card, series) {
    if (!window.echarts) {
      node.appendChild(renderTable(firstResponse(series)));
      return;
    }
    requestAnimationFrame(() => {
      if (!node.isConnected) return;
      const existing = window.echarts.getInstanceByDom(node);
      if (existing) existing.dispose();
      const chart = window.echarts.init(node, null, {renderer: "svg"});
      chart.setOption(chartOption(card, series), true);
      requestAnimationFrame(() => chart.resize());
      window.addEventListener("resize", () => chart.resize(), {passive: true});
    });
  }

  function chartOption(card, series) {
    const effectiveSeries = calculatedSeries(card, series);
    if (card.viz === "pie") return pieOption(effectiveSeries, card);
    if (card.viz === "funnel") return funnelOption(effectiveSeries, card);
    const lineLike = card.viz === "line" || card.viz === "area";
    return {
      backgroundColor: "#000",
      color: ["#00ff88", "#ffffff", "#7a8a7a", "#ff6b6b"],
      tooltip: {trigger: "axis"},
      grid: {left: 12, right: 18, top: 18, bottom: 62, containLabel: true},
      xAxis: {
        type: "category",
        axisLabel: {color: "#7a8a7a", hideOverlap: true, margin: 12},
        axisLine: {lineStyle: {color: "#1d2a20"}},
      },
      yAxis: {
        type: "value",
        axisLabel: {color: "#7a8a7a", margin: 10},
        splitLine: {lineStyle: {color: "#1d2a20"}},
      },
      legend: {textStyle: {color: "#7a8a7a"}, bottom: 4, type: "scroll"},
      series: effectiveSeries.flatMap(s => responseToChartSeries(s, card, lineLike)),
    };
  }

  function calculatedSeries(card, series) {
    const resp = calculatedResponse(card, series);
    if (!resp) return series || [];
    return [{
      name: card.title || "value",
      query: (series || []).map(s => s.query).join("\n/\n"),
      response: resp,
    }];
  }

  function calculatedResponse(card, series) {
    if (optionString(card, "calculate") !== "ratio") return null;
    if (!series || series.length < 2) return null;
    const numerator = series[0].response;
    const denominator = series[1].response;
    if (!numerator || !denominator) return null;
    return ratioResponse(card, numerator, denominator);
  }

  function ratioResponse(card, numerator, denominator) {
    const xCol = ratioAxisColumn(card, numerator);
    const nCol = valueColumn(numerator, xCol || "", optionString(card, "numerator_y"));
    const dCol = valueColumn(denominator, ratioAxisColumn(card, denominator) || "", optionString(card, "denominator_y"));
    if (!xCol) {
      const n = Number(((numerator.rows || [])[0] || {})[nCol]);
      const d = Number(((denominator.rows || [])[0] || {})[dCol]);
      return {
        columns: ["value"],
        rows: [{value: d ? n / d : null}],
        mode: "formula",
      };
    }
    const denomX = ratioAxisColumn(card, denominator) || xCol;
    const denomByX = new Map();
    for (const row of denominator.rows || []) {
      denomByX.set(String(row[denomX]), Number(row[dCol]));
    }
    const rows = [];
    for (const row of numerator.rows || []) {
      const key = String(row[xCol]);
      const n = Number(row[nCol]);
      const d = denomByX.get(key);
      rows.push({[xCol]: row[xCol], value: d ? n / d : null});
    }
    return {
      columns: [xCol, "value"],
      rows: rows,
      mode: "formula",
    };
  }

  function ratioAxisColumn(card, resp) {
    const selected = resolveColumn(resp, optionString(card, "x"));
    if (selected) return selected;
    return (resp.columns || []).find(col => !isMetricColumn(col)) || "";
  }

  function responseToChartSeries(series, card, lineLike) {
    const resp = series.response || {columns: [], rows: []};
    const cols = resp.columns || [];
    const rows = resp.rows || [];
    if (cols.length === 0) return [];
    const xCol = axisColumn(resp, optionString(card, "x"));
    const yCol = valueColumn(resp, xCol, optionString(card, "y"));
    const seriesCol = resolveColumn(resp, optionString(card, "series"));
    const groupCols = seriesCol
      ? [seriesCol]
      : cols.filter(col => col !== xCol && col !== yCol && !isMetricColumn(col) && rows.some(row => row[col] !== null && row[col] !== undefined && row[col] !== ""));
    if (groupCols.length === 0) {
      return [rowsToChartSeries(series.name, rows, xCol, yCol, lineLike)];
    }
    const groups = new Map();
    for (const row of rows) {
      const key = groupCols.map(col => formatValue(row[col], col)).join(" / ");
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(row);
    }
    return [...groups.entries()].map(([key, groupRows]) => {
      const name = series.name && series.name !== key ? series.name + " · " + key : key;
      return rowsToChartSeries(name, groupRows, xCol, yCol, lineLike);
    });
  }

  function rowsToChartSeries(name, rows, xCol, yCol, lineLike) {
    const sorted = sortRowsForAxis(rows, xCol);
    return {
      name: name,
      type: lineLike ? "line" : "bar",
      areaStyle: lineLike ? {} : undefined,
      showSymbol: sorted.length <= 80,
      data: sorted.map(r => [formatAxisValue(r[xCol], xCol), Number(r[yCol]) || 0]),
    };
  }

  function pieOption(series, card) {
    const resp = calculatedResponse(card, series) || firstResponse(series);
    const cols = resp.columns || [];
    const rows = resp.rows || [];
    const nameCol = axisColumn(resp, optionString(card, "x") || optionString(card, "series"));
    const valueCol = valueColumn(resp, nameCol, optionString(card, "y"));
    return {
      backgroundColor: "#000",
      color: ["#00ff88", "#ffffff", "#7a8a7a", "#ff6b6b", "#1d2a20"],
      tooltip: {trigger: "item"},
      series: [{type: "pie", radius: "68%", data: rows.map(r => ({name: formatValue(r[nameCol], nameCol), value: Number(r[valueCol]) || 0}))}],
    };
  }

  function funnelOption(series, card) {
    const resp = firstResponse(series);
    const rows = resp.rows || [];
    const cols = resp.columns || [];
    const nameCol = axisColumn(resp, optionString(card, "x"));
    const valueCol = valueColumn(resp, nameCol, optionString(card, "y"));
    return {
      backgroundColor: "#000",
      color: ["#00ff88", "#ffffff", "#7a8a7a", "#ff6b6b"],
      tooltip: {trigger: "item"},
      series: [{type: "funnel", left: "8%", top: 10, bottom: 10, width: "84%", label: {color: "#fff"}, data: rows.map(r => ({name: formatValue(r[nameCol], nameCol), value: Number(r[valueCol]) || 0}))}],
    };
  }

  function renderDebug(card, series) {
    const details = document.createElement("details");
    details.className = "debug";
    const summary = document.createElement("summary");
    summary.textContent = "query";
    const resp = firstResponse(series);
    const pre = document.createElement("pre");
    pre.textContent = JSON.stringify({
      bindings: chartBindings(card, resp),
      columns: resp.columns || [],
      rows: (resp.rows || []).length,
      series: (series || []).map(s => ({name: s.name, query: s.query, error: s.error || ""})),
    }, null, 2);
    details.append(summary, pre);
    return details;
  }

  function chartBindings(card, resp) {
    if (!resp || !(resp.columns || []).length) return {};
    const x = axisColumn(resp, optionString(card, "x"));
    const y = valueColumn(resp, x, optionString(card, "y"));
    const series = resolveColumn(resp, optionString(card, "series"));
    return {
      x: x || "",
      y: y || "",
      series: series || "",
    };
  }

  function openEditor() {
    el.raw.value = state.raw;
    el.drawer.hidden = false;
  }

  async function saveEditor() {
    el.saveStatus.textContent = "saving";
    const res = await fetch("/api/dashboard?id=" + encodeURIComponent(state.dashboardID), {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
        "X-WireLog-Dashboard-Token": payload.session_token,
      },
      body: JSON.stringify({raw: el.raw.value, etag: state.etag}),
    });
    if (!res.ok) {
      el.saveStatus.textContent = await res.text();
      return;
    }
    const data = await res.json();
    state.etag = data.etag;
    state.raw = el.raw.value;
    await loadLocal(state.dashboardID);
    await runVisible({force: true});
    el.saveStatus.textContent = "saved";
  }

  function initialTimezone(dashboard) {
    return localStorage.getItem("wirelog.dashboard.timezone") || browserTimezone() || (dashboard && dashboard.timezone) || "UTC";
  }

  function browserTimezone() {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  }

  function timezoneOptions() {
    const local = browserTimezone();
    return [local, "UTC", "America/New_York", "America/Los_Angeles", "Europe/London", "Europe/Berlin", "Asia/Tokyo"].filter((zone, index, all) => all.indexOf(zone) === index);
  }

  function sortRowsForAxis(rows, column) {
    const copy = rows.slice();
    const type = axisValueType(copy.map(row => row[column]), column);
    if (type === "number") {
      return copy.sort((a, b) => Number(a[column]) - Number(b[column]));
    }
    if (type === "time") {
      return copy.sort((a, b) => timeValue(a[column]) - timeValue(b[column]));
    }
    return copy;
  }

  function axisValueType(values, column) {
    const present = values.filter(value => value !== null && value !== undefined && value !== "");
    if (present.length === 0) return "string";
    if (present.every(value => typeof value === "number" || (typeof value === "string" && value.trim() !== "" && Number.isFinite(Number(value))))) {
      return "number";
    }
    if (isTimeColumn(column) || present.every(value => isTimeLike(value))) {
      return "time";
    }
    return "string";
  }

  function formatAxisValue(value, column) {
    if (value === null || value === undefined) return "";
    if (isTimeColumn(column) || isTimeLike(value)) return formatTime(value, column);
    return String(value);
  }

  function displayColumnName(column) {
    const text = String(column || "");
    const eventProp = text.match(/^arrayElement\(event_properties, '([^']+)'\)$/);
    if (eventProp) return eventProp[1];
    const userProp = text.match(/^arrayElement\(user_properties, '([^']+)'\)$/);
    if (userProp) return "user_" + userProp[1];
    if (text.startsWith("event_properties_")) return text.slice("event_properties_".length);
    return text;
  }

  function formatValue(value, column) {
    if (value === null || value === undefined) return "";
    if (isTimeColumn(column) || isTimeLike(value)) return formatTime(value, column);
    if (typeof value === "number") return new Intl.NumberFormat().format(value);
    return String(value);
  }

  function isTimeColumn(column) {
    return /(^|_)(time|date|day|hour|week|month|seen|created|updated|started|ended)(_|$)/i.test(column || "");
  }

  function isTimeLike(value) {
    return typeof value === "string" && /^\d{4}-\d{2}-\d{2}(?:[ T]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?)?$/.test(value);
  }

  function timeValue(value) {
    const date = parseTime(value);
    return date ? date.getTime() : 0;
  }

  function parseTime(value) {
    if (!isTimeLike(value)) return null;
    const text = String(value);
    const dateOnly = /^\d{4}-\d{2}-\d{2}$/.test(text);
    const parsed = Date.parse(dateOnly ? text + "T00:00:00" : text);
    if (Number.isNaN(parsed)) return null;
    return new Date(parsed);
  }

  function formatTime(value, column) {
    const date = parseTime(value);
    if (!date) return String(value);
    const text = String(value);
    const dateOnly = /^\d{4}-\d{2}-\d{2}$/.test(text);
    const hourBucket = /(^|_)hour(_|$)/i.test(column || "");
    const opts = dateOnly && !hourBucket
      ? {month: "short", day: "numeric", timeZone: state.timezone}
      : {month: "short", day: "numeric", hour: "numeric", minute: "2-digit", timeZone: state.timezone};
    return new Intl.DateTimeFormat(undefined, opts).format(date);
  }

  function meta(text) {
    const div = document.createElement("div");
    div.className = "meta";
    div.textContent = text;
    return div;
  }

  function error(text) {
    const div = document.createElement("div");
    div.className = "error";
    div.textContent = text;
    return div;
  }

  function setStatus(text) {
    el.status.textContent = text;
  }

  function trimSlash(s) {
    return (s || "").replace(/\/+$/, "");
  }
})();
