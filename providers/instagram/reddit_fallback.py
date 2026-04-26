import html
import json
import mimetypes
import os
import subprocess
import sys
from pathlib import Path
from urllib.parse import urlparse
from urllib.request import Request, urlopen


def collect_files(root_dir: Path) -> list[str]:
    file_paths: list[str] = []
    for path in root_dir.rglob("*"):
        if not path.is_file():
            continue
        lower_name = path.name.lower()
        if lower_name.endswith((".json", ".txt", ".part", ".temp", ".xz", ".ytdl", ".db")):
            continue
        file_paths.append(str(path))
    file_paths.sort()
    return file_paths


def sanitize_filename(raw: str) -> str:
    out = []
    for ch in raw.strip():
        if ch.isalnum() or ch in ("-", "_", "."):
            out.append(ch)
        else:
            out.append("_")
    cleaned = "".join(out).strip("._")
    return cleaned or "reddit_media"


def submission_id_from_link(link: str) -> str:
    parsed = urlparse(link)
    host = parsed.netloc.lower()
    parts = [part for part in parsed.path.split("/") if part]

    if host.endswith("redd.it"):
        if not parts:
            raise ValueError("invalid redd.it link")
        return parts[0]

    if host.endswith("reddit.com"):
        lowered = [part.lower() for part in parts]
        if parts and lowered[0] == "gallery" and len(parts) >= 2:
            return parts[1]
        if "comments" in lowered:
            idx = lowered.index("comments")
            if idx + 1 < len(parts):
                return parts[idx + 1]

    raise ValueError("unable to extract reddit submission id from link")


def best_extension(url: str, mime_hint: str = "") -> str:
    suffix = Path(urlparse(url).path).suffix
    if suffix:
        return suffix
    if mime_hint:
        guessed = mimetypes.guess_extension(mime_hint)
        if guessed:
            return guessed
    return ".bin"


def download_file(url: str, destination: Path) -> None:
    req = Request(url, headers={"User-Agent": reddit_user_agent()})
    with urlopen(req) as resp, destination.open("wb") as fh:
        fh.write(resp.read())


def reddit_user_agent() -> str:
    value = os.getenv("REDDIT_USER_AGENT", "").strip()
    return value or "telegram-bot/1.0"


def resolve_reddit_link(link: str) -> str:
    parsed = urlparse(link)
    host = parsed.netloc.lower()
    if not host.endswith("reddit.com"):
        return link
    if "/s/" not in parsed.path.lower():
        return link

    req = Request(link, headers={"User-Agent": reddit_user_agent()})
    with urlopen(req) as resp:
        final_url = resp.geturl().strip()

    if not final_url:
        return link

    final_parsed = urlparse(final_url)
    if not final_parsed.scheme or not final_parsed.netloc:
        return link

    clean_path = final_parsed.path or "/"
    return f"{final_parsed.scheme}://{final_parsed.netloc}{clean_path}"


def fetch_reddit_post_json(link: str) -> dict:
    canonical_link = resolve_reddit_link(link).rstrip("/")
    json_url = canonical_link + "/.json?raw_json=1"
    req = Request(json_url, headers={"User-Agent": reddit_user_agent()})
    with urlopen(req) as resp:
        data = json.load(resp)
    if not isinstance(data, list) or not data:
        raise RuntimeError("unexpected reddit json payload")
    listing = data[0]
    post = (((listing or {}).get("data") or {}).get("children") or [{}])[0].get("data") or {}
    if not isinstance(post, dict) or not post:
        raise RuntimeError("reddit json did not contain post data")
    return post


def direct_media_url(post: dict) -> str:
    media = post.get("secure_media") or post.get("media") or {}
    if isinstance(media, dict):
        reddit_video = media.get("reddit_video") or {}
        if isinstance(reddit_video, dict):
            value = reddit_video.get("fallback_url")
            if isinstance(value, str) and value.strip():
                return html.unescape(value.strip())

    preview = post.get("preview") or {}
    if isinstance(preview, dict):
        reddit_video_preview = preview.get("reddit_video_preview") or {}
        if isinstance(reddit_video_preview, dict):
            value = reddit_video_preview.get("fallback_url")
            if isinstance(value, str) and value.strip():
                return html.unescape(value.strip())

        images = preview.get("images") or []
        if images and isinstance(images[0], dict):
            source = images[0].get("source") or {}
            value = source.get("url")
            if isinstance(value, str) and value.strip():
                return html.unescape(value.strip())

    for key in ("url_overridden_by_dest", "url"):
        value = post.get(key)
        if isinstance(value, str) and value.strip():
            clean = html.unescape(value.strip())
            if any(token in clean for token in ("i.redd.it", "v.redd.it", "preview.redd.it", "external-preview.redd.it")):
                return clean
    return ""


def run_reddownloader(link: str, output_dir: Path) -> list[str]:
    from RedDownloader import RedDownloader

    output_dir.mkdir(parents=True, exist_ok=True)
    canonical_link = resolve_reddit_link(link)
    base_name = sanitize_filename(submission_id_from_link(canonical_link) if "redd.it" in canonical_link or "reddit.com" in canonical_link else "reddit_download")

    try:
        RedDownloader.Download(canonical_link, output=base_name, destination=str(output_dir), verbose=False)
    except TypeError:
        RedDownloader.Download(canonical_link, output=base_name, destination=str(output_dir))

    return collect_files(output_dir)


def run_redditjson(link: str, output_dir: Path) -> list[str]:
    output_dir.mkdir(parents=True, exist_ok=True)
    post = fetch_reddit_post_json(link)
    base_name = sanitize_filename(str(post.get("id") or submission_id_from_link(resolve_reddit_link(link))))

    if post.get("is_gallery") and post.get("gallery_data") and post.get("media_metadata"):
        for idx, item in enumerate(post["gallery_data"].get("items", []), start=1):
            media_id = item.get("media_id")
            if not media_id:
                continue
            media = (post.get("media_metadata") or {}).get(media_id) or {}
            media_url = first_media_url(media)
            if not media_url:
                continue
            ext = best_extension(media_url, str(media.get("m", "")))
            destination = output_dir / f"{base_name}_{idx}{ext}"
            download_file(media_url, destination)
        files = collect_files(output_dir)
        if files:
            return files

    media_url = direct_media_url(post)
    if not media_url:
        raise RuntimeError("reddit json fallback found no downloadable media url")

    ext = best_extension(media_url)
    destination = output_dir / f"{base_name}{ext}"
    download_file(media_url, destination)
    return collect_files(output_dir)


def run_redditmedia(link: str, output_dir: Path) -> list[str]:
    client_id = os.getenv("REDDIT_CLIENT_ID", "").strip()
    client_secret = os.getenv("REDDIT_CLIENT_SECRET", "").strip()
    if not client_id or not client_secret:
        raise RuntimeError("reddit-media credentials not configured")

    output_dir.mkdir(parents=True, exist_ok=True)
    canonical_link = resolve_reddit_link(link)
    submission_id = submission_id_from_link(canonical_link)

    cmd = [
        sys.executable,
        "-m",
        "redditmedia",
        "-c",
        client_id,
        client_secret,
        "-p",
        str(output_dir),
        "-s",
        "get",
        submission_id,
    ]
    proc = subprocess.run(cmd, capture_output=True, text=True)
    if proc.returncode != 0:
        stderr = (proc.stderr or "").strip()
        stdout = (proc.stdout or "").strip()
        detail = stderr or stdout or f"exit status {proc.returncode}"
        raise RuntimeError(f"reddit-media failed: {detail}")

    return collect_files(output_dir)


def first_media_url(entry: dict) -> str:
    if not isinstance(entry, dict):
        return ""

    source = entry.get("s") or {}
    for key in ("u", "gif", "mp4"):
        value = source.get(key)
        if isinstance(value, str) and value.strip():
            return html.unescape(value.strip())

    previews = entry.get("p") or []
    if previews:
        preview = previews[-1]
        if isinstance(preview, dict):
            for key in ("u", "gif", "mp4"):
                value = preview.get(key)
                if isinstance(value, str) and value.strip():
                    return html.unescape(value.strip())

    return ""


def run_praw(link: str, output_dir: Path) -> list[str]:
    import praw

    client_id = os.getenv("REDDIT_CLIENT_ID", "").strip()
    client_secret = os.getenv("REDDIT_CLIENT_SECRET", "").strip()
    if not client_id or not client_secret:
        raise RuntimeError("praw credentials not configured")

    output_dir.mkdir(parents=True, exist_ok=True)
    reddit = praw.Reddit(
        client_id=client_id,
        client_secret=client_secret,
        user_agent=reddit_user_agent(),
    )
    canonical_link = resolve_reddit_link(link)
    submission = reddit.submission(url=canonical_link)
    _ = submission.title

    if getattr(submission, "is_gallery", False) and submission.gallery_data and submission.media_metadata:
        for idx, item in enumerate(submission.gallery_data.get("items", []), start=1):
            media_id = item.get("media_id")
            if not media_id:
                continue
            media = submission.media_metadata.get(media_id) or {}
            media_url = first_media_url(media)
            if not media_url:
                continue
            ext = best_extension(media_url, str(media.get("m", "")))
            destination = output_dir / f"{sanitize_filename(submission.id)}_{idx}{ext}"
            download_file(media_url, destination)
        return collect_files(output_dir)

    media = getattr(submission, "media", None) or getattr(submission, "secure_media", None) or {}
    reddit_video = media.get("reddit_video") if isinstance(media, dict) else None
    if isinstance(reddit_video, dict):
        video_url = reddit_video.get("fallback_url") or ""
        if video_url:
            video_url = html.unescape(video_url)
            destination = output_dir / f"{sanitize_filename(submission.id)}.mp4"
            download_file(video_url, destination)
            return collect_files(output_dir)

    direct_url = getattr(submission, "url_overridden_by_dest", "") or getattr(submission, "url", "")
    if isinstance(direct_url, str) and direct_url.strip():
        direct_url = html.unescape(direct_url.strip())
        ext = best_extension(direct_url)
        destination = output_dir / f"{sanitize_filename(submission.id)}{ext}"
        download_file(direct_url, destination)
        return collect_files(output_dir)

    raise RuntimeError("praw found no downloadable reddit media")


def main() -> int:
    if len(sys.argv) != 4:
        print("usage: reddit_fallback.py <backend> <link> <output_dir>", file=sys.stderr)
        return 2

    backend = sys.argv[1].strip().lower()
    link = sys.argv[2].strip()
    output_dir = Path(sys.argv[3]).resolve()

    try:
        if backend == "reddownloader":
            file_paths = run_reddownloader(link, output_dir)
        elif backend == "reddit-json":
            file_paths = run_redditjson(link, output_dir)
        elif backend == "reddit-media":
            file_paths = run_redditmedia(link, output_dir)
        elif backend == "praw":
            file_paths = run_praw(link, output_dir)
        else:
            print(f"unknown backend: {backend}", file=sys.stderr)
            return 2
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        return 1

    for file_path in file_paths:
        print(file_path)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
