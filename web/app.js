(() => {
  const cfg = window.FR_MUSIC || {};
  const invite = (cfg.inviteURL || "").trim();
  const guildID = (cfg.guildID || "").trim();
  const apiBase = (cfg.apiBase || "").replace(/\/$/, "");

  const applyInvite = (el) => {
    if (!el) return;
    if (invite) {
      el.href = invite;
      el.target = "_blank";
      el.rel = "noopener noreferrer";
    } else {
      el.href = "#invite";
    }
  };

  ["invite-hero", "invite-nav", "invite-main"].forEach((id) => {
    applyInvite(document.getElementById(id));
  });

  const note = document.getElementById("invite-note");
  if (note && invite) {
    note.hidden = true;
  }

  const player = document.getElementById("demo-player");
  const fill = document.getElementById("demo-fill");
  const dot = document.getElementById("demo-dot");
  const currentEl = document.getElementById("demo-current");
  const totalEl = document.querySelector(".progress-times span:last-child");
  const label = document.getElementById("demo-label");
  const toggle = document.getElementById("demo-toggle");
  const titleEl = document.querySelector(".player-title");
  const artistEl = document.querySelector(".player-artist");
  const footerEl = document.querySelector(".player-footer");
  const albumArt = document.querySelector(".album-art");
  const sectionHint = document.querySelector("#demo .section-head p");

  if (!player || !fill || !dot || !currentEl || !label || !toggle) return;

  let liveMode = false;
  let totalSec = 213;
  let elapsed = 42;
  let paused = false;

  const format = (sec) => {
    const m = Math.floor(sec / 60);
    const s = Math.floor(sec % 60);
    return `${m}:${String(s).padStart(2, "0")}`;
  };

  const renderProgress = () => {
    const pct = totalSec > 0 ? Math.min(100, (elapsed / totalSec) * 100) : 0;
    fill.style.width = `${pct}%`;
    dot.style.left = `${pct}%`;
    currentEl.textContent = format(elapsed);
    if (totalEl) totalEl.textContent = totalSec > 0 ? format(totalSec) : "—";
    label.textContent = paused ? "Paused" : "Now playing";
    toggle.textContent = paused ? "Resume" : "Pause";
    player.classList.toggle("is-paused", paused);
  };

  const applyLive = (doc) => {
    liveMode = true;
    if (sectionHint) {
      sectionHint.textContent = doc.guild_name
        ? `Live from ${doc.guild_name}`
        : "Live from your Discord server";
    }
    if (!doc.now) {
      label.textContent = "Player";
      if (titleEl) titleEl.textContent = "Nothing playing";
      if (artistEl) artistEl.textContent = "Use /play in Discord";
      if (footerEl) footerEl.textContent = "music.frlabs.me";
      paused = false;
      elapsed = 0;
      totalSec = 0;
      renderProgress();
      return;
    }

    paused = !!doc.paused;
    if (titleEl) titleEl.textContent = doc.now.title || "Unknown track";
    if (artistEl) artistEl.textContent = doc.now.artist || "";
    totalSec = doc.now.duration_sec || 0;
    if (footerEl) {
      const bits = [];
      if (doc.now.requester) bits.push("Requested by " + doc.now.requester);
      if (doc.upcoming_count > 0) bits.push(doc.upcoming_count + " in queue");
      bits.push("Live");
      footerEl.textContent = bits.join(" · ");
    }
    if (albumArt && doc.now.thumbnail) {
      albumArt.style.backgroundImage = `url("${doc.now.thumbnail}")`;
      albumArt.style.backgroundSize = "cover";
      albumArt.style.backgroundPosition = "center";
    }
    renderProgress();
  };

  const fetchLive = async () => {
    if (!guildID) return false;
    try {
      const res = await fetch(`${apiBase}/api/guilds/${guildID}/now`, {
        headers: { Accept: "application/json" },
      });
      if (!res.ok) return false;
      const doc = await res.json();
      applyLive(doc);
      return true;
    } catch (_) {
      return false;
    }
  };

  toggle.addEventListener("click", () => {
    if (liveMode) return; // controls live in Discord for now
    paused = !paused;
    renderProgress();
  });

  renderProgress();

  fetchLive().then((ok) => {
    if (!ok && sectionHint) {
      sectionHint.textContent =
        "Demo preview — live track appears after the bot syncs Mongo.";
    }
  });

  window.setInterval(() => {
    if (liveMode) {
      fetchLive();
      return;
    }
    if (paused) return;
    elapsed += 1;
    if (elapsed >= totalSec) elapsed = 0;
    renderProgress();
  }, 1000);

  // Refresh live state every 8s as well (in case interval overlaps with tick).
  window.setInterval(() => {
    if (liveMode || guildID) fetchLive();
  }, 8000);
})();
