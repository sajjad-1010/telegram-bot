# Implementation Plan — Easy + Medium Features

Target: implement the **Easy** and **Medium** feature sets on the existing Go Telegram bot.

## Existing building blocks (reuse, don't rebuild)

- **DB**: SQLite via `modernc.org/sqlite`, `db.DB` global. Tables created in `db/Init` (`db/db.go`) through `ensure*Table()` functions. Follow that exact pattern for new tables.
- **Audience**: `db/audience.go` — `audience_contacts` table already stores every user/chat (`UpsertAudienceContact`). Broadcast + stats read from here.
- **Admin gate**: `isAdmin(user)` in `handlers.go:1134` (checks `ADMIN_USER_ID` + optional `ADMIN_USERNAME`). Reuse for every new admin command.
- **Command routing**: `HandleUpdate` → `switch update.Message.Command()` at `handlers.go:102`. Add new `case` arms here.
- **Redis**: `bot/state/pending_store.go` already wires a Redis client with RAM fallback. Reuse the same client for rate limiting.
- **Link matcher**: `extractSupportedMediaLink` (`handlers.go:253`) returns `(link, platform, ok)`. Add platforms here. NOTE: TikTok already supported.
- **Config**: `config.GetEnv(key, fallback)`. All new knobs go through it + `env-example`.
- **Media flow**: `handleMediaRequest` → yt-dlp download → MP4/MP3/both inline buttons (`callbackMediaOptionPrefix`). Download happens in `providers/instagram`.

---

## EASY

### E1 — `/help` command
- **Where**: new `case "help"` in `handlers.go:102` switch → `handleHelp(bot, msg)`.
- **What**: static text listing supported platforms, how deep-links work, admin commands (only if `isAdmin`).
- Replace/expand the thin `/start` text too.
- **Effort**: ~20 min. No DB.

### E2 — `/stats` (admin only)
- **Where**: new `case "stats"`, gated by `isAdmin`.
- **DB**: add `db/stats.go` with read queries:
  - total unique users: `SELECT COUNT(DISTINCT user_id) FROM audience_contacts`
  - total chats, split by `chat_type`
  - new download-count table (see E4 dependency): total downloads, per-platform breakdown.
- **What**: format one message with the numbers. Users now, downloads all-time + per platform.
- **Effort**: ~1 hr. Depends on E4 for download counters (users/chats work standalone).

### E3 — Per-user rate limit
- **Where**: check at top of `handleMediaRequest`.
- **How**: Redis `INCR` key `rl:{userID}:{yyyymmddhh}` with `EXPIRE`. If count > limit → reply "slow down, try later" and return. RAM fallback if Redis down (reuse pending_store fallback approach).
- **Config**: `RATE_LIMIT_PER_HOUR=30` (0 = disabled). Admins bypass.
- **Effort**: ~1.5 hr.

### E4 — Download event logging (backs E2 stats + E7 cache)
- **DB**: add `db/downloads.go`, table `downloads`:
  ```sql
  CREATE TABLE IF NOT EXISTS downloads (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      user_id INTEGER NOT NULL,
      platform TEXT NOT NULL,
      link TEXT NOT NULL,
      mode TEXT,                -- mp4 / mp3 / both
      status TEXT NOT NULL,     -- ok / failed
      created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
  );
  CREATE INDEX IF NOT EXISTS idx_downloads_link ON downloads(link);
  ```
- Register `ensureDownloadsTable()` in `db/Init`.
- Call `db.LogDownload(...)` after each send attempt (success + failure) in the media flow.
- **Effort**: ~1 hr.

---

## MEDIUM

### M1 — More platforms (Twitter/X, Pinterest)
- **Where**: `extractSupportedMediaLink` (`handlers.go:253`). Add host arms:
  - `twitter.com`, `x.com`, `mobile.twitter.com` → return `"Twitter"`.
  - `pinterest.com`, `*.pinterest.com`, `pin.it` → return `"Pinterest"`.
- yt-dlp already handles these; only URL matching + platform label needed.
- Verify the download path in `providers/instagram` is generic enough (it drives yt-dlp by URL) — if it branches on platform name, add the new labels to the generic branch.
- **Effort**: ~1.5 hr incl. testing each host.

### M2 — Download cache (Telegram file_id reuse)
- **Idea**: after a successful upload, Telegram returns a `file_id`. Store it keyed by `(link, mode)`. Next identical request → `SendVideo/SendAudio` with the cached `file_id` instead of re-downloading. Instant + free.
- **DB**: add `db/media_cache.go`, table:
  ```sql
  CREATE TABLE IF NOT EXISTS media_cache (
      link TEXT NOT NULL,
      mode TEXT NOT NULL,       -- mp4 / mp3
      file_id TEXT NOT NULL,
      file_type TEXT NOT NULL,  -- video / audio / photo / document
      created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
      PRIMARY KEY (link, mode)
  );
  ```
- **Flow**:
  1. In media send path, before download: `GetCachedMedia(link, mode)`. Hit → send by file_id, done.
  2. After a fresh successful upload: capture returned `file_id` from the send result, `UpsertMediaCache(...)`.
- Use the **normalized** link as key (Instagram already normalized via `normalizeInstagramLink`; normalize others too so `?utm=` junk doesn't fragment the cache).
- **Config**: `MEDIA_CACHE_ENABLED=true`.
- **Note**: file_id is bot-specific and can expire; on a "file_id invalid" send error, delete the cache row and fall back to a fresh download.
- **Effort**: ~3 hr (the send-result → file_id capture is the fiddly part per media type).

### M3 — Quality picker
- **Where**: extend the inline-button flow. Today: MP4 / MP3 / both. Add a step (or extra buttons) for MP4: **1080p / 720p / 480p**.
- **How**: pass the chosen height into yt-dlp format selector, e.g. `bestvideo[height<=720]+bestaudio/best[height<=720]` (current code uses `best[ext=mp4]/best` at `handlers.go:1251`).
- Encode quality into the callback payload (extend `callbackMediaOptionPrefix` data, e.g. `media_opt:mp4:720`).
- **Config**: `DEFAULT_VIDEO_QUALITY=best` (best|1080|720|480).
- **Effort**: ~3 hr (callback payload plumbing + format string).

### M4 — Multi-language (fa / en)
- **Problem**: all reply strings are hardcoded English across `handlers.go`.
- **Approach**:
  1. Add `internal/i18n/` (or `utils/i18n.go`) with a `map[lang]map[key]string` and `T(lang, key) string`.
  2. Move all user-facing strings to keys. Provide `en` + `fa`.
  3. Per-user language: add `language` column to `audience_contacts` (default from `DEFAULT_LANG`), `/lang fa` / `/lang en` command to set it.
  4. Resolve lang per request from the stored contact.
- **Config**: `DEFAULT_LANG=en`.
- **Effort**: ~4-5 hr (mostly the mechanical string extraction).

---

## Suggested build order

1. **E4** (downloads table) — foundation for stats + cache.
2. **E1** `/help`, **E2** `/stats` — quick wins, immediately visible.
3. **E3** rate limit — protects the bot before wider platform support.
4. **M1** more platforms — cheap, high value.
5. **M2** download cache — biggest performance win, needs E4's normalization thinking.
6. **M3** quality picker.
7. **E broadcast (optional stretch)** + **M4** i18n last — i18n touches every string, do it once features are stable.

## Cross-cutting conventions

- Every new table = an `ensure*Table()` registered in `db/Init` (`db/db.go`), mirroring existing ones.
- Every new config knob = added to `env-example` with a sane default.
- Every new admin command = `isAdmin` gate first, mirror `handleAdminRequiredChannelsCommand`.
- Keep download logging (E4) wrapping both success and failure paths so stats stay honest.
