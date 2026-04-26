# Telegram Bot

## Run Redis with Docker

Use this command:

```bash
docker run -d --name tg-bot-redis -p 6379:6379 --restart unless-stopped redis:7-alpine
```

Optional check:

```bash
docker ps
docker logs tg-bot-redis
```

## Env config for Redis

Set these in `.env`:

```env
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
REDIS_DB=0
REDIS_PENDING_TTL_MINUTES=15
```

If Redis is down/unreachable, bot falls back to RAM store and writes fallback reason in logs.

## Instagram cookies

If Instagram returns empty playlists or missing items, configure one of these in `.env`:

```env
INSTAGRAM_COOKIES_FROM_BROWSER=chrome
INSTAGRAM_COOKIES_FILE=
```

Use only one of them.

Instagram fallback downloader:

```bash
py -m pip install -U -r requirements.txt
```

Optional explicit binary path:

```env
INSTAGRAM_GALLERY_DL_BIN=
INSTAGRAM_PYTHON_BIN=
INSTAGRAM_USERNAME=
INSTAGRAM_PASSWORD=
INSTAGRAM_SESSION_ID=
INSTAGRAM_INSTALOADER_SESSIONFILE=
INSTAGRAM_INSTAGRAPI_SETTINGS_FILE=
INSTAGRAM_FALLBACK_SCRIPT=
```

## Reddit fallback downloader

Reddit downloader chain:

- `RedDownloader` primary
- `reddit-media` fallback
- `praw` fallback

Install:

```bash
py -m pip install -U -r requirements.txt
```

Optional `.env` values for Reddit fallback:

```env
REDDIT_PYTHON_BIN=
REDDIT_FALLBACK_SCRIPT=
REDDIT_CLIENT_ID=
REDDIT_CLIENT_SECRET=
REDDIT_USER_AGENT=
REDDIT_DOWNLOAD_TIMEOUT_SECONDS=
```

Notes:

- `RedDownloader` does not require Reddit API credentials for the primary path.
- `reddit-media` and `praw` fallback need `REDDIT_CLIENT_ID` and `REDDIT_CLIENT_SECRET`.

## Reddit scheduled feed

The bot can publish one post for each Reddit topic below on a schedule:

- `latest`
- `top`
- `hot`

Config:

```env
REDDIT_FEED_ENABLED=false
REDDIT_FEED_SUBREDDIT=
REDDIT_FEED_CHAT_ID=
REDDIT_FEED_INTERVAL_HOURS=24
REDDIT_FEED_FETCH_LIMIT=25
REDDIT_FEED_POSTS_PER_TOPIC=3
REDDIT_FEED_TOP_WINDOW=day
REDDIT_FEED_POLL_MINUTES=15
```

Behavior:

- state is stored in SQLite, so already-sent posts are not sent again for the same topic
- native Reddit media is downloaded and uploaded to Telegram
- external media is sent as a link only
- after every published post, the bot sends a second message with only the topic name
- default behavior is `3` posts for each topic per run
