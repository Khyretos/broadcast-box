<div align="center">

# 📡 Broadcast Box

**Sub-second live streaming in a box. Stream from OBS, watch in the browser, chat, clip and react together.**

[![License][license-image]][license-url]
[![Discord][discord-image]][discord-invite-url]

</div>

Broadcast Box lets you broadcast or screen-share to friends with sub-second latency. It uses WebRTC
([WHIP](https://obsproject.com/kb/whip-streaming-guide) to publish, WHEP to watch), so there is no transcoding and no
waiting: what you stream is what your viewers see, a fraction of a second later. It was designed to be simple to use and
easy to modify.

This fork adds a full community layer on top: live chat with emotes and GIFs, reactions, clips, Discord notifications
and Social Stream Ninja integration.

---

## ✨ Features

| | Feature | What it does |
|---|---|---|
| ⚡ | **Sub-second streaming** | WebRTC from OBS, FFmpeg, GStreamer or the browser, with simulcast and multi-view playback |
| 📊 | **Quality selector** | Every quality layer shows its detected resolution, frame rate and bitrate, e.g. `1080p @ 120fps, 12 Mb/s` |
| 💬 | **Live chat** | Per-stream chat with display names, built into the player |
| 😀 | **Emoji picker** | Every Unicode emoji, searchable, with each viewer's 50 most used ones first |
| 🐸 | **Emotes** | Twitch, 7TV, BetterTTV and FrankerFaceZ emotes, plus a live search of all of 7TV, BTTV and FFZ |
| 🎞️ | **GIFs** | Search your own [Slink](https://github.com/andrii-kryvoviaz/slink) servers, GIPHY and KLIPY, or paste a link |
| ❤️ | **Reactions** | Click to react, hold to pick any emoji or emote; scales to hundreds of viewers |
| ✂️ | **Clips** | Viewers clip the last minutes of a stream, trim it in an editor and publish it, stored locally or on S3 |
| 🔔 | **Notifications** | "Stream is live" / "Stream ended" messages on Discord, Slack, Mattermost, n8n and more |
| 🥷 | **Social Stream Ninja** | Forward chat to [Social Stream Ninja](https://github.com/steveseguin/social_stream) next to your other platforms |
| 🔐 | **Access control** | Reserved stream keys with tokens, an admin portal, and webhooks for your own authorization |
| 📈 | **Built to scale** | Every viewer has their own send queue, so one slow viewer never stalls the stream for the others |
| 🌍 | **Languages** | English and Danish |

---

## 📑 Table of Contents

- [🚀 Quick Start](#-quick-start)
- [🎥 Going Live](#-going-live)
  - [OBS](#obs) · [Browser](#browser) · [FFmpeg](#ffmpeg) · [GStreamer](#gstreamer)
- [👀 Watching](#-watching)
- [💬 Chat](#-chat)
  - [Emojis](#-emojis) · [Emotes](#-emotes) · [GIFs](#️-gifs) · [Reactions](#️-reactions)
- [✂️ Clips](#️-clips)
- [🔔 Stream Notifications](#-stream-notifications)
- [🥷 Social Stream Ninja](#-social-stream-ninja)
- [🔐 Access Control](#-access-control)
- [📈 Performance and Scaling](#-performance-and-scaling)
- [🛠️ Building From Source](#️-building-from-source)
- [🐳 Docker](#-docker)
- [⚙️ Configuration Reference](#️-configuration-reference)
- [🧪 Network Test on Start](#-network-test-on-start)
- [🧩 API Reference](#-api-reference)
- [🤝 Contributing](#-contributing)

---

## 🚀 Quick Start

The quickest way to run Broadcast Box is Docker Compose:

```shell
git clone https://github.com/Khyretos/broadcast-box.git
cd broadcast-box
cp .env.example .env      # then fill in the settings you want
docker compose up -d --build
```

The included [docker-compose.yml](./docker-compose.yml) expects:

- 🌐 **A reverse proxy** (nginx, Caddy, Traefik, ...) on a Docker network called `nginx-reverse-proxy_default`, which
  forwards your domain to `broadcast-box:8080`. Change the network name at the bottom of the file to match yours.
- 📶 **UDP port `8181`** open in your firewall. All WebRTC video goes through it.
- 📝 **`IP_ADDRESS`** in `.env` set to your server's public IP.

Everything else is optional. Each feature below lists the settings it needs, and
[.env.example](./.env.example) has all of them with examples.

> 💡 Just want to try it? Run `go run .` and open <http://localhost:8080>. See [Building From Source](#️-building-from-source).

---

## 🎥 Going Live

You can use any stream key you like. The same key is used for broadcasting and for watching: stream to `MyStream` and
it plays at `https://your-server.com/MyStream`. To stop others from using your key, [reserve it](#-access-control).

### OBS

Go to `Settings → Stream` and set:

| Setting | Value |
|---|---|
| Service | `WHIP` |
| Server | `https://your-server.com/api/whip` |
| Bearer Token | Your stream key (or your profile token if the key is [reserved](#-access-control)) |

![OBS Stream settings example](./.github/img/streamSettings.png)

OBS has about 2 seconds of latency by default. For sub-second latency go to `Settings → Output`, pick `x264` as encoder
and set tune to `zerolatency`:

![OBS Output settings example](./.github/img/outputPage.png)

Press `Start Streaming` and you're live! 🎉

> 💡 **Simulcast:** when OBS sends multiple quality layers, viewers can pick one in the player. Broadcast Box detects
> the resolution, frame rate and bitrate of each layer and shows them in the quality selector.

### Browser

Open `/publish/<streamKey>` to go live straight from the browser, with your screen or webcam. If your bearer token
belongs to a reserved profile, the page also lets you change the stream's message of the day and switch it between
public and private.

### FFmpeg

Broadcast `video-test.mp4` with the bearer token `ffmpeg-test`:

```shell
ffmpeg -re -i video-test.mp4 -bf 0 -f whip -authorization ffmpeg-test https://your-server.com/api/whip
```

FFmpeg's WHIP output supports H.264 and Opus, and uses them by default. Any H.264 encoder works, for example with
hardware encoding:

```shell
ffmpeg -hwaccel vulkan -re -i video-test.mp4 -bf 0 -vcodec h264_vaapi \
  -vf 'format=nv12|vulkan,hwupload' -init_hw_device vulkan \
  -f whip -authorization ffmpeg-test https://your-server.com/api/whip
```

Or broadcast a test pattern with a tone:

```shell
ffmpeg \
  -re \
  -f lavfi -i testsrc=size=1280x720 \
  -f lavfi -i sine=frequency=440 \
  -pix_fmt yuv420p -vcodec libx264 -profile:v baseline -r 25 -g 50 \
  -acodec libopus -ar 48000 -ac 2 \
  -f whip -authorization "ffmpeg-test" \
  "https://your-server.com/api/whip"
```

> ℹ️ WHIP was added in FFmpeg 8.

### GStreamer

See [examples/gstreamer-broadcast.sh](examples/gstreamer-broadcast.sh). It can broadcast GStreamer's test sources or
a webcam and microphone (`v4l2src` + `pulsesrc`), and needs `gstreamer-1.0` with the `good`, `bad` and `ugly` plugins
and `gst-plugins-rs`.

```shell
# Test sources
./examples/gstreamer-broadcast.sh http://localhost:8080/api/whip testStream1
# Webcam
./examples/gstreamer-broadcast.sh http://localhost:8080/api/whip testStream1 v4l2
```

---

## 👀 Watching

Open `https://your-server.com/<streamKey>`, or enter the stream key on the home page.

![Example of the player with 120 ms latency](./.github/img/broadcastView.png)

- 🖼️ **Multi-view:** use `Add Stream` below the player to watch several streams side by side.
- 🎬 **Cinema mode:** hides everything but the video. Toggle it below the player, or open `?cinemaMode=true`.
- 📊 **Quality selector:** each layer is labeled with what it really is, e.g. `0 - 1080p @ 120fps, 12 Mb/s`.
- 👥 **Viewer count:** shown in the player controls.
- 📈 **Statistics:** `/statistics` shows uptime, track metrics and WHEP session details of every public stream.

![Statistics](./.github/img/statistics.png)

---

## 💬 Chat

Every stream has its own live chat next to the player. Viewers pick a display name the first time they chat,
and every name gets its own color.
Messages travel over the same WebRTC connection as the video (a data channel), so chat is as fast as the stream.

The smiley button opens the **media picker** with three tabs: **Emoji**, **Emotes** and **GIFs**. Each tab has a
search box and remembers the 50 items each viewer uses most, in their own browser.

> 🔌 Building your own client? The chat protocol is documented in [CONNECTING.md](internal/chat/CONNECTING.md).

### 😀 Emojis

The emoji tab has every Unicode emoji, grouped by category and searchable by name and keyword ("fire", "party",
"thumbs"). The emoji data is only downloaded the first time a viewer opens the picker, so it doesn't slow down the page.

### 🐸 Emotes

Chat shows emotes from **7TV**, **BetterTTV**, **FrankerFaceZ** and **Twitch**. Typing an emote's name in a message
(e.g. `KEKW`) shows it as an image.

- 🌐 **Global emotes** of each provider are always available.
- 📺 **Channel emotes:** set `CHAT_EMOTES_TWITCH_IDS` to a Twitch user ID to add that channel's 7TV, BTTV and FFZ
  emotes, and its Twitch subscriber emotes when Twitch is set up. Use one ID for all streams (`12345678`) or one per
  stream key (`mystream:12345678,otherstream:87654321`). This is the numeric user ID, not the channel name.
- 🔎 **Emote search:** the Emotes tab searches all of 7TV, BTTV and FFZ as you type, with fuzzy matching, so
  `pepe` finds `PepeLaugh`, `pepeD` and friends. An emote picked from a search is sent along with the message, so
  every viewer sees it, even if it isn't one of the channel's emotes.
- ⏱️ Emote lists are cached on the server for 30 minutes, so viewers never hit the emote providers directly.

**Twitch emotes** (like `Kappa` and a channel's subscriber emotes) need a free Twitch application:

1. Go to [dev.twitch.tv/console](https://dev.twitch.tv/console) and register an application. Any OAuth redirect URL
   works, e.g. `http://localhost`.
2. Set `TWITCH_CLIENT_ID` and `TWITCH_CLIENT_SECRET` to its client ID and a new secret.

```env
CHAT_EMOTE_PROVIDERS=7tv,bttv,ffz
CHAT_EMOTES_TWITCH_IDS=12345678
TWITCH_CLIENT_ID=your-client-id
TWITCH_CLIENT_SECRET=your-client-secret
```

### 🎞️ GIFs

The GIFs tab lets viewers search for GIFs and send them into chat with one click. There are three GIF sources, and
any combination works:

| Source | Needs | When it's searched |
|---|---|---|
| 🏠 **[Slink](https://github.com/andrii-kryvoviaz/slink)**, your own image server | `SLINK_INSTANCES` | While typing. With an empty search it shows the newest images |
| 🟪 **GIPHY** | `GIPHY_API_KEY` | When the viewer presses **Enter** |
| 🟦 **KLIPY** | `KLIPY_API_KEY` | When the viewer presses **Enter** |

#### 🏠 Slink: your own GIF library

[Slink](https://github.com/andrii-kryvoviaz/slink) is a self-hosted image sharing server. List one or more of your
Slink servers and their public images show up in the GIF tab:

```env
SLINK_INSTANCES=gifs.example.com,https://memes.example.org
```

- 🔑 **No API key needed.** The search uses Slink's public image list. Slink's API keys are only for uploading, so a
  key added as `host:sk_...` is ignored (the server logs a warning).
- 🔓 **Guest access:** set `USER_ALLOW_UNAUTHENTICATED_ACCESS=true` on each Slink server, and make the images public.
  Private images are never shown.
- 📝 **Add descriptions:** Slink searches image descriptions and uploader names, not file names. A GIF described as
  "cat dance" is found by searching `cat`.
- ✅ Your Slink servers are allowed in chat automatically.

#### 🟪 GIPHY and 🟦 KLIPY

Get a free API key from [developers.giphy.com](https://developers.giphy.com/dashboard/) and/or
[partner.klipy.com](https://partner.klipy.com/api-keys), and paste it in:

```env
GIPHY_API_KEY=your-giphy-key
KLIPY_API_KEY=your-klipy-key
```

These services only allow a limited number of requests per hour (a free GIPHY key allows 100), so Broadcast Box uses
them sparingly:

- ⏎ Typing only searches your Slink servers. The picker shows *"Press Enter to also search GIPHY & KLIPY"*, and only
  pressing Enter asks them.
- 🗄️ Their results are cached on the server for an hour, and shared by all viewers.
- 🧮 The server makes at most `GIPHY_HOURLY_LIMIT` (default `90`) GIPHY searches and `KLIPY_HOURLY_LIMIT` (default
  `100`) KLIPY searches per hour. When a limit is reached, viewers see a short notice and still get the other results.
- 🔒 API keys stay on the server, viewers never see them.
- 🔞 `GIF_CONTENT_RATING` (`g`, `pg`, `pg-13` or `r`, default `pg-13`) sets how explicit results may be.
- ✅ GIPHY's hosts (`*.giphy.com`) and KLIPY's (`static.klipy.com`) are allowed in chat automatically.

#### 🔗 Pasting GIF links

Below the search results is a **Paste a GIF link** box. It accepts:

- 🖼️ A direct link to a GIF, from an allowed host (see below).
- 📄 A link to a GIF's **web page** on KLIPY (`https://klipy.com/gifs/cat-blink-9`) or GIPHY
  (`https://giphy.com/gifs/cat-dance-abc123`). The server finds the GIF file behind the page for you. A page link
  pasted in the search box works too.

#### ✅ Allowed GIF hosts

Only GIFs from trusted hosts are shown as images, so viewers can't post images from anywhere on the internet.
Your Slink servers, GIPHY and KLIPY are allowed automatically. Add other sites with `CHAT_GIF_HOSTS`:

```env
# One site, and a domain with all its subdomains
CHAT_GIF_HOSTS=gifs.example.com,*.imgur.com
```

`*.example.com` allows `example.com` and all its subdomains, `*` allows any https site. Links from other hosts are sent
as plain links.

### ❤️ Reactions

The heart button next to the chat box sends reactions that float over the video for everyone:

- 👆 **Click** to send the selected reaction.
- ✋ **Hold** to open a picker and choose any emoji or emote as your reaction.

The server counts reactions and sends the totals to all viewers four times per second, instead of forwarding every
single click, so reactions stay smooth with hundreds of viewers.

---

## ✂️ Clips

Viewers can clip the last minutes of a live stream, like on Twitch:

1. ✂️ Press the **scissors** button in the player. This saves a copy of the recent part of the stream.
2. 🎚️ In the editor, pick where the clip starts and ends, and give it a title (the date and time if left empty).
3. 📤 Press **Publish clip**.

Published clips are listed in the **clips panel**, opened with the film button next to the chat button. From there
they can be watched and downloaded. Deleting a clip needs the admin token or the token of the stream's profile.

Enable clips by choosing where to store them, a folder or S3-compatible storage (AWS S3, MinIO, Backblaze B2,
Cloudflare R2, ...):

```env
# A folder...
CLIP_STORAGE_PATH=/clips

# ...or S3
CLIP_S3_ENDPOINT=https://s3.eu-central-1.amazonaws.com
CLIP_S3_BUCKET=my-clips
CLIP_S3_ACCESS_KEY=...
CLIP_S3_SECRET_KEY=...
CLIP_S3_REGION=eu-central-1
```

How it works:

- 🧠 Each live stream keeps a rolling buffer of its best quality layer and its audio in memory, `CLIP_BUFFER_DURATION`
  long (default 2 minutes). That's about 90 MB per minute at 12 Mb/s.
- 🎬 Clips start at the keyframe at or just before the chosen start, so they always play from the first frame.
- 📦 Clips are saved as Matroska (`<streamKey>/<id>.mkv`) with a small `<id>.json` holding the title and details.
  The video is not re-encoded, so a clip has the full stream quality.
- 🎞️ Supported for H.264, AV1, VP8 and VP9 video with Opus audio.
- ⏳ Drafts in the editor expire after 10 minutes if they're not published.

> ⚠️ Clip previews play in Chromium-based browsers (Chrome, Edge, Brave, Opera). Firefox and Safari have limited
> Matroska support; viewers there can still create and download clips.

---

## 🔔 Stream Notifications

Broadcast Box can announce your streams in Discord (or Slack, Mattermost, n8n, ... anything that accepts a JSON
`content`, `text` or `message` field):

- 🔴 **Stream is Live:** posted when a stream starts, with a link to watch it.
- ⚫ **Stream Ended:** on Discord, the live message is edited to show the stream has ended and how long it lasted.
  Other services get a new message.
- 🔁 **No spam on reconnects:** a stream is only announced as ended after it has been offline for
  `NOTIFY_OFFLINE_GRACE_PERIOD` (default `60s`). If OBS reconnects within that time, the original message stays.

```env
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...
PUBLIC_URL=https://stream.example.com       # used for the "watch" link
NOTIFY_STREAM_KEYS=mystream                 # optional, all streams when empty
```

To get a Discord webhook URL: open your channel's settings → **Integrations** → **Webhooks** → **New Webhook** →
**Copy Webhook URL**.

---

## 🥷 Social Stream Ninja

Forward your Broadcast Box chat to [Social Stream Ninja](https://github.com/steveseguin/social_stream), so it shows
up in one place with your Twitch, YouTube and other chats:

```env
SSN_SESSION_ID=your-session-id
SSN_STREAM_KEYS=mystream        # optional, all chat is forwarded when empty
```

Messages are sent through the SSN API server with the stream key as source name. When the connection to SSN drops,
messages are queued and delivered when it's back. Set `SSN_VERBOSE=true` to log every forwarded message.

---

## 🔐 Access Control

By default anyone can stream to any stream key. Broadcast Box has three ways to control that.

### 🎟️ Stream profiles

A stream profile **reserves** a stream key: only someone with the profile's token can stream to it. Profiles also
hold the stream's message of the day and whether it's public (listed on the home page and in `/api/status`) or
private.

Create a profile in the admin portal, or from the command line:

```shell
go run . -createNewProfile -streamKey MyStream
```

This prints the token to use as bearer token in OBS. Profiles are stored in `STREAM_PROFILE_PATH`.

`STREAM_PROFILE_POLICY` decides who may stream:

| Value | Who can stream |
|---|---|
| `ANYONE_WITH_RESERVED` *(default)* | Reserved keys need their token. Keys that aren't reserved are open to anyone. |
| `RESERVED` | Only reserved keys, with their token. The most restrictive mode. |

### 🛡️ Admin portal

Set `FRONTEND_ADMIN_TOKEN` to enable the admin portal at `/admin`, logging in with that token. It shows:

- 📋 **Status:** all publishers and viewers, including private streams.
- 🎟️ **Profiles:** create profiles, rotate their tokens, remove them.
- 📜 **Logging:** the current server log.

![Admin Portal](./.github/img/adminPortal.png)

### 🪝 Webhooks

For your own authorization logic, set `WEBHOOK_URL`. Broadcast Box then calls it for every broadcaster and viewer
that connects, with this JSON:

| Field | Description |
|---|---|
| `action` | `whip-connect` for broadcasters, `whep-connect` for viewers |
| `bearerToken` | The token they connected with |
| `queryParams` | Query parameters of the request |
| `ip` | Their IP address |
| `userAgent` | Their user agent |

Reply `200 OK` with `{ "streamKey": "TheStreamKey" }` to allow the connection. Any other status rejects it. This lets
you use different keys for streaming and watching, log who connects, and more.

- [examples/webhook-server/main.go](examples/webhook-server/main.go) only allows the stream `broadcastBoxRulez`.
- [broadcastbox-webhookserver](https://github.com/chrisingenhaag/broadcastbox-webhookserver) separates the streaming
  key from the watching key.

---

## 📈 Performance and Scaling

Broadcast Box forwards the video as-is and never re-encodes it, so one server can serve many viewers. It's built to
keep the stream smooth for everyone:

- 🚦 **A send queue per viewer:** a viewer with a slow connection only delays themselves, never the broadcaster or
  the other viewers.
- 🧮 **Chat and reactions are rate-limited and batched**, so busy chats don't flood viewers.
- 👥 **Viewer cap:** `MAX_VIEWERS_PER_STREAM` limits viewers per stream to protect your upload bandwidth. Viewers over
  the limit get an error instead of a stuttering stream for everyone.

💡 **Tips for high bitrates** (4K, high frame rates, 10+ Mb/s):

- 📶 **Upload bandwidth** is the real limit: each viewer receives the full stream. 20 viewers of a 12 Mb/s stream need
  240 Mb/s upload. Simulcast lets viewers on slower connections pick a lower layer.
- 🧺 **UDP receive buffer:** bursts of large keyframes can overflow the UDP buffer, which shows as stutter.
  Broadcast Box asks for an 8 MiB buffer (`UDP_MUX_READ_BUFFER_SIZE`), but Linux caps it at `net.core.rmem_max`. Raise
  the cap on the host (not inside the container):

  ```shell
  sudo sysctl -w net.core.rmem_max=8388608
  echo 'net.core.rmem_max=8388608' | sudo tee /etc/sysctl.d/99-broadcast-box.conf   # keep it after reboots
  ```

---

## 🛠️ Building From Source

Broadcast Box has two parts: a **Go server** that handles WebRTC and the API, and a **React frontend** that the Go
server serves.

### Frontend

```shell
cd web
npm install
npm run build     # builds into web/build
```

- `npm start` runs the Vite dev server and proxies `/api` to the backend.
- `npm run host` does the same, reachable on your local network.

### Backend

```shell
go run .
```

```console
2026/02/24 12:00:00 Environment: Loading `.env.production`
2026/02/24 12:00:00 Starting HTTP server at :8080
```

Open `http://<YOUR_IP>:8080`, and broadcast to `http://<YOUR_IP>:8080/api/whip`.

The server loads [.env.production](./.env.production) by default, or [.env.development](./.env.development) with
`APP_ENV=development`. Set `DISABLE_FRONTEND=TRUE` to run only the API.

---

## 🐳 Docker

**Locally** (the UDP mux is needed because Docker on macOS and Windows runs behind a NAT):

```shell
docker run -e UDP_MUX_PORT=8080 -e NAT_1_TO_1_IP=127.0.0.1 -p 8080:8080 -p 8080:8080/udp seaduboi/broadcast-box
```

Then open <http://localhost:8080>.

**On a cloud server** (AWS and others), use host networking, as Broadcast Box listens on random UDP ports:

```shell
docker run --net=host -e INCLUDE_PUBLIC_IP_IN_NAT_1_TO_1_IP=yes seaduboi/broadcast-box
```

**With Docker Compose**, see [Quick Start](#-quick-start). The compose file keeps stream profiles in `./profiles` and
clips in `./clips` (or `CLIP_DIRECTORY`), so they survive rebuilds.

> ℹ️ The `seaduboi/broadcast-box` image is the upstream project. For the features of this fork, build the image
> yourself (`docker compose build`, or `docker build -t broadcast-box .`).

### Reverse proxies

`/api/sse` must not be buffered, or stream events and the video controls lag. In nginx:

```nginx
location /api/sse {
    proxy_pass http://<YOUR_IP>:<PORT>;
    # ... other configuration
    proxy_buffering off;
}
```

---

## ⚙️ Configuration Reference

All settings are environment variables. Set them in `.env` (with Docker Compose) or in `.env.production`.

<details>
<summary>🖥️ <b>Server</b></summary>

| Variable | Description |
|---|---|
| `APP_ENV` | `development` loads `.env.development` instead of `.env.production`. |
| `HTTP_ADDRESS` | Address the server listens on. Default `:8080`. HTTPS when certificates are set. |
| `ENABLE_HTTP_REDIRECT` | Redirect HTTP to HTTPS. |
| `HTTPS_REDIRECT_PORT` | Port for the HTTP-to-HTTPS redirect. |
| `NETWORK_TEST_ON_START` | `true` runs a [network test](#-network-test-on-start) at startup. |
| `DISABLE_STATUS` | Disables `/api/status`. Stream listing and `/statistics` need it. |
| `ENABLE_PROFILING` | `true` enables pprof on `localhost:6060`. |
| `SSL_CERT` / `SSL_KEY` | Certificate and key files. When both are set, the server serves HTTPS. |

</details>

<details>
<summary>🔐 <b>Authorization and frontend</b></summary>

| Variable | Description |
|---|---|
| `STREAM_PROFILE_PATH` | Folder for stream profiles. Default `profiles`. |
| `STREAM_PROFILE_POLICY` | `ANYONE_WITH_RESERVED` (default) or `RESERVED`. See [Stream profiles](#️-stream-profiles). |
| `WEBHOOK_URL` | Webhook that authorizes broadcasters and viewers. See [Webhooks](#-webhooks). |
| `FRONTEND_ADMIN_TOKEN` | Enables `/admin`, logging in with this token. Also allows deleting clips. |
| `DISABLE_FRONTEND` | Serve only the API, no web pages. |
| `FRONTEND_PATH` | Folder with the built frontend. Default `./web/build`. |

</details>

<details>
<summary>💬 <b>Chat, emotes and GIFs</b></summary>

| Variable | Description |
|---|---|
| `CHAT_MAX_HISTORY` | Chat messages kept per stream. |
| `CHAT_DEFAULT_TTL` | How long an idle chat stays alive. |
| `CHAT_CLEANUP_INTERVAL` | How often expired chats are cleaned up. |
| `CHAT_EMOTE_PROVIDERS` | Emote providers: `7tv`, `bttv` and/or `ffz`, comma separated. Twitch is added when set up. |
| `CHAT_EMOTES_TWITCH_IDS` | Twitch user ID for channel emotes: `12345678` for all streams, or `key:12345678,...` per stream. |
| `TWITCH_CLIENT_ID` | Client ID of a Twitch application, for Twitch emotes. |
| `TWITCH_CLIENT_SECRET` | Client secret of that Twitch application. |
| `CHAT_GIF_HOSTS` | Extra hosts whose GIF links are shown in chat, comma separated. `*.example.com` includes subdomains, `*` allows any https host. |
| `SLINK_INSTANCES` | Your Slink servers to search for GIFs, comma separated. |
| `GIPHY_API_KEY` | GIPHY API key, enables GIPHY search on Enter. |
| `GIPHY_HOURLY_LIMIT` | Most GIPHY searches per hour. Default `90`. |
| `KLIPY_API_KEY` | KLIPY API key, enables KLIPY search on Enter. |
| `KLIPY_HOURLY_LIMIT` | Most KLIPY searches per hour. Default `100`. |
| `GIF_CONTENT_RATING` | Highest rating for GIPHY and KLIPY results: `g`, `pg`, `pg-13` or `r`. Default `pg-13`. |

</details>

<details>
<summary>✂️ <b>Clips</b></summary>

| Variable | Description |
|---|---|
| `CLIP_STORAGE_PATH` | Folder to store clips in. Enables clips. |
| `CLIP_S3_BUCKET` | S3 bucket to store clips in instead. Enables clips. |
| `CLIP_S3_ENDPOINT` | S3 endpoint. Default `s3.amazonaws.com`. |
| `CLIP_S3_ACCESS_KEY` | S3 access key. |
| `CLIP_S3_SECRET_KEY` | S3 secret key. |
| `CLIP_S3_REGION` | S3 region. |
| `CLIP_S3_PREFIX` | Optional prefix for the clip files in the bucket. |
| `CLIP_S3_USE_SSL` | `false` to connect to S3 without TLS, e.g. a local MinIO. Default `true`. |
| `CLIP_BUFFER_DURATION` | How much of the stream can be clipped. Default `2m`. |
| `CLIP_MAX_DURATION` | Longest allowed clip. Default the buffer duration. |
| `CLIP_MAX_DRAFTS` | Clip drafts kept at once, each a temporary file of the whole buffer. Default `10`. |
| `CLIP_DRAFT_PATH` | Folder for clip drafts. Default a folder in the system's temp folder. |

</details>

<details>
<summary>🔔 <b>Notifications and Social Stream Ninja</b></summary>

| Variable | Description |
|---|---|
| `DISCORD_WEBHOOK_URL` | Webhook for live/ended messages. Notifications are off when empty. |
| `PUBLIC_URL` | Public address of your site, for the watch link. Without it only the stream key is posted. |
| `NOTIFY_OFFLINE_GRACE_PERIOD` | How long a stream may be offline before it's announced as ended. Default `60s`. |
| `NOTIFY_STREAM_KEYS` | Stream keys to announce, comma separated. All streams when empty. |
| `SSN_SESSION_ID` | Social Stream Ninja session ID. Forwarding is off when empty. |
| `SSN_STREAM_KEYS` | Stream keys whose chat is forwarded, comma separated. All when empty. (`WATCH_STREAM_KEY` still works too.) |
| `SSN_VERBOSE` | `true` logs every forwarded message. |

</details>

<details>
<summary>🌐 <b>WebRTC and networking</b></summary>

| Variable | Description |
|---|---|
| `UDP_MUX_PORT` | One UDP port for all WebRTC traffic. Random ports when unset. |
| `UDP_MUX_PORT_WHIP` | UDP port for broadcasters only. |
| `UDP_MUX_PORT_WHEP` | UDP port for viewers only. |
| `UDP_MUX_READ_BUFFER_SIZE` | UDP receive buffer in bytes. Default `8388608` (8 MiB), capped by `net.core.rmem_max`. |
| `MAX_VIEWERS_PER_STREAM` | Most viewers per stream, more get `503`. Unlimited when unset. |
| `NAT_1_TO_1_IP` | IPs to announce (like your public IP), separated by `\|`. |
| `INCLUDE_PUBLIC_IP_IN_NAT_1_TO_1_IP` | Detect and announce your public IP. |
| `NAT_ICE_CANDIDATE_TYPE` | `srflx` adds the IPs above instead of replacing the detected ones. |
| `INTERFACE_FILTER` | Only use this network interface. |
| `NETWORK_TYPES` | Network types to use, separated by `\|`, e.g. `udp4\|udp6`. |
| `INCLUDE_LOOPBACK_CANDIDATE` | Also use the loopback interface. |
| `TCP_MUX_ADDRESS` | Address to serve WebRTC over TCP. |
| `TCP_MUX_FORCE` | Only use TCP for WebRTC. |
| `APPEND_CANDIDATE` | Extra ICE candidates to announce. |
| `STUN_SERVERS` | STUN servers, separated by `\|`. Used by the server's own WebRTC connections. |

</details>

<details>
<summary>📜 <b>Logging and debugging</b></summary>

| Variable | Description |
|---|---|
| `LOGGING_ENABLED` | Write logs to files. |
| `LOGGING_LEVEL` | `DEBUG`, `INFO` (default), `WARN` or `ERROR`. |
| `LOGGING_DIRECTORY` | Folder for log files. |
| `LOGGING_SINGLEFILE` | Log to one file called `log` instead of one file per day. |
| `LOGGING_NEW_FILE_ON_STARTUP` | Start a new log file at every startup. |
| `LOGGING_API_ENABLED` | Enables `/api/log` with the current log. |
| `LOGGING_API_KEY` | Bearer token required for `/api/log`. |
| `DEBUG_PRINT_OFFER` | Print WebRTC offers from clients. |
| `DEBUG_PRINT_ANSWER` | Print WebRTC answers to clients. |
| `DEBUG_INCOMING_API_REQUEST` | Log incoming API request paths. |
| `DEBUG_PRINT_SSE_MESSAGES` | Log server-sent events. |

</details>

---

## 🧪 Network Test on Start

With `NETWORK_TEST_ON_START=true`, Broadcast Box checks at startup that WebRTC traffic can reach your server, and
exits if it can't:

```console
NETWORK_TEST_ON_START is enabled. If the test fails Broadcast Box will exit.
See the README.md for how to debug or disable NETWORK_TEST_ON_START
```

✅ When it passes:

```console
Network Test passed.
Have fun using Broadcast Box
```

❌ When it fails (the middle line explains why):

```console
Network Test failed.
Network Test client reported nothing in 30 seconds
Please see the README and join Discord for help
```

To debug, check:

- Is UDP traffic allowed through your firewall?
- Are there restrictions on ports?
- Is your server reachable from the internet?

[Join the Discord][discord-invite-url] if you're stuck. To skip the test, set `NETWORK_TEST_ON_START=false`.

---

## 🧩 API Reference

<details>
<summary>📺 <b>Streaming</b></summary>

| Endpoint | Description |
|---|---|
| `/api/whip` | Start broadcasting (WHIP). Needs `Authorization: Bearer <token>`. |
| `/api/whip/{sessionID}` | `PATCH` for trickle ICE, `DELETE` to stop. Same bearer token. |
| `/api/whip/profile` | `GET`/`POST` the profile (message of the day, public/private) of the bearer token. |
| `/api/whep` | Start watching (WHEP). Needs `Authorization: Bearer <streamKey>`. |
| `/api/whep/{sessionID}` | `PATCH` for trickle ICE. |
| `/api/sse/{sessionID}` | Server-sent events with stream status and available layers. |
| `/api/layer/{sessionID}` | Switch the quality layer of a viewer. |
| `/api/status` | Active public streams. `?key=<streamKey>` for one stream. |

</details>

<details>
<summary>💬 <b>Chat</b></summary>

| Endpoint | Description |
|---|---|
| `/api/chat/emotes?key=<streamKey>` | The stream's emotes, allowed GIF hosts and configured GIF sources. |
| `/api/chat/emotes/search?q=<query>` | Search 7TV, BetterTTV and FrankerFaceZ. |
| `/api/chat/gifs/search?q=<query>[&apis]` | Search the Slink servers, and GIPHY and KLIPY with `apis`. |
| `/api/chat/gifs/resolve?url=<link>` | The GIF file behind a KLIPY or GIPHY page link. |

Chat messages themselves travel over the `bb-chat-v1` WebRTC data channel, see
[CONNECTING.md](internal/chat/CONNECTING.md). A raw `bb-data-v1` channel is also available for your own messages, see
[DATA_CHANNEL.md](internal/webrtc/sessions/session/DATA_CHANNEL.md).

</details>

<details>
<summary>✂️ <b>Clips</b></summary>

| Endpoint | Description |
|---|---|
| `GET /api/clips/config` | Clip settings, `{"enabled": false}` when clips are off. |
| `GET /api/clips?key=<streamKey>` | Published clips of a stream, newest first. |
| `POST /api/clips/drafts?key=<streamKey>` | Save the stream's clip buffer as a draft. |
| `GET /api/clips/drafts/{id}` | A draft's video, for the editor. |
| `POST /api/clips/drafts/{id}/publish` | Publish a draft: `{"start", "end", "title"}`. |
| `GET /api/clips/{streamKey}/{id}` | A clip's video, `?download` to download it. |
| `DELETE /api/clips/{streamKey}/{id}` | Delete a clip. Needs the admin token or the stream profile's token. |

</details>

<details>
<summary>🛡️ <b>Admin and logging</b></summary>

All `/api/admin/*` endpoints need the `FRONTEND_ADMIN_TOKEN` bearer token.

| Endpoint | Description |
|---|---|
| `/api/admin/login` | Check the admin token. |
| `/api/admin/status` | Full session state, including private streams. |
| `/api/admin/profiles` | List stream profiles. |
| `/api/admin/profiles/add-profile` | Create a stream profile. |
| `/api/admin/profiles/remove-profile` | Remove a stream profile. |
| `/api/admin/profiles/reset-token` | Give a stream profile a new token. |
| `/api/admin/logging` | The current log file. |
| `/api/log` | The current log file, when `LOGGING_API_ENABLED=TRUE` (with `LOGGING_API_KEY` as bearer token if set). |

</details>

<details>
<summary>🗺️ <b>Web pages</b></summary>

| Route | Description |
|---|---|
| `/` | Home page: join a stream or start broadcasting. |
| `/{streamKey}` | Player with chat, reactions and clips. Add more streams for multi-view. |
| `/publish/{streamKey}` | Broadcast from the browser. |
| `/statistics` | Live statistics of all public streams. |
| `/admin` | Admin portal, when `FRONTEND_ADMIN_TOKEN` is set. |

</details>

### 📚 Examples

- [simple-watcher.html](./examples/simple-watcher.html): a minimal WHEP viewer without any framework.
- [dynamic-watcher.html](./examples/dynamic-watcher.html): polls `/api/status` and opens a viewer for every stream.
- [gstreamer-broadcast.sh](./examples/gstreamer-broadcast.sh): broadcast with GStreamer.
- [gstreamer-whep-to-rtmp.sh](./examples/gstreamer-whep-to-rtmp.sh): watch over WHEP and restream to RTMP.
- [webhook-server/main.go](./examples/webhook-server/main.go): a simple webhook authorization server.
- [recording/main.go](./examples/recording/main.go): a webhook-driven recorder that writes `.ogg` and `.h264` files.

---

## 🤝 Contributing

Want to help build Broadcast Box? See [CONTRIBUTING.md](./CONTRIBUTING.md), and come say hi on
[Discord][discord-invite-url]! 💜

[license-image]: https://img.shields.io/badge/License-MIT-yellow.svg
[license-url]: https://opensource.org/licenses/MIT
[discord-image]: https://img.shields.io/discord/1162823780708651018?logo=discord
[discord-invite-url]: https://discord.gg/An5jjhNUE3
