// Replay viewer: load an RMN log, scrub the timeline, or press play to watch
// the game animate with the same move/battle/build effects as the demo.

let game = null;
let viewer = "";
let states = [];
let actions = [];
let idx = 0;
let playing = false;

const $ = (id) => document.getElementById(id);
const N = () => Math.max(0, states.length - 1);

function render() {
  if (!game) return;
  renderTurnBanner(game);
  renderPlayers(game);
  renderBoard(game);
  renderLog(game);
  updateLabel();
  if (howtoIsOpen()) howtoRender(activeFaction(game));
}

function updateLabel() {
  const el = $("rlabel");
  if (!el) return;
  if (!states.length) { el.textContent = "no replay loaded"; return; }
  const g = states[idx] || {};
  const phase = (PHASE[g.phase] || g.phase || "").toLowerCase();
  const act = idx > 0 && actions[idx - 1] ? " · " + bannerText(actions[idx - 1]) : "";
  el.textContent = `round ${g.round} · ${phase} · event ${idx}/${N()}${act}`;
}

function setIdx(k) {
  if (!states.length) return;
  idx = Math.max(0, Math.min(N(), k));
  game = states[idx];
  const s = $("timeline");
  if (s && +s.value !== idx) s.value = String(idx);
  // Keep the window in view while playing.
  const g = states[idx] || {};
  if (g.winner && g.winner.length) render();
  else render();
}

function pause() {
  playing = false;
  const b = $("play");
  if (b) { b.textContent = "▶"; b.classList.remove("on"); }
}

async function play() {
  if (!states.length || playing) return;
  playing = true;
  const b = $("play");
  if (b) { b.textContent = "⏸"; b.classList.add("on"); }
  if (idx >= N()) setIdx(0);
  while (playing && idx < N()) {
    const pre = states[idx];
    const a = actions[idx];
    const post = states[idx + 1];
    game = pre;
    await animateAction(pre, a, post);
    idx++;
    game = post;
    const s = $("timeline");
    if (s) s.value = String(idx);
    updateLabel();
  }
  pause();
}

function setSpeed(v) {
  animSpeed = Math.max(0.1, +v || 1);
}

function buildTicks() {
  const ticks = $("ticks");
  if (!ticks) return;
  ticks.innerHTML = "";
  if (!states.length) return;
  const total = N();
  if (total === 0) return;
  let lastRound = null;
  for (let i = 0; i <= total; i++) {
    const r = (states[i] && states[i].round) || 0;
    if (r !== lastRound) {
      lastRound = r;
      const t = document.createElement("span");
      t.className = "tick";
      t.style.left = (100 * i / total) + "%";
      t.title = "round " + r;
      ticks.append(t);
    }
  }
}

function loadRMN(text) {
  const err = $("pasteerr");
  err.textContent = "";
  if (!window.RootBot) { err.textContent = "Engine not ready yet."; return; }
  let res;
  try {
    res = JSON.parse(RootBot.replay(text));
  } catch (e) {
    err.textContent = "Could not parse the log: " + e;
    return;
  }
  if (res.error) { err.textContent = res.error; return; }

  states = [res.start, ...res.events.map((e) => e.state)];
  actions = res.events.map((e) => e.action);
  for (const s of states) s.cards = res.cards;

  const s = $("timeline");
  s.max = String(N());
  s.value = "0";
  idx = 0;
  playing = false;
  const b = $("play");
  if (b) { b.textContent = "▶"; b.classList.remove("on"); }

  hidePaste();
  buildTicks();
  setIdx(0);

  const warn = $("replaywarn");
  if (res.failure) {
    warn.hidden = false;
    warn.textContent = "Replay stopped early — " + res.failure;
  } else {
    warn.hidden = true;
  }
}

function showPaste() { $("paste").hidden = false; }
function hidePaste() { $("paste").hidden = true; }

async function loadEngineBytes() {
  if (typeof DecompressionStream === "function") {
    try {
      const r = await fetch("bot.wasm.gz");
      if (r.ok) {
        const stream = r.body.pipeThrough(new DecompressionStream("gzip"));
        return await new Response(stream).arrayBuffer();
      }
    } catch (e) { /* fall through */ }
  }
  return await (await fetch("bot.wasm")).arrayBuffer();
}

async function boot() {
  showPaste();
  try {
    const go = new Go();
    const mod = await WebAssembly.instantiate(await loadEngineBytes(), go.importObject);
    go.run(mod.instance);
  } catch (e) {
    $("pasteerr").textContent = "Failed to load the engine: " + e;
    return;
  }
  for (let i = 0; i < 600 && !window.RootBot; i++) await new Promise((r) => setTimeout(r, 20));
  const btn = $("loadrmn");
  if (btn) btn.disabled = false;
  if (!window.RootBot) $("pasteerr").textContent = "The engine did not start.";
}

// --- wiring ---
$("play").onclick = () => { if (playing) pause(); else play(); };
$("prev").onclick = () => { pause(); setIdx(idx - 1); };
$("next").onclick = () => { pause(); setIdx(idx + 1); };
$("timeline").oninput = (e) => { pause(); setIdx(+e.target.value); };
$("speed").onchange = (e) => setSpeed(e.target.value);
$("openpaste").onclick = showPaste;
$("togglehowto").onclick = () => howtoToggle(activeFaction(game || {}));
$("howtoclose").onclick = howtoClose;
$("howtobackdrop").onclick = howtoClose;
$("loadrmn").onclick = () => loadRMN($("rmntext").value);
$("rmnfile").onchange = (e) => {
  const f = e.target.files && e.target.files[0];
  if (!f) return;
  const r = new FileReader();
  r.onload = () => { $("rmntext").value = String(r.result); loadRMN(String(r.result)); };
  r.readAsText(f);
};
window.addEventListener("keydown", (e) => {
  if (e.target && /INPUT|TEXTAREA|SELECT/.test(e.target.tagName)) return;
  if (e.key === "Escape") { howtoClose(); return; }
  if (e.key === " ") { e.preventDefault(); if (playing) pause(); else play(); }
  if (e.key === "ArrowLeft") { pause(); setIdx(idx - 1); }
  if (e.key === "ArrowRight") { pause(); setIdx(idx + 1); }
});

boot();
