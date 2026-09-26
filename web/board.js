// Shared board rendering and animation for the ROOT tools. This mirrors the
// functions in app.js (the playable demo) so the replay viewer animates moves
// exactly the same way. Loaded only by replay.html; the demo keeps its copy.
//
// It reads two globals the page provides: `game` (current snapshot, with
// `game.you` and `game.cards`) and `viewer` (the side being shown, "" for a
// replayed spectator view). The page must also provide render().

const PHASE = { S: "Setup", B: "Birdsong", D: "Daylight", E: "Evening" };

// Autumn map: clearing positions (percent) and the 18 printed paths.
const POS = {
  C1: [13, 16], C2: [87, 16], C3: [87, 84], C4: [13, 84],
  C5: [50, 10], C6: [90, 48], C7: [57, 82], C8: [29, 88],
  C9: [11, 48], C10: [50, 33], C11: [70, 61], C12: [33, 55],
};
const FORESTS = {
  AutumnN: [50, 19], AutumnNW: [27, 38], AutumnW: [19, 62], AutumnSW: [33, 77],
  AutumnS: [62, 70], AutumnE: [82, 64], Witchwood: [66, 43],
};
const EDGES = [
  ["C1", "C5"], ["C1", "C9"], ["C1", "C10"], ["C2", "C5"], ["C2", "C6"], ["C2", "C10"],
  ["C3", "C6"], ["C3", "C7"], ["C3", "C11"], ["C4", "C8"], ["C4", "C9"], ["C4", "C12"],
  ["C6", "C11"], ["C7", "C8"], ["C7", "C12"], ["C9", "C12"], ["C10", "C12"], ["C11", "C12"],
];

const FACTION_NAME = { MC: "Marquise", ED: "Eyrie" };

// Vagabond characters and their special action, shown in the player panel so the
// ability is always visible.
const VB_CHARACTERS = {
  thief:  { name: "Thief",  ability: "Steal",     text: "Exhaust a torch to take a random card from any player in your clearing." },
  tinker: { name: "Tinker", ability: "Day Labor", text: "Exhaust a torch to take a card from the discard pile whose suit matches your clearing (or a bird)." },
  ranger: { name: "Ranger", ability: "Hideout",   text: "Exhaust a torch to repair 3 items, then immediately end Daylight and begin Evening." },
};

function toast(msg) {
  const el = document.getElementById("pendhint");
  if (el) { el.textContent = msg; setTimeout(() => { if (el.textContent === msg) el.textContent = ""; }, 4000); }
}

const PENDING_LABELS = {
  "battle-hits": "assigning battle hits",
  "battle-ambush": "ambush",
  "battle-effects": "battle effects",
  "discard-down": "discarding cards",
  "field-hospitals": "field hospitals",
};

function activeFaction(g) {
  if (g.pending && g.pending.Player) return g.pending.Player;
  return g.current;
}

function actionLabel(g) {
  if (g.setupMode) return "setup";
  if (g.pending && PENDING_LABELS[g.pending.Kind]) return PENDING_LABELS[g.pending.Kind];
  if (g.battle) return "battle · " + (g.battle.StepName || "");
  return (PHASE[g.phase] || g.phase || "").toLowerCase();
}

function renderTurnBanner(g) {
  const el = document.getElementById("turnbanner");
  if (!el) return;
  if (g.winner && g.winner.length) {
    el.hidden = false;
    el.className = "turnbanner win";
    el.innerHTML = `<span class="tb-who">Game over</span><span class="tb-label">${g.winner.join(" + ")} won</span>`;
    return;
  }
  const actor = activeFaction(g);
  if (!actor) { el.hidden = true; return; }
  const yours = actor === viewer;
  el.hidden = false;
  el.className = "turnbanner " + actor + (yours ? " your" : "");
  el.innerHTML =
    `<span class="tb-who">${yours ? "Your turn" : actor + "'s turn"}</span>` +
    `<span class="tb-label">${actionLabel(g)}</span>`;
}

function hideTurnBanner() {
  const el = document.getElementById("turnbanner");
  if (el) el.hidden = true;
}

function renderPlayers(g) {
  const el = document.getElementById("players");
  if (!el) return;
  el.innerHTML = "";
  const actor = activeFaction(g);
  for (const f of g.order) {
    const p = g.players[f];
    const div = document.createElement("div");
    div.className = "pcard " + f + (f === actor ? " current" : "");
    let extra = "";
    if (f === "MC") {
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
      const ch = VB_CHARACTERS[p.Character];
      extra = `<div class="row"><span>character</span><span>${ch ? ch.name : p.Character}</span></div>` +
        (ch ? `<div class="vbability"><b>${ch.ability}:</b> ${ch.text}</div>` : "") +
        `<div class="row"><span>at</span><span>${p.Pawn}</span></div>` +
        vbItemsHTML(p);
      const rel = p.Relationships || {};
      const tags = Object.entries(rel).map(([k, v]) => `<span class="tag ${v === "hostile" ? "hostile" : ""}">${k}:${v}</span>`).join("");
      extra += vbQuestsHTML(g, p);
      extra += `<div class="tags">${tags}</div>`;
    }
    div.innerHTML =
      `<div class="phead"><span class="f">${f}${game.you === f ? " · you" : ""}</span><span class="vp">${p.VP} VP</span></div>` +
      `<div class="pbody">${extra}` +
      `<div class="row"><span>crafted</span><span>${(p.Crafted || []).map(cardLabel).join(" ") || "—"}</span></div>` +
      `<div class="row"><span>crafted items</span><span>${craftedItems(p)}</span></div>` +
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

// vbQuestsHTML lists the face-up quests (public info) with their required items
// and reward: one VP per matching completed quest (including this one) or draw 2.
function vbQuestsHTML(g, p) {
  const defs = g.quests || {};
  const avail = g.questAvail || [];
  if (!avail.length) return "";
  const done = {};
  for (const qid of p.Quests || []) {
    const q = defs[qid];
    if (q) done[q.suit] = (done[q.suit] || 0) + 1;
  }
  let rows = "";
  for (const qid of avail) {
    const q = defs[qid];
    if (!q) continue;
    const vp = 1 + (done[q.suit] || 0);
    rows +=
      `<div class="quest"><span class="qsuit ${q.suit}">${q.suit}</span>` +
      `<span class="qname">${q.name}</span>` +
      `<span class="qreq">needs ${q.items.join(" + ")}</span>` +
      `<span class="qrew">${vp} VP or draw 2</span></div>`;
  }
  return `<div class="qhead">active quests</div>${rows}`;
}

// vbItemsHTML shows the Vagabond's items: tea/coin/bag counts on their tracks,
// the satchel contents (with an "x" prefix for exhausted items) and the
// satchel/damaged count against the 6 + 2-per-bag limit.
function vbItemsHTML(p) {
  const items = Object.values(p.Items || {});
  const track = (t) => items.filter((it) => it.Type === t && it.Zone === "track" && it.FaceUp).length;
  const satchel = items.filter((it) => it.Zone === "satchel");
  const damaged = items.filter((it) => it.Damaged);
  const used = satchel.length + damaged.length;
  const limit = 6 + 2 * track("bag");
  const sat = satchel.map((it) => (it.FaceUp ? "" : "x") + it.Type).join(" ") || "—";
  let rows =
    `<div class="row"><span>track</span><span class="cards">tea: ${track("tea")}/3  coin: ${track("coin")}/3  bag: ${track("bag")}/3</span></div>` +
    `<div class="row"><span>satchel</span><span class="cards">${sat} (${used}/${limit})</span></div>`;
  if (damaged.length) {
    rows += `<div class="row"><span>damaged</span><span class="cards">${damaged.map((it) => it.Type).join(" ")}</span></div>`;
  }
  return rows;
}

function cardLabel(id) {
  if (id === "VIZIER") return "Viz";
  if (/^[FRMB]\d\d$/.test(id)) return id;
  return id;
}

// craftedItems lists the item types a faction has crafted — the pool the
// Vagabond can take from with Aid.
function craftedItems(p) {
  const items = (p.CraftedItems || []).filter(Boolean);
  return items.length ? items.join(" ") : "—";
}

function renderBoard(g) {
  const el = document.getElementById("board");
  if (!el) return;
  el.innerHTML = "";

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
    const ruinLabel = c.Ruin ? `<span class="ruin">ruin</span>` : "";
    const slotRow = (slots || hasRuin)
      ? `<div class="slots" title="building slots: ${free} free of ${slots}"><span class="slotpips">${pips}</span>` +
        `<span class="slotnum">${free}/${slots}</span>${ruinLabel}</div>`
      : "";
    div.innerHTML =
      `<div class="cid"><span>${id}</span><span class="suit ${c.Suit}">${c.Suit}</span></div>` +
      `<div class="crowd">${chips}${wood}</div>${slotRow}`;
    el.append(div);
  }
  if (vbPawn && FORESTS[vbPawn]) {
    const pos = FORESTS[vbPawn];
    const pd = document.createElement("div");
    pd.className = "pawn";
    pd.style.left = pos[0] + "%";
    pd.style.top = (pos[1] + 8) + "%";
    pd.textContent = "VB pawn";
    el.append(pd);
  }

  const foot = document.getElementById("boardfoot");
  if (foot) {
    foot.textContent =
      "roads: " + EDGES.map(([a, b]) => a + "–" + b).join("  ") +
      "   ·   forests: " + Object.keys(FORESTS).join(", ");
  }
}

function highlightRoads(svg, id, on) {
  for (const ln of svg.querySelectorAll("line")) {
    if (ln.dataset.c1 === id || ln.dataset.c2 === id) ln.classList.toggle("hot", on);
  }
}

function renderLog(g) {
  const el = document.getElementById("log");
  if (!el) return;
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

const reducedMotion = () => window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
// Playback speed for the replay viewer (1 = normal). Pages may set this.
let animSpeed = 1;
const dur = (ms) => Math.max(0, Math.round(ms / animSpeed));

function showBanner(text, faction) {
  const el = document.getElementById("actionbanner");
  if (!el) return;
  el.getAnimations && el.getAnimations().forEach((a) => a.cancel());
  el.textContent = text;
  el.className = "actionbanner " + (faction || "");
  el.hidden = false;
  if (!reducedMotion()) {
    el.animate(
      [{ opacity: 0, transform: "translate(-50%, 8px)" }, { opacity: 1, transform: "translate(-50%, 0)" }],
      { duration: 160, easing: "ease-out" }
    );
  }
}
function hideBanner() {
  const el = document.getElementById("actionbanner");
  if (!el || el.hidden) return;
  if (reducedMotion()) { el.hidden = true; return; }
  el.getAnimations && el.getAnimations().forEach((a) => a.cancel());
  const a = el.animate(
    [{ opacity: 1, transform: "translate(-50%, 0)" }, { opacity: 0, transform: "translate(-50%, 8px)" }],
    { duration: 140, easing: "ease-in", fill: "forwards" }
  );
  a.onfinish = () => { el.hidden = true; a.cancel(); };
}

const BATTLE_KINDS = ["battle", "decree-battle", "vb-battle-ally", "vb-strike"];
const BUILD_KINDS = ["mc-build", "decree-build", "setup-mc-build"];
// Every action that moves warriors from one clearing to another (the Eyrie's
// Decree Move is "decree-move", not "move").
const MOVE_KINDS = ["move", "decree-move", "wa-move", "organize", "vb-move"];

const GLYPH = {
  sword: '<svg class="gi" viewBox="0 0 20 20" aria-hidden="true"><path d="M10 2v9M6 11h8M10 11v7" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>',
  hammer: '<svg class="gi" viewBox="0 0 20 20" aria-hidden="true"><rect x="5" y="2.5" width="10" height="4.5" rx="1" fill="currentColor"/><rect x="9.2" y="6.5" width="1.6" height="11.5" rx="0.8" fill="currentColor"/></svg>',
};

function clearingEl(id) {
  return document.querySelector('.clearing[data-clearing="' + id + '"]');
}

function showClearingCard(clearing, html, faction) {
  const el = clearingEl(clearing);
  if (!el) return null;
  el.classList.add("flash", faction || "");
  const card = document.createElement("div");
  card.className = "clearingtag " + (faction || "");
  card.innerHTML = html;
  el.appendChild(card);
  if (!reducedMotion()) {
    card.animate(cardKeyframes("in"), { duration: dur(180), easing: "ease-out" });
  }
  return card;
}

function hideClearingCard(card) {
  return new Promise((resolve) => {
    if (!card) { resolve(); return; }
    if (reducedMotion() || !card.animate) { card.remove(); resolve(); return; }
    const a = card.animate(cardKeyframes("out"), { duration: dur(150), easing: "ease-in", fill: "forwards" });
    const done = () => { card.remove(); resolve(); };
    a.onfinish = done;
    a.oncancel = done;
  });
}

function cardKeyframes(dir) {
  const tableMode = window.matchMedia && window.matchMedia("(max-width: 720px)").matches;
  if (tableMode || reducedMotion()) {
    return dir === "in" ? [{ opacity: 0 }, { opacity: 1 }] : [{ opacity: 1 }, { opacity: 0 }];
  }
  const center = "translateY(0) scale(1)";
  const off = "translateY(-6px) scale(.97)";
  return dir === "in"
    ? [{ opacity: 0, transform: off }, { opacity: 1, transform: center }]
    : [{ opacity: 1, transform: center }, { opacity: 0, transform: off }];
}

function battleCard(a) {
  const atk = `<span class="tagf ${a.faction}">${a.faction}</span>`;
  const def = a.target ? `<span class="tagf ${a.target}">${a.target}</span>` : "";
  return atk + GLYPH.sword + def;
}
function buildCard(a) {
  const f = `<span class="tagf ${a.faction}">${a.faction}</span>`;
  const name = a.building || (a.faction === "ED" ? "roost" : "");
  const b = name ? `<span class="tagg">${name}</span>` : "";
  return f + GLYPH.hammer + b;
}

function bannerText(a) {
  const who = FACTION_NAME[a.faction] || a.faction;
  if (MOVE_KINDS.includes(a.kind) && a.from && a.to && a.amount) {
    return `${who} moving ${a.amount} warrior${a.amount > 1 ? "s" : ""} ${a.from} → ${a.to}`;
  }
  if (BATTLE_KINDS.includes(a.kind) && a.clearing) {
    return `${who} attacking ${a.target || "?"} in ${a.clearing}`;
  }
  if (BUILD_KINDS.includes(a.kind) && a.clearing) {
    const name = a.building || (a.faction === "ED" ? "roost" : "a building");
    return `${who} building ${name} at ${a.clearing}`;
  }
  return (a.faction ? who + ": " : "") + (a.label || a.id);
}

// withSourceMoved copies the state with the moving warriors already removed from
// the origin clearing (visual step 1: the source count drops).
function withSourceMoved(pre, a) {
  const g = Object.assign({}, pre);
  g.clearings = Object.assign({}, pre.clearings);
  const src = Object.assign({}, pre.clearings[a.from]);
  src.Warriors = Object.assign({}, src.Warriors || {});
  const left = (src.Warriors[a.faction] || 0) - a.amount;
  if (left > 0) src.Warriors[a.faction] = left;
  else delete src.Warriors[a.faction];
  g.clearings[a.from] = src;
  return g;
}

// animateAction plays one engine action: moves slide a dot along the road,
// battles and builds flash the clearing with a notification card, then the
// resulting position is committed. The page's render() draws `game`.
async function animateAction(pre, a, post) {
  const isMove = MOVE_KINDS.includes(a.kind) && a.from && a.to && a.amount > 0;
  if (isMove) {
    game = withSourceMoved(pre, a);
    render();
    showBanner(bannerText(a), a.faction);
    await animateDot(a.from, a.to, a);
    game = post;
    render();
    await sleep(reducedMotion() ? 0 : dur(120));
    hideBanner();
    return;
  }
  if (BATTLE_KINDS.includes(a.kind) && a.clearing) {
    showBanner(bannerText(a), a.faction);
    const card = showClearingCard(a.clearing, battleCard(a), a.faction);
    await sleep(reducedMotion() ? 0 : dur(560));
    await hideClearingCard(card);
    game = post;
    render();
    await sleep(reducedMotion() ? 0 : dur(100));
    hideBanner();
    return;
  }
  if (BUILD_KINDS.includes(a.kind) && a.clearing) {
    showBanner(bannerText(a), a.faction);
    const card = showClearingCard(a.clearing, buildCard(a), a.faction);
    await sleep(reducedMotion() ? 0 : dur(360));
    await hideClearingCard(card);
    game = post;
    render();
    await sleep(reducedMotion() ? 0 : dur(100));
    hideBanner();
    return;
  }
  showBanner(bannerText(a), a.faction);
  await sleep(reducedMotion() ? 0 : dur(260));
  hideBanner();
  game = post;
  render();
}

function animateDot(from, to, a) {
  return new Promise((resolve) => {
    const board = document.getElementById("board");
    const tableMode = window.matchMedia && window.matchMedia("(max-width: 720px)").matches;
    if (!board || !POS[from] || !POS[to] || tableMode || reducedMotion()) {
      resolve();
      return;
    }
    const dot = document.createElement("div");
    dot.className = "movdot " + a.faction;
    dot.textContent = a.amount;
    dot.style.left = POS[from][0] + "%";
    dot.style.top = POS[from][1] + "%";
    board.appendChild(dot);
    const rect = board.getBoundingClientRect();
    const dx = ((POS[to][0] - POS[from][0]) / 100) * rect.width;
    const dy = ((POS[to][1] - POS[from][1]) / 100) * rect.height;
    const anim = dot.animate(
      [
        { transform: "translate(-50%, -50%)" },
        { transform: `translate(calc(-50% + ${dx}px), calc(-50% + ${dy}px))` },
      ],
      { duration: dur(620), easing: "cubic-bezier(.33,.08,.36,1)", fill: "forwards" }
    );
    const done = () => { dot.remove(); resolve(); };
    anim.onfinish = done;
    anim.oncancel = done;
  });
}
