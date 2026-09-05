# FR Music website (V1)

Public site for the Discord music bot.

- **URL:** https://music.frlabs.me
- **Root domain** `frlabs.me` stays free for other FR Labs projects

## What V1 includes

- Landing page (brand + invite)
- Commands / help
- Fake now-playing demo (look inspired by the Figma UI kit)
- Discord embeds link to the site (footer + **Website** button)

## Local preview

```bash
cd web
python3 -m http.server 4173
# open http://127.0.0.1:4173
```

Set the Discord invite in `web/config.js`:

```js
window.FR_MUSIC = {
  inviteURL: "https://discord.com/oauth2/authorize?client_id=YOUR_APP_ID&permissions=3148800&scope=bot%20applications.commands",
  siteURL: "https://music.frlabs.me",
};
```

## Deploy on the Azure VM

### 1. Namecheap DNS

Add an **A** record:

| Host  | Type | Value           | TTL  |
|-------|------|-----------------|------|
| music | A    | `YOUR_VM_IP`    | Auto |

Do **not** point `@` at the bot unless you want the root domain used.

### 2. Azure

- Public IP → **Static**
- NSG inbound: **80** and **443** (SSH 22 unchanged)

### 3. Start the site (same VM as the bot)

```bash
cd ~/dcBot
git pull
# paste invite URL into web/config.js
sudo docker compose --profile docker --profile warp --profile web up -d --build
sudo docker compose --profile web ps
```

Caddy auto-issues HTTPS for `music.frlabs.me`.

## Phase 2 (later — not built yet)

Live dashboard: Discord login → see real now playing / queue from the bot. Discuss after V1 is live.
