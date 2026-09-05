(() => {
  const cfg = window.FR_MUSIC || {};
  const invite = (cfg.inviteURL || "").trim();

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

  // Fake now-playing demo — serene progress animation.
  const player = document.getElementById("demo-player");
  const fill = document.getElementById("demo-fill");
  const dot = document.getElementById("demo-dot");
  const current = document.getElementById("demo-current");
  const label = document.getElementById("demo-label");
  const toggle = document.getElementById("demo-toggle");
  if (!player || !fill || !dot || !current || !label || !toggle) return;

  const totalSec = 213;
  let elapsed = 42;
  let paused = false;

  const format = (sec) => {
    const m = Math.floor(sec / 60);
    const s = Math.floor(sec % 60);
    return `${m}:${String(s).padStart(2, "0")}`;
  };

  const render = () => {
    const pct = Math.min(100, (elapsed / totalSec) * 100);
    fill.style.width = `${pct}%`;
    dot.style.left = `${pct}%`;
    current.textContent = format(elapsed);
    label.textContent = paused ? "Paused" : "Now playing";
    toggle.textContent = paused ? "Resume" : "Pause";
    player.classList.toggle("is-paused", paused);
  };

  toggle.addEventListener("click", () => {
    paused = !paused;
    render();
  });

  render();
  window.setInterval(() => {
    if (paused) return;
    elapsed += 1;
    if (elapsed >= totalSec) elapsed = 0;
    render();
  }, 1000);
})();
