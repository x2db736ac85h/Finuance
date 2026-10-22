// Runs inside the legacy page (added with page.addScriptTag after boot). Uses only window.__legacy,
// the names the oracle injects into the legacy IIFE, and returns plain JSON-able objects.
(() => {
  const L = window.__legacy, S = L.S;

  const iso = t => {
    if (t == null || typeof t !== 'number' || !isFinite(t)) return null;
    const s = new Date(t).toISOString();
    return s.endsWith('T00:00:00.000Z') ? s.slice(0, 10) : s.replace('.000Z', 'Z');
  };
  const nums = a => Array.from(a, x => (isNaN(x) ? null : x));
  const bytes = b64 => { const bin = atob(b64), u = new Uint8Array(bin.length); for (let i = 0; i < bin.length; i++) u[i] = bin.charCodeAt(i); return u; };

  function entry(f, i){
    const e = {id: 'f' + (i + 1), name: f.name, size: 0, buf: bytes(f.b64).buffer, hash: 'oracle-' + i, added: i};
    if (f.cfg) e.cfg = f.cfg;
    L.parseEntry(e);
    return e;
  }
  function load(files){
    S.files = files.map(entry); S.cache = {}; S.views = {}; S.dupPick = {}; S.dupKeep = {};
    S.groups = []; S.tags = {};
  }
  const colJSON = c => ({name: c.name, type: c.type, numbers: nums(c.num), dates: Array.from(c.date, iso)});
  const findCol = (ds, n) => (n ? ds.cols.find(c => c.name === n) : null) || null;

  function ingest(f){
    const e = entry(f, 0);
    const o = {encoding: e.enc, delimiter: e.delim, error: e.error, headerAt: e.headerAt, dropped: e.dropped, headers: e.headers, rows: e.rows};
    if (!e.error){
      const ds = L.makeDataset('f1', f.name, e.headers, e.rows);
      Object.assign(o, {columns: ds.cols.map(colJSON), roles: ds.roles, rowNorm: ds.rowNorm});
    }
    if (f.cfg) o.cfg = f.cfg;
    return o;
  }

  function corpus(name, inputs){
    const fn = L[name];
    return inputs.map(x => {
      let out;
      try { out = fn(x); } catch (err){ return {in: x, error: String(err.message || err)}; }
      if (name === 'parseDate') out = iso(out);
      else if (name === 'looseDate') out = out ? out.map(iso) : null;
      else if (name === 'parseNum') out = isNaN(out) ? null : out;
      return {in: x, out};
    });
  }

  function dupJSON(){
    const ds = L.getDataset('merged'), d = ds.dup;
    const kept = ds.rows.map(r => JSON.stringify(r.slice(1))).sort().map(s => JSON.parse(s));   // drop the Grouping column
    const groups = [...d.groups.values()].map(g => ({
      key: g.k, both: g.both, date: iso(g.t),
      versions: g.idx.map(i => ({file: d.files[d.src[i]].name, fileIndex: d.src[i], kept: !!d.keep[i], row: d.rows[i]}))
    })).sort((a, b) => (a.key < b.key ? -1 : a.key > b.key ? 1 : 0));
    return {headers: ds.headers.slice(1), dupes: ds.dupes, rowCount: ds.rows.length, rows: kept, groups, dupPick: {...S.dupPick}, dupKeep: {...S.dupKeep}};
  }
  // default rule, then every group preferring its last file (dupPick), then every group kept twice (dupKeep)
  function dedupe(files, withOverrides){
    load(files); L.setActive('merged');
    const out = {default: dupJSON()};
    if (withOverrides){
      const d = L.getDataset('merged').dup;
      for (const g of d.groups.values()) S.dupPick[g.k] = d.files[d.src[g.idx[g.idx.length - 1]]].id;
      L.dupChanged(); out.preferLastFile = dupJSON();
      S.dupPick = {};
      for (const g of d.groups.values()) S.dupKeep[g.k] = 1;
      L.dupChanged(); out.keepBoth = dupJSON();
    }
    return out;
  }

  function txKeysOf(files){
    load(files);
    const ids = [...S.files.map(f => f.id), ...(S.files.length > 1 ? ['merged'] : [])];
    return ids.map(id => { const ds = L.getDataset(id); return {dataset: id === 'merged' ? 'merged' : ds.name, keys: L.txKeys(ds)}; });
  }

  function demoDataset(b64){ load([{name: 'demo.csv', b64}]); L.setActive('f1'); return L.getDataset('f1'); }

  function filters(b64, cases){
    const ds = demoDataset(b64);
    return cases.map(([column, cond]) => {
      const col = findCol(ds, column);
      if (!col) return {column, cond, error: 'no such column'};
      const fn = L.compileCond(cond, col), idx = [];
      if (fn) for (let i = 0; i < ds.rows.length; i++) if (fn(i)) idx.push(i);
      return {column, cond, type: col.type, compiled: !!fn, count: fn ? idx.length : null, first: idx.slice(0, 20)};
    });
  }

  function recurringAndPeriods(b64, presets){
    const ds = demoDataset(b64), R = ds.roles, col = n => findCol(ds, n);
    const dc = col(R.date), ac = col(R.amount), cc = col(R.category), pc = col(R.payee);
    const base = ds.rows.map((_, i) => i);
    let dmax = -Infinity; for (const t of dc.date) if (t > dmax) dmax = t;
    const rec = L.findRecurring(ds, dc, ac, cc, pc, base, dmax).map(r => ({...r, last: iso(r.last), next: iso(r.next)}));
    const periods = presets.map(([period, custom]) => {
      const v = L.view(); v.period = period; v.custom = custom ? {from: Date.parse(custom[0]), to: Date.parse(custom[1])} : null;
      const p = L.resolvePeriod();
      return {period, custom: custom || null, result: p ? {...p, from: iso(p.from), to: iso(p.to)} : null};
    });
    return {roles: R, recurring: rec, periods, roundNice: [0, -5, 1, 4, 5, 6, 99, 100, 101, 104, 105, 2849, 2855, 12345.6].map(x => ({in: x, out: L.roundNice(x)}))};
  }

  // fixed groups on the demo dataset: every 17th row → g1, category "Ogród" → g2
  function assignDemoGroups(ds){
    S.groups = [{id: 'g1', name: 'Wakacje nad morzem', color: 1, created: 0}, {id: 'g2', name: 'Remont', color: 2, created: 0}];
    const keys = L.txKeys(ds), cc = findCol(ds, ds.roles.category);
    ds.rows.forEach((r, i) => { if (i % 17 === 0) S.tags[keys[i]] = 'g1'; else if (r[cc.ci] === 'Ogród') S.tags[keys[i]] = 'g2'; });
    L.applyGroupsAll();
  }

  function assistant(b64, cases){
    const ds = demoDataset(b64);
    assignDemoGroups(ds);
    const D = L.aiData();
    const results = cases.map(([name, args]) => {
      const r = L.execTool(name, args, D);
      return {tool: name, args, result: r.result, summary: r.summary, sel: r.sel ? {id: r.sel.id, label: r.sel.label, ids: [...r.sel.ids].sort((a, b) => a - b)} : null};
    });
    const d = L.aiData();
    return {results, sysPrompt: L.sysPrompt(D), sysPromptNoData: L.sysPrompt(null), aiData: {id: d.id, scope: d.scope, cur: d.cur, min: iso(d.min), max: iso(d.max), rows: d.rows.length,
      columns: Object.fromEntries(['dc', 'ac', 'cc', 'pc', 'tc', 'kc', 'gc'].map(k => [k, d[k] ? d[k].name : null]))}};
  }

  function investments(b64, name, inv){
    load([{name, b64}]); S.inv = JSON.parse(JSON.stringify(inv)); L.setActive('f1');
    let el = document.getElementById('oracle-chart');
    if (!el){ el = document.createElement('div'); el.id = 'oracle-chart'; el.style.cssText = 'position:fixed;left:0;top:0;width:900px;height:420px;z-index:99999;background:#000'; document.body.appendChild(el); }
    const first = L.ovCtx(), lm = first.lm, prev = Date.UTC(new Date(lm).getUTCFullYear(), new Date(lm).getUTCMonth() - 1, 1);
    const out = [];
    for (const M of [lm, Math.max(prev, first.fm)]){   // latest month (partial) and the one before
      L.view().ovMonth = M;
      const ctx = L.ovCtx(), cards = {};
      for (const k of ['o_inv', 'o_yoc', 'o_fire']){ const c = L.drawChart(k, el, ctx, {big: false}); cards[k] = c.table; }
      out.push({month: iso(ctx.M), partial: !!ctx.partial, cards});
    }
    return out;
  }


  // Every pane chart on the demo dataset under each case's view and range settings: the table twin
  // (strings as the legacy formats them), the subtitle and, for the summary, the KPI captions.
  function charts(b64, cases){
    demoDataset(b64);
    let el = document.getElementById('oracle-chart');
    if (!el){ el = document.createElement('div'); el.id = 'oracle-chart'; el.style.cssText = 'position:fixed;left:0;top:0;width:900px;height:420px;z-index:99999;background:#000'; document.body.appendChild(el); }
    const defaults = {...S.cfg};
    return cases.map(cs => {
      const v = L.view();
      v.period = cs.period; v.custom = cs.custom ? {from: Date.parse(cs.custom[0]), to: Date.parse(cs.custom[1])} : null;
      v.search = cs.search || '';
      v.filters = Object.fromEntries(Object.entries(cs.filters || {}).map(([k, cond]) => [k, {cond, excluded: new Set()}]));
      S.cfg = {...defaults, flowMonths: 'all', ...cs.cfg};
      L.compute();
      const ctx = L.vizCtx(), out = {};
      for (const k of ['sum', 'cat', 'flow', 'pace', 'payee', 'heat']){
        const sub = document.createElement('span'), cx = document.createElement('span'), cfg = document.createElement('div');
        const c = L.drawChart(k, el, ctx, {sub, cx, cfg, big: true});
        out[k] = {sub: sub.textContent, table: c.table};
        if (k === 'sum') out[k].captions = [...el.querySelectorAll('.kpi .d')].map(d => d.textContent);
      }
      return {name: cs.name, period: S.period ? {...S.period, from: iso(S.period.from), to: iso(S.period.to)} : null, rows: S.order.length, charts: out};
    });
  }

  window.__oracle = {iso, ingest, corpus, dedupe, txKeysOf, filters, recurringAndPeriods, assistant, investments, charts, assignDemoGroups, load, entry, bytes};
})();
