import os
import sys
import json
from pathlib import Path
from urllib.parse import urlparse
from urllib.request import Request, urlopen


def shortcode_from_link(link: str) -> str:
    parts = [part for part in urlparse(link).path.split("/") if part]
    if len(parts) < 2:
        raise ValueError("invalid instagram link")
    return parts[1]


def collect_files(root_dir: Path) -> list[str]:
    file_paths: list[str] = []
    for path in root_dir.rglob("*"):
        if not path.is_file():
            continue
        lower_name = path.name.lower()
        if lower_name.endswith((".json", ".xz", ".txt", ".part", ".temp")):
            continue
        file_paths.append(str(path))
    file_paths.sort()
    return file_paths


def to_plain(value):
    if value is None or isinstance(value, (str, int, float, bool)):
        return value
    if isinstance(value, list):
        return [to_plain(item) for item in value]
    if isinstance(value, dict):
        return {key: to_plain(item) for key, item in value.items()}
    if hasattr(value, "model_dump"):
        return to_plain(value.model_dump())
    if hasattr(value, "dict"):
        return to_plain(value.dict())
    if hasattr(value, "__dict__"):
        return to_plain({key: item for key, item in vars(value).items() if not key.startswith("_")})
    return value


def deep_get(data, path):
    current = data
    for key in path:
        if not isinstance(current, dict) or key not in current:
            return None
        current = current[key]
    return current


def first_non_empty(data, paths):
    for path in paths:
        value = deep_get(data, path)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return ""


def recursive_find_audio_url(node, path=()):
    if isinstance(node, dict):
        for key, value in node.items():
            found = recursive_find_audio_url(value, path + (str(key).lower(),))
            if found:
                return found
    elif isinstance(node, list):
        for idx, item in enumerate(node):
            found = recursive_find_audio_url(item, path + (str(idx),))
            if found:
                return found
    elif isinstance(node, str):
        lower_path = "/".join(path)
        if node.startswith(("http://", "https://")) and any(token in lower_path for token in ("music", "audio", "track", "asset")):
            return node
    return ""


def sanitize_filename(raw: str) -> str:
    out = []
    for ch in raw.strip():
        if ch.isalnum() or ch in ("-", "_", "."):
            out.append(ch)
        else:
            out.append("_")
    cleaned = "".join(out).strip("._")
    return cleaned or "instagram_audio"


def download_file(url: str, destination: Path) -> None:
    req = Request(url, headers={"User-Agent": "Mozilla/5.0"})
    with urlopen(req) as resp, destination.open("wb") as fh:
        fh.write(resp.read())


def run_instaloader(link: str, output_dir: Path) -> list[str]:
    import instaloader

    output_dir.mkdir(parents=True, exist_ok=True)
    loader = instaloader.Instaloader(
        dirname_pattern=str(output_dir / "{target}"),
        filename_pattern="{shortcode}_{mediaid}_{filename}",
        download_comments=False,
        download_geotags=False,
        download_video_thumbnails=False,
        save_metadata=False,
        compress_json=False,
        post_metadata_txt_pattern="",
        storyitem_metadata_txt_pattern="",
        quiet=True,
    )

    username = os.getenv("INSTAGRAM_USERNAME", "").strip()
    password = os.getenv("INSTAGRAM_PASSWORD", "").strip()
    sessionfile = os.getenv("INSTAGRAM_INSTALOADER_SESSIONFILE", "").strip()

    if sessionfile and username:
        loader.load_session_from_file(username, sessionfile)
    elif username and password:
        loader.login(username, password)

    shortcode = shortcode_from_link(link)
    post = instaloader.Post.from_shortcode(loader.context, shortcode)
    loader.download_post(post, target=shortcode)
    return collect_files(output_dir)


def run_instagrapi(link: str, output_dir: Path) -> list[str]:
    from instagrapi import Client

    output_dir.mkdir(parents=True, exist_ok=True)
    client = Client()

    settings_file = os.getenv("INSTAGRAM_INSTAGRAPI_SETTINGS_FILE", "").strip()
    session_id = os.getenv("INSTAGRAM_SESSION_ID", "").strip()
    username = os.getenv("INSTAGRAM_USERNAME", "").strip()
    password = os.getenv("INSTAGRAM_PASSWORD", "").strip()

    if settings_file and Path(settings_file).exists():
        client.load_settings(settings_file)

    if session_id:
        client.login_by_sessionid(session_id)
    elif username and password:
        client.login(username, password)
        if settings_file:
            client.dump_settings(settings_file)

    media_pk = client.media_pk_from_url(link)
    try:
        media = client.media_info(media_pk)
    except Exception:
        media = client.media_info_a1(media_pk)

    code = shortcode_from_link(link)
    if media.media_type == 8:
        for idx, resource in enumerate(media.resources or [], start=1):
            filename = f"{code}_{idx}"
            if getattr(resource, "media_type", None) == 2 and getattr(resource, "video_url", None):
                client.video_download_by_url(resource.video_url, filename, str(output_dir))
            elif getattr(resource, "thumbnail_url", None):
                client.photo_download_by_url(resource.thumbnail_url, filename, str(output_dir))
    elif media.media_type == 2 and getattr(media, "video_url", None):
        client.video_download_by_url(media.video_url, code, str(output_dir))
    elif getattr(media, "thumbnail_url", None):
        client.photo_download_by_url(media.thumbnail_url, code, str(output_dir))

    return collect_files(output_dir)


def run_instagrapi_audio(link: str, output_dir: Path) -> dict:
    from instagrapi import Client

    output_dir.mkdir(parents=True, exist_ok=True)
    client = Client()

    settings_file = os.getenv("INSTAGRAM_INSTAGRAPI_SETTINGS_FILE", "").strip()
    session_id = os.getenv("INSTAGRAM_SESSION_ID", "").strip()
    username = os.getenv("INSTAGRAM_USERNAME", "").strip()
    password = os.getenv("INSTAGRAM_PASSWORD", "").strip()

    if settings_file and Path(settings_file).exists():
        client.load_settings(settings_file)

    if session_id:
        client.login_by_sessionid(session_id)
    elif username and password:
        client.login(username, password)
        if settings_file:
            client.dump_settings(settings_file)

    media_pk = client.media_pk_from_url(link)
    try:
        media = client.media_info(media_pk)
    except Exception:
        media = client.media_info_a1(media_pk)

    data = to_plain(media)
    audio_url = first_non_empty(data, [
        ("clips_metadata", "music_info", "music_asset_info", "progressive_download_url"),
        ("clips_metadata", "music_info", "music_asset_info", "url"),
        ("music_metadata", "music_info", "music_asset_info", "progressive_download_url"),
        ("music_metadata", "music_info", "music_asset_info", "url"),
        ("music_metadata", "audio_asset_info", "progressive_download_url"),
        ("music_metadata", "audio_asset_info", "url"),
    ])
    if not audio_url:
        audio_url = recursive_find_audio_url(data)
    if not audio_url:
        raise RuntimeError("no attached instagram audio found")

    title = first_non_empty(data, [
        ("clips_metadata", "music_info", "music_asset_info", "title"),
        ("clips_metadata", "music_info", "music_asset_info", "display_title"),
        ("music_metadata", "music_info", "music_asset_info", "title"),
        ("music_metadata", "music_info", "music_asset_info", "display_title"),
    ])

    code = shortcode_from_link(link)
    ext = Path(urlparse(audio_url).path).suffix or ".m4a"
    filename = sanitize_filename(f"{code}_audio{ext}")
    destination = output_dir / filename
    download_file(audio_url, destination)

    return {
        "path": str(destination),
        "title": title,
    }


def main() -> int:
    if len(sys.argv) != 4:
        print("usage: instagram_fallback.py <backend> <link> <output_dir>", file=sys.stderr)
        return 2

    backend = sys.argv[1].strip().lower()
    link = sys.argv[2].strip()
    output_dir = Path(sys.argv[3]).resolve()

    try:
        if backend == "instaloader":
            file_paths = run_instaloader(link, output_dir)
        elif backend == "instagrapi":
            file_paths = run_instagrapi(link, output_dir)
        elif backend == "instagrapi-audio":
            result = run_instagrapi_audio(link, output_dir)
            print(json.dumps(result))
            return 0
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
