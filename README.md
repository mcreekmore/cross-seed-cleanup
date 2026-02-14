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
  <a href="https://goreportcard.com/report/github.com/mcreekmore/cross-seed-cleanup"><img src="https://goreportcard.com/badge/github.com/mcreekmore/cross-seed-cleanup?v=1" alt="Go Report Card"></a>
  <a href="https://github.com/mcreekmore/cross-seed-cleanup/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License"></a>
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

| Variable             | Default           | Required | Description                                             |
| -------------------- | ----------------- | -------- | ------------------------------------------------------- |
| `QB_HOST`            | `localhost`       | No       | qBittorrent Web UI host (or qui)                        |
| `QB_PORT`            | `8080`            | No       | qBittorrent Web UI port (or qui)                        |
| `QB_USERNAME`        | `admin`           | No       | qBittorrent Web UI username (not needed for qui)        |
| `QB_PASSWORD`        |                   | Yes*     | qBittorrent Web UI password (not needed for qui)        |
| `QB_API_KEY`         |                   | Yes*     | qui API key (only needed for qui)                       |
| `TAG_REMOVABLE`      | `cross-seed-only` | No       | Tag applied to removable torrents                       |
| `EXCLUDE_TAGS`       | `pinned,keep`     | No       | Tags to skip (comma-separated)                          |
| `EXCLUDE_CATEGORIES` |                   | No       | Categories to skip (comma-separated)                    |
| `INCLUDE_CATEGORIES` |                   | No       | Only process these categories (comma-separated)         |
| `MIN_AGE_DAYS`       | `0`               | No       | Minimum torrent age in days                             |
| `DRY_RUN`            | `true`            | No       | Report only; set `false` to apply tags                  |
| `SCHEDULE`           |                   | No       | Cron expression for recurring runs (e.g. `0 */6 * * *`) |
| `RUN_ON_START`       | `true`            | No       | Run immediately before entering cron schedule           |

\* Provide either `QB_PASSWORD` (with `QB_USERNAME`) or `QB_API_KEY`, not both.

## Unraid

An [Unraid Community Applications](https://github.com/mcreekmore/cross-seed-cleanup) template is included (`cross-seed-cleanup.xml`). Install via the Apps tab or add the template manually.

## How It Works

1. **Stat** - Retrieves all torrents from qBittorrent and stats each file to collect inode/hardlink data.
2. **Classify** - Compares each file's hardlink count (`nlink`) against the number of torrents referencing the same inode. Files with external hardlinks are kept.
3. **Tag** - Torrents with no externally linked files are tagged as removable for bulk deletion in qBittorrent.
