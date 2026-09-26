"use strict";

// Board rendering, animation and the per-faction panels live in board.js (shared
// with the replay viewer); the "How to play" sidebar lives in howto.js. This file
// drives the correspondence transport: rooms, seats, polling/WebSocket, actions,
// custom-RMN input and export.

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
let rmnBusy = false;

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

// Apply a legal action, animating the move/battle/build when we can. The
// animation helper reads and writes the global `game`, so we hand it the
// pre-action payload plus the action that was chosen.
async function doAction(id) {
  const pre = game;
  const a = ((pre && pre.legal) || []).find(x => x.id === id) || { id };
  const r = await api("/api/action", { method: "POST", body: JSON.stringify({ id }) });
  if (r.data && !r.data.error) {
    const post = r.data;
    etag = r.etag;
    viewer = post.you || viewer;
    if (typeof animateAction === "function") { await animateAction(pre, a, post); }
    else { game = post; render(); }
  } else if (r.data && r.data.error) {
    toast(r.data.error);
  }
}

async function pickFaction(faction) {
  const r = await api("/api/faction", { method: "POST", body: JSON.stringify({ faction }) });
  if (r.data && !r.data.error) { game = r.data; etag = r.etag; viewer = game.you || viewer; render(); }
  else if (r.data && r.data.error) { toast(r.data.error); }
}

function render() {
  if (!game || !game.room) return;
  const room = game.room;
  if (!room.started) {
    hideTurnBanner();
    document.getElementById("turnbar").innerHTML =
      `<div>room <b>${room.id}</b></div><div>choosing factions…</div>`;
    renderLobby(room);
    showOverlay("lobby");
    return;
  }
  hideOverlays();
  const g = game;
  const actor = activeFaction(g);
  document.getElementById("turnbar").innerHTML =
    `<div>room <b>${room.id}</b> · round <b>${g.round}</b> · phase <b>${PHASE[g.phase] || g.phase}</b></div>` +
    `<div>to act <b>${actor || "?"}</b> · you <b>${viewer || "?"}</b>` +
    (g.winner && g.winner.length ? ` · winner <b>${g.winner.join("+")}</b>` : "") + `</div>`;
  renderTurnBanner(g);
  renderPlayers(g);
  renderMinimap(g);
  renderBoard(g);
  renderActions(g);
  renderLog(g);
  renderRMN(g);
  if (howtoIsOpen()) howtoRender(howtoFaction());
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
  for (const x of ["landing", "lobby"]) {
    document.getElementById(x).hidden = (x !== id);
  }
  if (id === "landing") loadRooms();
}
function hideOverlays() {
  for (const x of ["landing", "lobby"]) {
    document.getElementById(x).hidden = true;
  }
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

function renderActions(g) {
  const el = document.getElementById("actions");
  el.innerHTML = "";
  const head = document.getElementById("actionhead");
  const pend = document.getElementById("pendhint");
  const actor = activeFaction(g);
  pend.textContent = g.pending ? g.pending.Kind + " (" + g.pending.Player + ")" : "";
  if (g.setupMode) {
    head.textContent = "Setup · " + actor + (g.setupStage ? " · " + g.setupStage : "");
  } else if (g.winner && g.winner.length) {
    head.textContent = "Game over";
  } else if (g.pending) {
    head.textContent = "Actions · " + actor + " · " + (PENDING_LABELS[g.pending.Kind] || g.pending.Kind);
  } else if (g.dayStage === "craft") {
    head.textContent = "Actions · " + actor + " · craft first";
  } else if (g.dayStage === "decree") {
    head.textContent = "Actions · " + actor + " · resolve Decree";
  } else {
    head.textContent = "Actions · " + actor;
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
      d.textContent = (game.you && actor === game.you)
        ? "No legal actions."
        : "Waiting for " + actor + "…";
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

// --- How to play ---
const howtoFaction = () => (game && (game.you || activeFaction(game))) || "";
document.getElementById("togglehowto").onclick = () => howtoToggle(howtoFaction());
document.getElementById("howtoclose").onclick = howtoClose;
document.getElementById("howtobackdrop").onclick = howtoClose;

// --- Custom RMN input ---
// The player types just "intent + operands" (e.g. "V:explore at=C12"); the
// active player is assumed and the sequence/round.phase are inferred from the
// log. The server rebuilds authoritatively; we show a preview of the full line.
const rmnIn = document.getElementById("rmnin");
const rmnPrev = document.getElementById("rmnpreview");
const rmnErr = document.getElementById("rmnerr");

function rmnFullLine(text) {
  if (!text || !game || !game.room || !game.room.started) return "";
  const actor = activeFaction(game);
  const seq = ((game.rmn && game.rmn.length) || 0) + 1;
  return `${seq} ${game.round}.${game.phase} ${actor} ${text}`;
}

function updateRMNPreview() {
  const text = rmnIn.value.trim();
  rmnPrev.textContent = text ? "→ " + rmnFullLine(text) : "";
}

async function applyCustomRMN() {
  const text = rmnIn.value.trim();
  if (!text || rmnBusy) return;
  if (!game || !game.room || !game.room.started) { rmnErr.textContent = "the game has not started"; return; }
  rmnBusy = true;
  rmnErr.textContent = "";
  try {
    const r = await api("/api/rmn", { method: "POST", body: JSON.stringify({ line: text }) });
    if (r.data && !r.data.error) {
      rmnIn.value = "";
      rmnPrev.textContent = "";
      const post = r.data;
      etag = r.etag;
      viewer = post.you || viewer;
      game = post;
      render();
    } else {
      rmnErr.textContent = (r.data && r.data.error) || "could not apply RMN";
    }
  } catch (e) {
    rmnErr.textContent = "network error";
  } finally {
    rmnBusy = false;
  }
}
document.getElementById("rmngo").onclick = applyCustomRMN;
rmnIn.addEventListener("input", updateRMNPreview);
rmnIn.addEventListener("keydown", e => {
  if (e.key === "Enter") { e.preventDefault(); applyCustomRMN(); }
});

// --- Export the unredacted RMN log ---
const exportDlg = document.getElementById("exportdlg");
const exportText = document.getElementById("exporttext");
const exportStatus = document.getElementById("exportstatus");
const exportShare = document.getElementById("exportshare");

document.getElementById("exportrmn").onclick = async () => {
  if (!token) return;
  exportStatus.textContent = "";
  exportText.value = "loading…";
  exportDlg.hidden = false;
  exportShare.hidden = !(navigator.canShare && window.File);
  try {
    const r = await fetch("/api/export", { headers: authHeaders() });
    exportText.value = r.ok ? await r.text() : "";
    if (!r.ok) exportStatus.textContent = "Export failed (" + r.status + ").";
  } catch (e) {
    exportText.value = "";
    exportStatus.textContent = "Export failed.";
  }
  exportText.focus();
};
document.getElementById("exportclose").onclick = () => { exportDlg.hidden = true; };

document.getElementById("exportcopy").onclick = async () => {
  const text = exportText.value;
  try {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(text);
    } else {
      exportText.focus();
      exportText.select();
      document.execCommand("copy");
    }
    exportStatus.textContent = "Copied " + text.length + " characters.";
  } catch (e) {
    exportText.focus();
    exportText.select();
    exportStatus.textContent = "Copy failed — select the text and copy manually.";
  }
};

document.getElementById("exportdl").onclick = () => {
  try {
    const blob = new Blob([exportText.value], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "root.rmn";
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    exportStatus.textContent = "Download started (if your browser allows it).";
  } catch (e) {
    exportStatus.textContent = "Download not supported here — use Copy.";
  }
};

exportShare.onclick = async () => {
  const text = exportText.value;
  try {
    const file = new File([text], "root.rmn", { type: "text/plain" });
    if (navigator.canShare && navigator.canShare({ files: [file] })) {
      await navigator.share({ files: [file], title: "ROOT RMN" });
    } else {
      await navigator.share({ title: "ROOT RMN", text });
    }
  } catch (e) { /* user cancelled */ }
};

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

// --- Lobby: the host's tables, with clickable seats ---
function setToken(tok) {
  token = tok;
  localStorage.setItem("rmn-token", tok);
  history.replaceState(null, "", location.pathname);
  hideOverlays();
  fetchState(true).then(() => { if (wsLive) connectWS(); restartTimer(); });
}

function clearSession(msg) {
  token = ""; game = null; etag = ""; viewer = "";
  hideTurnBanner();
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

// signOut clears a token the server has already rejected.
function signOut(msg) {
  closeLogout();
  clearSession(msg);
}

// leaveSeat frees the seat on the server (revoking the token), then clears it.
async function leaveSeat() {
  closeLogout();
  if (token) {
    try { await api("/api/leave", { method: "POST", body: "{}" }); } catch (e) { /* ignore */ }
  }
  clearSession("");
}

// --- Lobby: the host's tables, with clickable seats ---
async function loadRooms() {
  const el = document.getElementById("roomlist");
  if (!el) return;
  el.innerHTML = '<div class="roomempty">Setting the tables…</div>';
  let r;
  try {
    r = await api("/api/rooms");
  } catch (e) {
    el.innerHTML = '<div class="roomempty">Could not reach the server.</div>';
    return;
  }
  const rooms = (r.data && r.data.rooms) || [];
  el.innerHTML = "";
  if (rooms.length === 0) {
    el.innerHTML = '<div class="roomempty">No tables yet — the host hasn’t set any up.</div>';
    return;
  }
  for (const room of rooms) el.append(roomTable(room));
}

// roomTable wraps the pixel-art table with its name and status.
function roomTable(room) {
  const wrap = document.createElement("div");
  wrap.className = "roomtable";

  const seated = room.seats.filter(s => s.occupied).length;
  const status = room.started ? "in progress" : "lobby";
  const head = document.createElement("div");
  head.className = "rt-head";
  head.innerHTML =
    `<span class="rt-name">${escapeHTML(room.name || ("room " + room.id))}</span>` +
    `<span class="rt-meta">${status} · ${seated}/${room.seats.length} seated</span>`;
  wrap.append(head, pixelTable(room));
  return wrap;
}

// pixelTable draws the table and one chair per seat as crisp pixel blocks.
function pixelTable(room) {
  const NS = "http://www.w3.org/2000/svg";
  const n = room.seats.length;
  const svg = document.createElementNS(NS, "svg");
  svg.setAttribute("viewBox", "0 0 120 84");
  svg.setAttribute("class", "ptable");
  svg.setAttribute("role", "group");
  svg.setAttribute("aria-label", "Room " + room.id + ", " + n + " seats");

  // Chair anchors around the table, chosen by seat count.
  const layouts = {
    2: [[60, 7], [60, 77]],
    3: [[60, 7], [19, 77], [101, 77]],
    4: [[60, 7], [60, 77], [7, 42], [113, 42]],
  };
  const spots = layouts[n] || layouts[4];

  // Tabletop: blocky planks with a felt centre and an engraved plaque.
  svg.append(pixelRect(26, 20, 68, 44, "wood-dark"));
  svg.append(pixelRect(26, 20, 68, 4, "wood-hi"));
  svg.append(pixelRect(30, 27, 60, 30, "felt"));
  svg.append(pixelRect(40, 37, 40, 11, "plaque"));
  const plaque = document.createElementNS(NS, "text");
  plaque.setAttribute("x", 60);
  plaque.setAttribute("y", 45);
  plaque.setAttribute("class", "ptable-id");
  plaque.textContent = room.id;
  svg.append(plaque);

  room.seats.forEach((seat, i) => {
    const [cx, cy] = spots[i] || spots[spots.length - 1];
    svg.append(chair(room, seat, cx, cy));
  });
  return svg;
}

function pixelRect(x, y, w, h, cls) {
  const NS = "http://www.w3.org/2000/svg";
  const r = document.createElementNS(NS, "rect");
  r.setAttribute("x", x); r.setAttribute("y", y);
  r.setAttribute("width", w); r.setAttribute("height", h);
  r.setAttribute("class", cls);
  return r;
}

// chair is a top-down seat; free chairs are interactive, taken ones show the
// occupant's faction.
function chair(room, seat, cx, cy) {
  const NS = "http://www.w3.org/2000/svg";
  const g = document.createElementNS(NS, "g");
  // A chair is takeable before the game starts, or to take over an abandoned
  // faction afterwards. The unused chairs at a 4-seat table stay closed.
  const takeable = !seat.occupied && (!room.started || !!seat.faction);
  const state = seat.occupied ? "occupied" : (takeable ? "free" : "empty");
  g.setAttribute("class", "pseat " + state + " " + (seat.faction || "") + (seat.bot ? " bot" : ""));
  g.setAttribute("transform", `translate(${cx} ${cy})`);
  g.append(pixelRect(-7, -7, 14, 14, "seat-shadow"));
  g.append(pixelRect(-6, -6, 12, 12, "seat-body"));
  g.append(pixelRect(-6, -6, 12, 3, "seat-back"));
  if (seat.faction) g.append(pixelRect(-4, -1, 8, 7, "seat-cushion"));

  const label = document.createElementNS(NS, "text");
  label.setAttribute("x", 0);
  label.setAttribute("y", 3);
  label.setAttribute("class", "pseat-label");
  label.textContent = seat.faction || (seat.bot ? "AI" : String(seat.index + 1));
  g.append(label);

  if (!takeable) {
    const who = seat.faction ? " by " + seat.faction : "";
    g.setAttribute("aria-label", seat.occupied
      ? "Seat " + (seat.index + 1) + (seat.bot ? " (bot)" : "") + " taken" + who
      : "Seat " + (seat.index + 1) + " closed");
    return g;
  }
  g.setAttribute("role", "button");
  g.setAttribute("tabindex", "0");
  g.setAttribute("aria-label", "Take seat " + (seat.index + 1));
  const take = () => takeSeat(room.id, seat, g);
  g.addEventListener("click", take);
  g.addEventListener("keydown", e => {
    if (e.key === "Enter" || e.key === " ") { e.preventDefault(); take(); }
  });
  return g;
}

async function takeSeat(roomID, seat, node) {
  const verb = seat.faction ? ("Take over " + seat.faction + " in") : "Take";
  if (!window.confirm(`${verb} seat ${seat.index + 1}?`)) return;
  node.classList.add("busy");
  let r;
  try {
    r = await api("/api/take", { method: "POST", body: JSON.stringify({ room: roomID, seat: seat.id }) });
  } catch (e) {
    r = { data: { error: "network error" } };
  }
  node.classList.remove("busy");
  if (r.data && r.data.token) { setToken(r.data.token); return; }
  document.getElementById("landingerr").textContent = (r.data && r.data.error) || "could not take that seat";
  loadRooms();
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

document.getElementById("refreshrooms").onclick = () => { loadRooms(); };
document.getElementById("join").onclick = () => {
  const tok = document.getElementById("jointoken").value.trim();
  if (tok) setToken(tok);
};
// --- Leave (always available) ---
const logoutModal = document.getElementById("logoutmodal");

function openLogout() {
  document.getElementById("logoutconfirm").hidden = !token;
  logoutModal.hidden = false;
}
function closeLogout() { logoutModal.hidden = true; }

document.getElementById("logout").onclick = openLogout;
document.getElementById("logoutcancel").onclick = closeLogout;
document.getElementById("logoutconfirm").onclick = () => { leaveSeat(); };

// --- Collapsible Players drawer (small viewports) ---
const playersToggle = document.getElementById("toggleplayers");
const drawerBackdrop = document.getElementById("drawerbackdrop");
function setDrawer(open) {
  document.body.classList.toggle("players-open", open);
  if (playersToggle) playersToggle.setAttribute("aria-expanded", open ? "true" : "false");
  if (drawerBackdrop) drawerBackdrop.hidden = !open;
}
if (playersToggle) {
  playersToggle.onclick = () => setDrawer(!document.body.classList.contains("players-open"));
}
if (drawerBackdrop) {
  drawerBackdrop.onclick = () => setDrawer(false);
}
window.addEventListener("keydown", e => { if (e.key === "Escape") { setDrawer(false); howtoClose(); } });

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
