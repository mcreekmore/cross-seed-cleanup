//go:build linux

package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	qbittorrent "github.com/autobrr/go-qbittorrent"
	"github.com/robfig/cron/v3"
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

func run() {
	qbHost := getenv("QB_HOST", "localhost")
	qbPort := getenvInt("QB_PORT", 8080)
	qbUsername := getenv("QB_USERNAME", "admin")
	qbPassword := getenv("QB_PASSWORD", "")
	qbApiKey := getenv("QB_API_KEY", "")

	tagRemovable := getenv("TAG_REMOVABLE", "cross-seed-only")
	excludeTags := splitSet(getenv("EXCLUDE_TAGS", "pinned,keep"))
	excludeCategories := splitSet(getenv("EXCLUDE_CATEGORIES", ""))
	includeCategories := splitSet(getenv("INCLUDE_CATEGORIES", ""))
	minAgeDays := getenvInt("MIN_AGE_DAYS", 0)

	dryRun := true
	if v := strings.ToLower(getenv("DRY_RUN", "true")); v == "false" || v == "0" || v == "no" {
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
			fmt.Fprintf(out, "  Fetching file info %d/%d...\r", i+1, len(torrents))
		}
		files, err := client.GetFilesInformation(torrent.Hash)
		if err != nil {
			fileInfoErrors++
			slog.Debug("failed to get file info", "hash", torrent.Hash, "name", torrent.Name, "err", err)
			continue
		}
		torrentFiles[torrent.Hash] = files
	}
	fmt.Fprintln(out)
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

	fmt.Fprintf(out, "\n%s\n", strings.Repeat("=", 60))
	fmt.Fprintf(out, "  Externally linked (KEEP):        %d\n", len(kept))
	fmt.Fprintf(out, "  Cross-seed only (REMOVABLE):     %d\n", len(removable))
	fmt.Fprintf(out, "  No accessible files (SKIPPED):   %d\n", len(skippedTorrents))
	fmt.Fprintf(out, "%s\n", strings.Repeat("=", 60))

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

func main() {
	setupLogging()

	if len(os.Args) > 1 && os.Args[1] == "run" {
		slog.Info("running cross-seed-cleanup")
		run()
		return
	}

	schedule := os.Getenv("SCHEDULE")

	if schedule == "" {
		run()
		return
	}

	runOnStart := getenvBool("RUN_ON_START", true)

	slog.Info("scheduler configured", "schedule", schedule, "run_on_start", runOnStart)
	if runOnStart {
		slog.Info("executing initial run")
		run()
	}

	c := cron.New()
	_, err := c.AddFunc(schedule, func() {
		slog.Info("scheduled run starting")
		run()
		slog.Info("scheduled run complete")
	})
	if err != nil {
		slog.Error("invalid SCHEDULE expression", "schedule", schedule, "err", err)
		os.Exit(1)
	}

	c.Start()
	slog.Info("cron scheduler started, waiting for next run")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	slog.Info("shutting down scheduler")
	c.Stop()
}
