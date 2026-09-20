"use strict";


const PHASE = { S: "Setup", B: "Birdsong", D: "Daylight", E: "Evening" };

// Autumn map: clearing positions (percent) and the 18 printed paths.
const POS = {
  C1: [13, 16], C2: [87, 16], C3: [87, 84], C4: [13, 84],
  C5: [50, 10], C6: [90, 48], C7: [57, 82], C8: [29, 88],
  C9: [11, 48], C10: [50, 33], C11: [70, 61], C12: [33, 55],
};
// Forest regions (centroids) and their adjacent clearings.
const FORESTS = {
  AutumnN:  [50, 19],
  AutumnNW: [27, 38],
  AutumnW:  [19, 62],
  AutumnSW: [33, 77],
  AutumnS:  [62, 70],
  AutumnE:  [82, 64],
  Witchwood:[66, 43],
};
const EDGES = [
  ["C1","C5"],["C1","C9"],["C1","C10"],["C2","C5"],["C2","C6"],["C2","C10"],
  ["C3","C6"],["C3","C7"],["C3","C11"],["C4","C8"],["C4","C9"],["C4","C12"],
  ["C6","C11"],["C7","C8"],["C7","C12"],["C9","C12"],["C10","C12"],["C11","C12"],
];

let token = localStorage.getItem("rmn-token") || "";
let game = null; // full payload: { room, seat, you, ...snapshot }
let etag = "";
let viewer = "";
let autofetch = true;
let intervalSec = 5;
let timer = null;
let ws = null;
let wsLive = false;
let wsConnected = false;
let wsRetry = 0;

function authHeaders(extra) {
  const h = Object.assign({}, extra || {});
  if (token) h["Authorization"] = "Bearer " + token;
  return h;
}

async function api(path, opts) {
  opts = opts || {};
  const headers = authHeaders(opts.headers);
  if (opts.body) headers["Content-Type"] = "application/json";
  const r = await fetch(path, Object.assign({}, opts, { headers }));
  let data = null;
  if (r.status !== 304) {
    try { data = await r.json(); } catch (e) { data = null; }
  }
  return { status: r.status, data, etag: r.headers.get("ETag") || "" };
}

async function fetchState(force) {
  if (!token) return false;
  const headers = authHeaders();
  if (!force && etag) headers["If-None-Match"] = etag;
  const r = await fetch("/api/state", { headers });
  if (r.status === 304) return false;
  if (r.status === 401) { signOut("Your token was rejected."); return false; }
  if (r.status === 404) { signOut("That room no longer exists."); return false; }
  etag = r.headers.get("ETag") || "";
  game = await r.json();
  viewer = game.you || viewer;
  render();
  return true;
}

async function doAction(id) {
  const r = await api("/api/action", { method: "POST", body: JSON.stringify({ id }) });
  if (r.data && !r.data.error) { game = r.data; etag = r.etag; viewer = game.you || viewer; render(); }
  else if (r.data && r.data.error) { toast(r.data.error); }
}

async function pickFaction(faction) {
  const r = await api("/api/faction", { method: "POST", body: JSON.stringify({ faction }) });
  if (r.data && !r.data.error) { game = r.data; etag = r.etag; viewer = game.you || viewer; render(); }
  else if (r.data && r.data.error) { toast(r.data.error); }
}

function toast(msg) {
  const el = document.getElementById("pendhint");
  if (el) { el.textContent = msg; setTimeout(() => { if (el.textContent === msg) el.textContent = ""; }, 4000); }
}

function render() {
  if (!game || !game.room) return;
  const room = game.room;
  if (!room.started) {
    document.getElementById("turnbar").innerHTML =
      `<div>room <b>${room.id}</b></div><div>choosing factions…</div>`;
    renderLobby(room);
    showOverlay("lobby");
    return;
  }
  hideOverlays();
  const g = game;
  document.getElementById("turnbar").innerHTML =
    `<div>room <b>${room.id}</b> · round <b>${g.round}</b> · phase <b>${PHASE[g.phase] || g.phase}</b></div>` +
    `<div>current <b>${g.current}</b> · you <b>${viewer || "?"}</b>` +
    (g.winner && g.winner.length ? ` · winner <b>${g.winner.join("+")}</b>` : "") + `</div>`;
  renderPlayers(g);
  renderMinimap(g);
  renderBoard(g);
  renderActions(g);
  renderLog(g);
  renderRMN(g);
}

function renderLobby(room) {
  const info = document.getElementById("lobbyinfo");
  const el = document.getElementById("factionpick");
  el.innerHTML = "";
  const seat = game.seat;
  const mine = room.seats[seat] ? room.seats[seat].faction : "";
  if (mine) {
    info.textContent = `You are ${mine}. Waiting for the other players to pick…`;
    return;
  }
  info.textContent = `You are seat ${seat + 1}. Pick an available faction.`;
  const taken = new Set(room.seats.filter(s => s.faction).map(s => s.faction));
  for (const f of ["MC", "ED", "WA", "VB"]) {
    const b = document.createElement("button");
    b.className = f + (taken.has(f) ? " taken" : "");
    b.textContent = f;
    b.disabled = taken.has(f);
    b.onclick = () => pickFaction(f);
    el.append(b);
  }
}

function showOverlay(id) {
  for (const x of ["landing", "created", "lobby"]) {
    document.getElementById(x).hidden = (x !== id);
  }
}
function hideOverlays() {
  for (const x of ["landing", "created", "lobby"]) {
    document.getElementById(x).hidden = true;
  }
}

function renderPlayers(g) {
  const el = document.getElementById("players");
  el.innerHTML = "";
  for (const f of g.order) {
    const p = g.players[f];
    const div = document.createElement("div");
    div.className = "pcard " + f + (f === g.current ? " current" : "");
    let extra = "";
    if (f === "MC") {
      // Wood on the board (spendable) vs the off-board supply reserve.
      const woodBoard = Object.values(g.clearings).reduce((n, c) => n + (c.Wood || 0), 0);
      extra =
        `<div class="row"><span>wood (board)</span><span>${woodBoard}</span></div>` +
        `<div class="row"><span>wood (supply)</span><span>${p.WoodSupply}</span></div>` +
        `<div class="row"><span>buildings left</span><span>${p.Sawmills}/${p.Workshops}/${p.Recruiters}</span></div>` +
        `<div class="row"><span>keep</span><span>${p.KeepClearing}</span></div>`;
    } else if (f === "ED") {
      extra = `<div class="row"><span>leader</span><span>${p.Leader}</span></div>` +
        `<div class="row"><span>roosts</span><span>${countRoosts(g, "ED")}</span></div>`;
      const dec = p.Decree || {};
      for (const col of ["RECRUIT", "MOVE", "BATTLE", "BUILD"]) {
        const cards = (dec[col] || []).map(cardLabel).join(" ");
        if (cards) extra += `<div class="row"><span>${col.slice(0, 3)}</span><span class="cards">${cards}</span></div>`;
      }
    } else if (f === "WA") {
      extra = `<div class="row"><span>officers</span><span>${p.Officers}</span></div>` +
        `<div class="row"><span>supporters</span><span class="cards">${(p.Supporters || []).map(cardLabel).join(" ")}</span></div>`;
    } else if (f === "VB") {
      extra = `<div class="row"><span>character</span><span>${p.Character}</span></div>` +
        `<div class="row"><span>at</span><span>${p.Pawn}</span></div>` +
        `<div class="row"><span>items</span><span class="cards">${itemList(p)}</span></div>`;
      const rel = p.Relationships || {};
      const tags = Object.entries(rel).map(([k, v]) => `<span class="tag ${v === "hostile" ? "hostile" : ""}">${k}:${v}</span>`).join("");
      extra += `<div class="tags">${tags}</div>`;
    }
    div.innerHTML =
      `<div class="phead"><span class="f">${f}${game.you === f ? " · you" : ""}</span><span class="vp">${p.VP} VP</span></div>` +
      `<div class="pbody">${extra}` +
      `<div class="row"><span>crafted</span><span>${(p.Crafted || []).map(cardLabel).join(" ") || "—"}</span></div>` +
      `</div>`;
    div.append(renderHand(p));
    el.append(div);
  }
}

function renderHand(p) {
  const wrap = document.createElement("div");
  wrap.className = "hand";
  const hand = p.Hand || [];
  if (hand.length === 0) {
    wrap.innerHTML = '<div class="hempty">no cards</div>';
    return wrap;
  }
  for (const id of hand) {
    if (id === "??") {
      const h = document.createElement("div");
      h.className = "hidden-card";
      h.textContent = "hidden card";
      wrap.append(h);
      continue;
    }
    const info = (game.cards && game.cards[id]) || { name: id, suit: "B", desc: "" };
    const c = document.createElement("div");
    c.className = "hcard suit-" + (info.suit || "B");
    c.innerHTML =
      `<div class="hname">${info.name}<span class="hid">${id}</span></div>` +
      `<div class="hcost">${info.cost ? "craft: " + info.cost : (info.kind === "ambush" ? "battle" : info.kind)}</div>` +
      `<div class="hdesc">${info.desc}</div>`;
    wrap.append(c);
  }
  return wrap;
}

function countRoosts(g, f) {
  let n = 0;
  for (const c of Object.values(g.clearings)) {
    for (const b of (c.Buildings || [])) if (b.Owner === f && b.Type === "roost") n++;
  }
  return n;
}

function itemList(p) {
  const out = [];
  for (const [id, it] of Object.entries(p.Items || {})) {
    let s = it.Type;
    if (it.Zone === "track") s += "↑";
    if (!it.FaceUp) s += "×";
    if (it.Damaged) s += "✗";
    out.push(s);
  }
  return out.join(" ");
}

function cardLabel(id) {
  if (id === "VIZIER") return "Viz";
  if (/^[FRMB]\d\d$/.test(id)) return id;
  return id;
}

function renderBoard(g) {
  const el = document.getElementById("board");
  el.innerHTML = "";

  // Roads (SVG underlay).
  const NS = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(NS, "svg");
  svg.setAttribute("class", "roads");
  svg.setAttribute("viewBox", "0 0 100 100");
  svg.setAttribute("preserveAspectRatio", "none");
  for (const [a, b] of EDGES) {
    if (!POS[a] || !POS[b]) continue;
    for (const cls of ["casing", "road"]) {
      const ln = document.createElementNS(NS, "line");
      ln.setAttribute("x1", POS[a][0]); ln.setAttribute("y1", POS[a][1]);
      ln.setAttribute("x2", POS[b][0]); ln.setAttribute("y2", POS[b][1]);
      ln.setAttribute("class", cls);
      ln.dataset.c1 = a; ln.dataset.c2 = b;
      svg.append(ln);
    }
  }
  el.append(svg);

  // Forest region labels.
  for (const [name, pos] of Object.entries(FORESTS)) {
    const f = document.createElement("div");
    f.className = "forest";
    f.style.left = pos[0] + "%";
    f.style.top = pos[1] + "%";
    f.textContent = name;
    el.append(f);
  }

  const ids = Object.keys(g.clearings).sort((a, b) => parseInt(a.slice(1)) - parseInt(b.slice(1)));
  const hl = new Set((g.legal || []).map(a => a.clearing || a.to || a.from).filter(Boolean));
  const vbPawn = (g.players && g.players.VB) ? g.players.VB.Pawn : "";

  for (const id of ids) {
    const c = g.clearings[id];
    const div = document.createElement("div");
    div.className = "clearing" + (hl.has(id) ? " hl" : "");
    const [x, y] = POS[id] || [50, 50];
    div.style.left = x + "%";
    div.style.top = y + "%";
    div.dataset.clearing = id;
    div.onmouseenter = () => highlightRoads(svg, id, true);
    div.onmouseleave = () => highlightRoads(svg, id, false);

    let chips = "";
    const order = ["MC", "ED", "WA", "VB"];
    for (const f of order) {
      const n = (c.Warriors || {})[f];
      if (n) chips += `<span class="chip ${f}">${f}×${n}</span>`;
    }
    for (const b of (c.Buildings || [])) chips += `<span class="chip ${b.Owner}">${b.Type}</span>`;
    for (const t of (c.Tokens || [])) chips += `<span class="chip ${t.Owner}">${t.Type}</span>`;
    if (c.Sympathy) chips += `<span class="chip WA">sympathy</span>`;
    if (vbPawn === id) chips += `<span class="chip VB">pawn</span>`;
    const wood = c.Wood ? `<span class="wood">wood ${c.Wood}</span>` : "";
    const slots = c.Slots || 0;
    const used = (c.Buildings || []).length;
    const hasRuin = c.Ruin ? 1 : 0;
    const free = Math.max(0, slots - used - hasRuin);
    let pips = "";
    for (let i = 0; i < used; i++) pips += '<span class="slot used"></span>';
    for (let i = 0; i < hasRuin; i++) pips += '<span class="slot ruinslot"></span>';
    for (let i = 0; i < free; i++) pips += '<span class="slot free"></span>';
    const ruinLabel = c.Ruin ? `<span class="ruin">ruin ${(c.RuinItem || "").replace(/^i\./, "")}</span>` : "";
    const slotRow = (slots || hasRuin)
      ? `<div class="slots" title="building slots: ${free} free of ${slots}"><span class="slotpips">${pips}</span>` +
        `<span class="slotnum">${free}/${slots}</span>${ruinLabel}</div>`
      : "";
    div.innerHTML =
      `<div class="cid"><span>${id}</span><span class="suit ${c.Suit}">${c.Suit}</span></div>` +
      `<div class="crowd">${chips}${wood}</div>${slotRow}`;
    el.append(div);
  }
  // Vagabond pawn in a forest.
  if (vbPawn && FORESTS[vbPawn]) {
    const pos = FORESTS[vbPawn];
    const pd = document.createElement("div");
    pd.className = "pawn";
    pd.style.left = pos[0] + "%";
    pd.style.top = (pos[1] + 8) + "%";
    pd.textContent = "VB pawn";
    pd.title = "Vagabond in " + vbPawn;
    el.append(pd);
  }

  document.getElementById("boardfoot").textContent =
    "roads: " + EDGES.map(([a, b]) => a + "–" + b).join("  ") +
    "   ·   forests: " + Object.keys(FORESTS).join(", ") +
    (vbPawn ? "   ·   VB pawn: " + vbPawn : "");
}

// Compact graph view (shown on small viewports where the board becomes a table).
function renderMinimap(g) {
  const el = document.getElementById("minimap");
  if (!el) return;
  el.innerHTML = "";
  const NS = "http://www.w3.org/2000/svg";
  const SX = 1.6, SY = 1.1; // match the 16:11 board so circles stay round
  const svg = document.createElementNS(NS, "svg");
  svg.setAttribute("viewBox", "0 0 160 110");
  svg.setAttribute("preserveAspectRatio", "xMidYMid meet");
  svg.setAttribute("class", "mm-svg");

  const hl = new Set((g.legal || []).map(a => a.clearing || a.to || a.from).filter(Boolean));

  for (const [a, b] of EDGES) {
    if (!POS[a] || !POS[b]) continue;
    const ln = document.createElementNS(NS, "line");
    ln.setAttribute("x1", POS[a][0] * SX); ln.setAttribute("y1", POS[a][1] * SY);
    ln.setAttribute("x2", POS[b][0] * SX); ln.setAttribute("y2", POS[b][1] * SY);
    ln.setAttribute("class", "mm-road");
    svg.append(ln);
  }

  for (const [name, pos] of Object.entries(FORESTS)) {
    const t = document.createElementNS(NS, "text");
    t.setAttribute("x", pos[0] * SX);
    t.setAttribute("y", pos[1] * SY);
    t.setAttribute("class", "mm-forest");
    t.textContent = name === "Witchwood" ? "Witchwood" : name.replace("Autumn", "");
    svg.append(t);
  }

  for (const [id, pos] of Object.entries(POS)) {
    const c = g.clearings[id];
    const suit = c ? c.Suit : "B";
    const node = document.createElementNS(NS, "circle");
    node.setAttribute("cx", pos[0] * SX);
    node.setAttribute("cy", pos[1] * SY);
    node.setAttribute("r", 6);
    node.setAttribute("class", "mm-node mm-suit-" + suit + (hl.has(id) ? " mm-hl" : ""));
    svg.append(node);
    const t = document.createElementNS(NS, "text");
    t.setAttribute("x", pos[0] * SX);
    t.setAttribute("y", pos[1] * SY);
    t.setAttribute("class", "mm-label");
    t.textContent = id.slice(1);
    svg.append(t);
  }

  const vb = g.players && g.players.VB;
  if (vb && vb.Pawn) {
    const p = POS[vb.Pawn] || FORESTS[vb.Pawn];
    if (p) {
      const dot = document.createElementNS(NS, "circle");
      dot.setAttribute("cx", p[0] * SX + 4.2);
      dot.setAttribute("cy", p[1] * SY - 4.2);
      dot.setAttribute("r", 3);
      dot.setAttribute("class", "mm-pawn");
      svg.append(dot);
    }
  }
  el.append(svg);
}

function highlightRoads(svg, id, on) {
  for (const ln of svg.querySelectorAll("line")) {
    if (ln.dataset.c1 === id || ln.dataset.c2 === id) {
      ln.classList.toggle("hot", on);
    }
  }
}

function renderActions(g) {
  const el = document.getElementById("actions");
  el.innerHTML = "";
  const head = document.getElementById("actionhead");
  const pend = document.getElementById("pendhint");
  pend.textContent = g.pending ? g.pending.Kind + " (" + g.pending.Player + ")" : "";
  if (g.setupMode) {
    head.textContent = "Setup · " + (g.setupStage || "");
  } else if (g.winner && g.winner.length) {
    head.textContent = "Game over";
  } else if (g.dayStage === "craft") {
    head.textContent = "Actions · " + g.current + " · craft first";
  } else if (g.dayStage === "decree") {
    head.textContent = "Actions · " + g.current + " · resolve Decree";
  } else {
    head.textContent = "Actions · " + g.current;
  }

  if (g.battle) {
    const b = g.battle;
    const banner = document.createElement("div");
    banner.className = "battlebar";
    const dice = (b.D1 !== undefined && b.D1 !== null) ? ` · dice ${b.D1}-${b.D2}` : "";
    banner.innerHTML = `<b>Battle</b> ${b.Attacker}→${b.Defender} at ${b.Clearing}` +
      `<div class="step">step ${b.Step}/5 · ${b.StepName}${dice}</div>` +
      `<div class="hint">attacker hits ${b.AtkHits || 0} · defender hits ${b.DefHits || 0}` +
      (b.Remaining ? ` · ${b.Remaining} to assign` : "") + `</div>`;
    el.append(banner);
  }

  if (g.winner && g.winner.length) {
    const d = document.createElement("div");
    d.className = "winner";
    d.textContent = "Winner: " + g.winner.join(" + ");
    el.append(d);
    return;
  }
  const acts = g.legal || [];
  if (acts.length === 0) {
    const d = document.createElement("div");
    d.className = "hint";
    if (!(g.winner && g.winner.length)) {
      d.textContent = (game.you && g.current === game.you)
        ? "No legal actions."
        : "Waiting for " + g.current + "…";
    }
    el.append(d);
    return;
  }
  // Group: pending first, then by kind.
  for (const a of acts) {
    const b = document.createElement("button");
    b.className = (a.kind || "").replace(/:/g, "-");
    b.textContent = a.label || a.id;
    b.onclick = () => doAction(a.id);
    el.append(b);
  }
}

function renderLog(g) {
  const el = document.getElementById("log");
  el.innerHTML = "";
  const entries = g.log || [];
  for (const e of entries.slice(-120)) {
    const li = document.createElement("li");
    li.className = (e.kind || "") + (e.kind === "battle" || e.kind === "turmoil" || e.kind === "ambush" ? " battle" : "");
    li.innerHTML = `<span class="seq">${e.seq}</span><span>${e.round}.${e.phase}</span>` +
      `<span class="act ${e.actor}">${e.actor}</span><span>${e.text}</span>`;
    el.append(li);
  }
  el.scrollTop = el.scrollHeight;
}

function renderRMN(g) {
  const el = document.getElementById("rmn");
  if (!el) return;
  el.innerHTML = "";
  const lines = g.rmn || [];
  if (lines.length === 0) {
    el.innerHTML = '<li class="rmnempty">No RMN events yet.</li>';
    return;
  }
  for (const line of lines) {
    const li = document.createElement("li");
    li.textContent = line;
    el.append(li);
  }
  el.scrollTop = el.scrollHeight;
}

// --- Autofetch ---
const autofetchBox = document.getElementById("autofetch");
const intervalInput = document.getElementById("interval");
const fetchDot = document.getElementById("fetchdot");

function restartTimer() {
  if (timer) clearInterval(timer);
  timer = null;
  if (wsConnected) { fetchDot.classList.add("on"); return; } // live push replaces polling
  if (!token || !autofetch) { fetchDot.classList.remove("on"); return; }
  fetchDot.classList.add("on");
  timer = setInterval(() => { fetchState(false).catch(() => {}); }, Math.max(2, intervalSec) * 1000);
}

// --- Live updates (opt-in WebSocket notification channel) ---
const liveBox = document.getElementById("live");
const liveDot = document.getElementById("livedot");

function closeWS() {
  wsConnected = false;
  if (liveDot) liveDot.classList.remove("on");
  if (ws) {
    try { ws.onclose = null; ws.close(); } catch (e) { /* ignore */ }
    ws = null;
  }
}

function connectWS() {
  if (!token || !wsLive) return;
  closeWS();
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  ws = new WebSocket(proto + "//" + location.host + "/api/ws");
  ws.onopen = () => { wsRetry = 0; try { ws.send(JSON.stringify({ token })); } catch (e) { /* ignore */ } };
  ws.onmessage = (e) => {
    let m;
    try { m = JSON.parse(e.data); } catch (err) { return; }
    if (m.type === "ready") {
      wsConnected = true;
      if (liveDot) liveDot.classList.add("on");
      if (timer) { clearInterval(timer); timer = null; }
    } else if (m.type === "changed") {
      if (game && m.seq !== undefined && m.seq === game.seq) return;
      fetchState(false).catch(() => {});
    }
  };
  ws.onclose = () => {
    wsConnected = false;
    if (liveDot) liveDot.classList.remove("on");
    ws = null;
    restartTimer();
    if (wsLive) {
      wsRetry++;
      setTimeout(connectWS, Math.min(30000, 1000 * Math.pow(2, wsRetry)));
    }
  };
  ws.onerror = () => { try { ws.close(); } catch (e) { /* ignore */ } };
}

function setLive(on) {
  wsLive = on;
  if (on) { connectWS(); } else { closeWS(); restartTimer(); }
}
liveBox.onchange = () => setLive(liveBox.checked);
autofetchBox.onchange = () => { autofetch = autofetchBox.checked; restartTimer(); };
intervalInput.onchange = () => {
  intervalSec = Math.max(2, parseInt(intervalInput.value, 10) || 5);
  intervalInput.value = intervalSec;
  restartTimer();
};

// --- Landing / join / create ---
function renderTokens(roomId, tokens) {
  document.getElementById("createdid").textContent = roomId;
  const el = document.getElementById("toklist");
  el.innerHTML = "";
  tokens.forEach((tok, i) => {
    const link = location.origin + location.pathname + "#t=" + encodeURIComponent(tok);
    const row = document.createElement("div");
    row.className = "tokrow";
    const code = document.createElement("code");
    code.textContent = link;
    const copy = document.createElement("button");
    copy.className = "btn ghost";
    copy.textContent = "copy";
    copy.onclick = () => { if (navigator.clipboard) navigator.clipboard.writeText(link); copy.textContent = "copied"; };
    row.innerHTML = `<span class="chip">seat ${i + 1}</span>`;
    row.append(code, copy);
    el.append(row);
  });
  document.getElementById("enter").onclick = () => setToken(tokens[0]);
}

function setToken(tok) {
  token = tok;
  localStorage.setItem("rmn-token", tok);
  history.replaceState(null, "", location.pathname);
  hideOverlays();
  fetchState(true).then(() => { if (wsLive) connectWS(); restartTimer(); });
}

function signOut(msg) {
  closeLogout();
  token = ""; game = null; etag = ""; viewer = "";
  localStorage.removeItem("rmn-token");
  closeWS();
  if (timer) clearInterval(timer);
  timer = null;
  for (const id of ["players", "actions", "board", "minimap", "log", "rmn", "turnbar"]) {
    const el = document.getElementById(id);
    if (el) el.innerHTML = "";
  }
  document.getElementById("boardfoot").textContent = "";
  document.getElementById("landingerr").textContent = msg || "";
  showOverlay("landing");
}

document.getElementById("create").onclick = async () => {
  const players = parseInt(document.getElementById("playerCount").value, 10);
  if (!Number.isInteger(players) || players < 2 || players > 4) {
    document.getElementById("landingerr").textContent = "Choose 2 to 4 players.";
    return;
  }
  const name = document.getElementById("creatorname").value.trim();
  const r = await api("/api/rooms", { method: "POST", body: JSON.stringify({ players, names: name ? [name] : [] }) });
  if (r.data && r.data.room) {
    renderTokens(r.data.room.id, r.data.tokens);
    showOverlay("created");
  } else {
    document.getElementById("landingerr").textContent = (r.data && r.data.error) || "failed to create room";
  }
};
document.getElementById("join").onclick = () => {
  const tok = document.getElementById("jointoken").value.trim();
  if (tok) setToken(tok);
};
// --- Logout (always available) ---
const logoutModal = document.getElementById("logoutmodal");
const logoutToken = document.getElementById("logouttoken");
const logoutWrap = document.getElementById("logouttokenwrap");
const logoutWarn = document.getElementById("logoutwarn");

function openLogout() {
  if (token) {
    logoutWarn.textContent =
      "This removes your seat token from this browser. If you haven't saved it " +
      "somewhere, you won't be able to rejoin this seat — the token will be lost forever.";
    logoutToken.textContent = token;
    logoutWrap.hidden = false;
    document.getElementById("logoutconfirm").hidden = false;
  } else {
    logoutWarn.textContent = "No seat token is stored in this browser.";
    logoutToken.textContent = "";
    logoutWrap.hidden = true;
    document.getElementById("logoutconfirm").hidden = true;
  }
  logoutModal.hidden = false;
}
function closeLogout() { logoutModal.hidden = true; }

document.getElementById("logout").onclick = openLogout;
document.getElementById("logoutcancel").onclick = closeLogout;
document.getElementById("copytoken").onclick = () => {
  const b = document.getElementById("copytoken");
  if (navigator.clipboard) {
    navigator.clipboard.writeText(token).then(() => { b.textContent = "copied"; setTimeout(() => { b.textContent = "copy"; }, 1500); });
  }
};
document.getElementById("logoutconfirm").onclick = () => {
  closeLogout();
  signOut("Logged out. Save your token if you want to rejoin this seat.");
};

// --- Boot ---
(async function boot() {
  autofetch = autofetchBox.checked;
  intervalSec = Math.max(2, parseInt(intervalInput.value, 10) || 5);
  const hash = location.hash.match(/[#&]t=([^&]+)/);
  if (hash) token = decodeURIComponent(hash[1]);
  if (!token) { showOverlay("landing"); return; }
  hideOverlays();
  await fetchState(true);
  restartTimer();
})().catch(err => {
  document.getElementById("actions").innerHTML = `<div class="winner">Failed to start: ${err}</div>`;
});
