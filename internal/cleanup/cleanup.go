//go:build linux

package cleanup

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	qbittorrent "github.com/autobrr/go-qbittorrent"

	"github.com/mcreekmore/cross-seed-cleanup/internal/env"
)

func realStat(path string) (*StatResult, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, fmt.Errorf("unexpected stat type")
	}
	return &StatResult{Dev: stat.Dev, Ino: stat.Ino, Nlink: stat.Nlink}, nil
}

func Run() {
	qbHost := env.Getenv("QB_HOST", "localhost")
	qbPort := env.GetenvInt("QB_PORT", 8080)
	qbUsername := env.Getenv("QB_USERNAME", "admin")
	qbPassword := env.Getenv("QB_PASSWORD", "")
	qbApiKey := env.Getenv("QB_API_KEY", "")

	tagRemovable := env.Getenv("TAG_REMOVABLE", "cross-seed-only")
	excludeTags := splitSet(env.Getenv("EXCLUDE_TAGS", "pinned,keep"))
	excludeCategories := splitSet(env.Getenv("EXCLUDE_CATEGORIES", ""))
	includeCategories := splitSet(env.Getenv("INCLUDE_CATEGORIES", ""))
	minAgeDays := env.GetenvInt("MIN_AGE_DAYS", 0)

	dryRun := true
	if v := strings.ToLower(env.Getenv("DRY_RUN", "true")); v == "false" || v == "0" || v == "no" {
		dryRun = false
	}

	var host string
	var cfg qbittorrent.Config

	if qbApiKey != "" {
		host = fmt.Sprintf("http://%s:%d/proxy/%s", qbHost, qbPort, qbApiKey)
		cfg = qbittorrent.Config{Host: host}
		slog.Info("using qui API key authentication", "host", host)
	} else {
		host = fmt.Sprintf("http://%s:%d", qbHost, qbPort)
		cfg = qbittorrent.Config{Host: host, Username: qbUsername, Password: qbPassword}
		slog.Debug("using username/password authentication", "host", host, "username", qbUsername)
	}

	slog.Info("configuration",
		"dry_run", dryRun,
		"tag_removable", tagRemovable,
		"min_age_days", minAgeDays,
	)

	client := qbittorrent.NewClient(cfg)
	if err := client.Login(); err != nil {
		slog.Error("failed to log in to qBittorrent, check credentials", "err", err)
		return
	}
	if version, err := client.GetAppVersion(); err == nil {
		slog.Info("connected to qBittorrent", "version", version)
	} else {
		slog.Debug("could not read qBittorrent version", "err", err)
	}

	torrents, err := client.GetTorrents(qbittorrent.TorrentFilterOptions{})
	if err != nil {
		slog.Error("failed to get torrents", "err", err)
		return
	}
	slog.Info("fetched torrents", "count", len(torrents))

	torrentFiles := make(map[string]*qbittorrent.TorrentFiles)
	var fileInfoErrors int
	for i, torrent := range torrents {
		if (i+1)%500 == 0 || i+1 == len(torrents) {
			_, err = fmt.Fprintf(out, "  Fetching file info %d/%d...\r", i+1, len(torrents))
			if err != nil {
				slog.Error("failed to format print", "err", err)
			}
		}
		files, err := client.GetFilesInformation(torrent.Hash)
		if err != nil {
			fileInfoErrors++
			slog.Debug("failed to get file info", "hash", torrent.Hash, "name", torrent.Name, "err", err)
			continue
		}
		torrentFiles[torrent.Hash] = files
	}
	_, err = fmt.Fprintln(out)
	if err != nil {
		slog.Error("failed to format print", "err", err)
	}
	if fileInfoErrors > 0 {
		slog.Warn("could not fetch file info for some torrents", "count", fileInfoErrors)
	}

	slog.Info("scanning files")
	classCfg := ClassifyConfig{
		ExcludeTags:       excludeTags,
		ExcludeCategories: excludeCategories,
		IncludeCategories: includeCategories,
		MinAgeDays:        minAgeDays,
		Now:               time.Now().Unix(),
	}
	result := classifyTorrents(torrents, torrentFiles, realStat, classCfg)

	slog.Info("scan complete",
		"files_scanned", result.TotalFiles,
		"files_inaccessible", result.SkippedFiles,
		"unique_inodes", result.UniqueInodes,
	)

	kept := result.Kept
	removable := result.Removable
	skippedTorrents := result.Skipped

	slog.Info("classification summary",
		"kept", len(kept),
		"removable", len(removable),
		"skipped", len(skippedTorrents),
	)

	_, err = fmt.Fprintf(out, "\n%s\n", strings.Repeat("=", 60))
	if err != nil {
		slog.Error("failed to format print", "err", err)
	}
	_, err = fmt.Fprintf(out, "  Externally linked (KEEP):        %d\n", len(kept))
	if err != nil {
		slog.Error("failed to format print", "err", err)
	}
	_, err = fmt.Fprintf(out, "  Cross-seed only (REMOVABLE):     %d\n", len(removable))
	if err != nil {
		slog.Error("failed to format print", "err", err)
	}
	_, err = fmt.Fprintf(out, "  No accessible files (SKIPPED):   %d\n", len(skippedTorrents))
	if err != nil {
		slog.Error("failed to format print", "err", err)
	}
	_, err = fmt.Fprintf(out, "%s\n", strings.Repeat("=", 60))

	if len(removable) == 0 {
		fmt.Fprintln(out, "\nAll torrents with files are externally linked. Nothing to tag.")
		return
	}

	fmt.Fprintln(out, "\nRemovable torrents (no external hardlinks):")

	sort.Slice(removable, func(i, j int) bool {
		return removable[i].Size > removable[j].Size
	})

	for _, t := range removable {
		sizeGiB := float64(t.Size) / (1024 * 1024 * 1024)
		cat := ""
		if t.Category != "" {
			cat = fmt.Sprintf("[%s]", t.Category)
		}
		fmt.Fprintf(out, "  %8.2f GiB  %-20s  %s\n", sizeGiB, cat, t.Name)
	}

	totalGiB := float64(result.ReclaimableBytes) / (1024 * 1024 * 1024)
	fmt.Fprintf(out, "\n  Total reclaimable: %.2f GiB\n", totalGiB)

	if !dryRun {
		hashes := make([]string, len(removable))
		for i, t := range removable {
			hashes[i] = t.Hash
		}
		if err := client.AddTags(hashes, tagRemovable); err != nil {
			slog.Error("failed to add tags", "tag", tagRemovable, "err", err)
		} else {
			slog.Info("applied tag", "tag", tagRemovable, "count", len(removable))
		}
	} else {
		slog.Info("dry run, no changes made; set DRY_RUN=false to apply tags")
	}
}
