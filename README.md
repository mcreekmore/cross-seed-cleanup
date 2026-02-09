<p align="center">
  <img src="assets/cross-seed.png" width="200" />
</p>

<h1 align="center">cross-seed-cleanup</h1>

<p align="center">
  Identify and tag qBittorrent torrents that exist only for cross-seeding and aren't hardlinked into your media library.
</p>

<p align="center">
  <a href="https://github.com/mcreekmore/cross-seed-cleanup/actions/workflows/docker-publish.yml"><img src="https://github.com/mcreekmore/cross-seed-cleanup/actions/workflows/docker-publish.yml/badge.svg" alt="Build"></a>
  <a href="https://github.com/mcreekmore/cross-seed-cleanup/releases/latest"><img src="https://img.shields.io/github/v/release/mcreekmore/cross-seed-cleanup" alt="Release"></a>
  <a href="https://goreportcard.com/report/github.com/mcreekmore/cross-seed-cleanup"><img src="https://goreportcard.com/badge/github.com/mcreekmore/cross-seed-cleanup" alt="Go Report Card"></a>
  <a href="https://github.com/mcreekmore/cross-seed-cleanup/blob/main/LICENSE"><img src="https://img.shields.io/github/license/mcreekmore/cross-seed-cleanup" alt="License"></a>
</p>

---

## Overview

Tools like qbit_manage can detect torrents with no hardlinks, but once cross-seed is in the mix those checks become unreliable. Cross-seeded torrents hardlink to each other, making them appear linked even when nothing points back to your media library.

**cross-seed-cleanup** solves this by comparing filesystem hardlink counts against the number of qBittorrent torrents referencing the same inodes. Torrents whose files are only linked by other torrents (and not by Sonarr, Radarr, etc.) are tagged for easy bulk removal, helping you reclaim disk space.

## Features

- Inode-level hardlink analysis to detect externally linked files
- Flexible filtering by tag, category, and torrent age
- Dry-run mode enabled by default for safe operation
- Optional cron scheduling for recurring cleanup
- Lightweight Docker image (Alpine-based)

## Quick Start

```bash
docker run --rm \
  -v /mnt/user/data:/data:ro \
  -e QB_HOST=192.168.1.100 \
  -e QB_PASSWORD=yourpassword \
  -e DRY_RUN=true \
  ghcr.io/mcreekmore/cross-seed-cleanup:latest
```

Set `DRY_RUN=false` to apply tags.

## Configuration

| Variable             | Default           | Description                                             |
| -------------------- | ----------------- | ------------------------------------------------------- |
| `QB_HOST`            | `localhost`       | qBittorrent Web UI host                                 |
| `QB_PORT`            | `8080`            | qBittorrent Web UI port                                 |
| `QB_USERNAME`        | `admin`           | qBittorrent Web UI username                             |
| `QB_PASSWORD`        |                   | qBittorrent Web UI password                             |
| `TAG_REMOVABLE`      | `cross-seed-only` | Tag applied to removable torrents                       |
| `EXCLUDE_TAGS`       | `pinned,keep`     | Tags to skip (comma-separated)                          |
| `EXCLUDE_CATEGORIES` |                   | Categories to skip (comma-separated)                    |
| `INCLUDE_CATEGORIES` |                   | Only process these categories (comma-separated)         |
| `MIN_AGE_DAYS`       | `0`               | Minimum torrent age in days                             |
| `DRY_RUN`            | `true`            | Report only; set `false` to apply tags                  |
| `SCHEDULE`           |                   | Cron expression for recurring runs (e.g. `0 */6 * * *`) |
| `RUN_ON_START`       | `true`            | Run immediately before entering cron schedule           |

## Unraid

An [Unraid Community Applications](https://github.com/mcreekmore/cross-seed-cleanup) template is included (`cross-seed-cleanup.xml`). Install via the Apps tab or add the template manually.

## How It Works

1. **Stat** - Retrieves all torrents from qBittorrent and stats each file to collect inode/hardlink data.
2. **Classify** - Compares each file's hardlink count (`nlink`) against the number of torrents referencing the same inode. Files with external hardlinks are kept.
3. **Tag** - Torrents with no externally linked files are tagged as removable for bulk deletion in qBittorrent.
