// "How to play" per faction — concise summaries adapted from the Law of Root.
// Shared by the demo and the replay viewer. The panel is hidden by default and
// opens for the player's faction (demo) or the current turn player (analysis).
// Action names are wrapped in <strong>.

const HOWTO = {
  MC: {
    name: "Marquise de Cat",
    goal: "Reach 30 VP (mostly buildings and crafted items), or win by playing a Dominance card.",
    sections: [
      ["Setup", "Place the Keep in a corner clearing. Put 1 warrior in every other clearing, then place 1 sawmill, 1 workshop and 1 recruiter in the Keep's clearing or one adjacent."],
      ["Birdsong", "Place 1 wood in each clearing that has any sawmills (one wood per sawmill there)."],
      ["Daylight", "First craft with workshops. Then take 3 actions, plus 1 per bird card spent: <strong>March</strong> (up to two moves), <strong>Recruit</strong> (once per turn), <strong>Build</strong> (spend wood; score the build track), <strong>Overwork</strong> (spend a matching card for wood at a sawmill)."],
      ["Evening", "Draw 1 card plus the uncovered recruiter draw bonus (up to 3), then discard down to 5."],
      ["Rules", "You rule with warriors + buildings. Only you may place pieces in the Keep clearing. <strong>Field Hospitals</strong>: spend a card matching a clearing to move your warriors removed there back to the Keep."],
    ],
  },
  ED: {
    name: "Eyrie Dynasties",
    goal: "Reach 30 VP. Roosts score every Evening based on how many you have.",
    sections: [
      ["Setup", "Place a roost and 6 warriors in the corner opposite the Marquise, then choose a leader. Each leader pins Loyal Viziers into specific Decree columns."],
      ["Birdsong", "Add a card to the <strong>Decree</strong>. If you have no roost on the map, place a new one."],
      ["Daylight", "Craft, then resolve the <strong>Decree</strong> left to right (<strong>Recruit</strong>, <strong>Move</strong>, <strong>Battle</strong>, <strong>Build</strong>). You must resolve every column and cannot end Daylight early."],
      ["Turmoil", "If any Decree card has no legal target you <strong>Turmoil</strong>: lose 1 VP per bird in the Decree, purge non-vizier cards, depose the leader, choose a new leader, and Daylight ends."],
      ["Evening", "Score VP for your roosts (by count) and draw cards."],
    ],
  },
  WA: {
    name: "Woodland Alliance",
    goal: "Reach 30 VP. Placing sympathy scores VP, and bases unlock recruiting.",
    sections: [
      ["Setup", "Draw 3 cards and place them as supporters."],
      ["Birdsong", "<strong>Revolt</strong> (spend matching supporters to place a base and spark a revolt) or <strong>Spread sympathy</strong> into an eligible clearing."],
      ["Daylight", "<strong>Mobilize</strong> (move a supporter to your hand) and <strong>Train</strong> (spend a matching card to gain an officer)."],
      ["Evening", "Military Operations, one per officer: <strong>Organize</strong> (place sympathy and score), <strong>Move</strong>, <strong>Battle</strong>, <strong>Recruit</strong>. Then draw cards."],
      ["Rules", "<strong>Outrage</strong>: whenever another faction removes your sympathy, they must hand you a matching card."],
    ],
  },
  VB: {
    name: "Vagabond",
    goal: "Reach 30 VP from relationships, quests and infamy.",
    sections: [
      ["Setup", "Choose a character, place your pawn in any forest, take your starting items, and set every relationship to Indifferent."],
      ["Birdsong", "<strong>Refresh</strong>: flip up 3 exhausted items, plus 2 per tea on your Refresh track (your choice), then <strong>Slip</strong> to an adjacent clearing or forest without a boot."],
      ["Daylight", "Exhaust items to act: <strong>Move</strong> (boot), <strong>Battle</strong> (sword), <strong>Explore</strong> (torch), <strong>Aid</strong> (any item), <strong>Quest</strong> (two listed items), <strong>Strike</strong> (crossbow), <strong>Repair</strong> (hammer), <strong>Craft</strong> (hammer), <strong>Special action</strong> (your character)."],
      ["Evening", "<strong>Rest</strong> (in a forest, repair and refresh damaged items), draw 1 plus 1 per coin, discard to 5, then check your item limit (6 plus 2 per bag)."],
      ["Relationships", "<strong>Aid</strong> advances a faction's marker one space after 1, 2 and 3 Aids in the same turn. Removing a warrior makes that faction Hostile; you then score <strong>Infamy</strong> for their pieces."],
    ],
  },
};

function howtoBodyHTML(f) {
  const h = HOWTO[f];
  if (!h) return `<p class="hint">No faction selected.</p>`;
  let html = `<p class="howto-goal"><b>Goal:</b> ${h.goal}</p>`;
  for (const [title, body] of h.sections) {
    html += `<section class="howto-sec"><h3>${title}</h3><p>${body}</p></section>`;
  }
  return html;
}

// HowTo is a collapsible sidebar, hidden by default. `faction` selects content.
function howtoRender(faction) {
  const body = document.getElementById("howtobody");
  const title = document.getElementById("howtotitle");
  if (!body) return;
  const h = HOWTO[faction];
  if (title) title.textContent = h ? h.name + " · how to play" : "How to play";
  body.innerHTML = howtoBodyHTML(faction);
}

function howtoIsOpen() {
  const el = document.getElementById("howto");
  return el && el.classList.contains("open");
}

function howtoToggle(faction) {
  const el = document.getElementById("howto");
  const back = document.getElementById("howtobackdrop");
  const btn = document.getElementById("togglehowto");
  if (!el) return;
  const open = !el.classList.contains("open");
  if (open) howtoRender(faction);
  el.classList.toggle("open", open);
  if (back) back.hidden = !open;
  if (btn) btn.setAttribute("aria-expanded", open ? "true" : "false");
}

function howtoClose() {
  const el = document.getElementById("howto");
  const back = document.getElementById("howtobackdrop");
  const btn = document.getElementById("togglehowto");
  if (el) el.classList.remove("open");
  if (back) back.hidden = true;
  if (btn) btn.setAttribute("aria-expanded", "false");
}
