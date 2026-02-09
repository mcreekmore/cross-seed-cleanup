#!/usr/bin/env python3
"""
Identify torrents only used for cross-seeding with no external hardlinks
(i.e., not linked into a media library by Sonarr/Radarr/etc).

Algorithm:
  For each file in a torrent, compare:
    - st_nlink: total hardlink count on the filesystem
    - torrent_refs: number of torrents in qBittorrent sharing that same inode

  If st_nlink == torrent_refs for ALL files → nothing outside qBit uses it → tag as removable
  If st_nlink > torrent_refs for ANY file → external link exists (media library) → keep it
"""

import os
import sys
from collections import defaultdict
from pathlib import Path

try:
    import qbittorrentapi
except ImportError:
    print("Install dependency: pip install qbittorrent-api")
    sys.exit(1)

# ── Configuration ─────────────────────────────────────────────────────────────

QB_HOST = os.environ.get("QB_HOST", "localhost")
QB_PORT = int(os.environ.get("QB_PORT", 8080))
QB_USERNAME = os.environ.get("QB_USERNAME", "admin")
QB_PASSWORD = os.environ.get("QB_PASSWORD", "")

TAG_REMOVABLE = os.environ.get("TAG_REMOVABLE", "cross-seed-only")
EXCLUDE_TAGS = set(filter(None, os.environ.get("EXCLUDE_TAGS", "pinned,keep").split(",")))
EXCLUDE_CATEGORIES = set(filter(None, os.environ.get("EXCLUDE_CATEGORIES", "").split(",")))
INCLUDE_CATEGORIES = set(filter(None, os.environ.get("INCLUDE_CATEGORIES", "").split(",")))

DRY_RUN = os.environ.get("DRY_RUN", "true").lower() not in ("false", "0", "no")

# ── Main Logic ────────────────────────────────────────────────────────────────

def main():
    qbt = qbittorrentapi.Client(
        host=QB_HOST, port=QB_PORT,
        username=QB_USERNAME, password=QB_PASSWORD,
        FORCE_SCHEME_FROM_HOST=True,
    )
    try:
        qbt.auth_log_in()
    except qbittorrentapi.LoginFailed:
        print("ERROR: Failed to log in to qBittorrent. Check credentials.")
        sys.exit(1)

    print(f"Connected to qBittorrent {qbt.app.version}")

    torrents = qbt.torrents_info()
    print(f"Total torrents: {len(torrents)}")

    # ── Phase 1: Stat every file and build inode → torrent hash mapping ───

    # (device, inode) → set of torrent hashes that have a file at that inode
    inode_to_hashes: dict[tuple, set[str]] = defaultdict(set)

    # (torrent_hash, file_index) → ((dev, ino), st_nlink) or None
    file_info: dict[tuple, tuple | None] = {}

    skipped = 0
    total_files = 0

    for i, torrent in enumerate(torrents, 1):
        if i % 500 == 0 or i == len(torrents):
            print(f"  Scanning torrent {i}/{len(torrents)}...", end="\r")

        save_path = Path(torrent.save_path)

        for f in torrent.files:
            total_files += 1
            file_path = save_path / f.name

            try:
                st = file_path.stat()
                key = (st.st_dev, st.st_ino)
                inode_to_hashes[key].add(torrent.hash)
                file_info[(torrent.hash, f.index)] = (key, st.st_nlink)
            except (FileNotFoundError, PermissionError, OSError):
                file_info[(torrent.hash, f.index)] = None
                skipped += 1

    print(f"\nScanned {total_files} files across {len(torrents)} torrents ({skipped} inaccessible)")
    print(f"Unique inodes: {len(inode_to_hashes)}")

    # ── Phase 2: Classify each torrent ────────────────────────────────────

    removable = []
    kept = []
    skipped_torrents = []

    for torrent in torrents:
        # Apply exclusion filters
        torrent_tags = set(torrent.tags.split(", ")) if torrent.tags else set()
        if torrent_tags & EXCLUDE_TAGS:
            continue
        if EXCLUDE_CATEGORIES and torrent.category in EXCLUDE_CATEGORIES:
            continue
        if INCLUDE_CATEGORIES and torrent.category not in INCLUDE_CATEGORIES:
            continue

        has_files = False
        externally_linked = False

        for f in torrent.files:
            info = file_info.get((torrent.hash, f.index))
            if info is None:
                continue

            has_files = True
            inode_key, nlink = info
            torrent_refs = len(inode_to_hashes[inode_key])

            if nlink > torrent_refs:
                externally_linked = True
                break

        if not has_files:
            skipped_torrents.append(torrent)
            continue

        if externally_linked:
            kept.append(torrent)
        else:
            removable.append(torrent)

    # ── Phase 3: Report ───────────────────────────────────────────────────

    print(f"\n{'='*60}")
    print(f"  Externally linked (KEEP):        {len(kept)}")
    print(f"  Cross-seed only (REMOVABLE):     {len(removable)}")
    print(f"  No accessible files (SKIPPED):   {len(skipped_torrents)}")
    print(f"{'='*60}")

    if removable:
        print(f"\nRemovable torrents (no external hardlinks):\n")
        # Sort by size descending so biggest space savings are visible first
        removable.sort(key=lambda t: t.size, reverse=True)
        total_size = 0
        for t in removable:
            size_gb = t.size / (1024 ** 3)
            total_size += t.size
            cat = f"[{t.category}]" if t.category else ""
            print(f"  {size_gb:8.2f} GB  {cat:20s}  {t.name}")

        total_gb = total_size / (1024 ** 3)
        print(f"\n  Total reclaimable: {total_gb:.2f} GB")

        if not DRY_RUN:
            hashes = [t.hash for t in removable]
            qbt.torrents_add_tags(TAG_REMOVABLE, hashes)
            print(f"\n  Applied tag '{TAG_REMOVABLE}' to {len(removable)} torrents.")
        else:
            print(f"\n  DRY RUN — no changes made. Set DRY_RUN = False to apply tags.")
    else:
        print("\nAll torrents with files are externally linked. Nothing to tag.")


if __name__ == "__main__":
    main()
