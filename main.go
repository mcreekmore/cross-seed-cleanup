//go:build linux

package main

import (
	"fmt"
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
		fmt.Println("Using qui API key authentication")
	} else {
		host = fmt.Sprintf("http://%s:%d", qbHost, qbPort)
		cfg = qbittorrent.Config{Host: host, Username: qbUsername, Password: qbPassword}
	}

	client := qbittorrent.NewClient(cfg)
	if err := client.Login(); err != nil {
		fmt.Println("ERROR: Failed to log in to qBittorrent. Check credentials.")
		return
	}
	if version, err := client.GetAppVersion(); err == nil {
		fmt.Printf("Connected to qBittorrent %s\n", version)
	}

	torrents, err := client.GetTorrents(qbittorrent.TorrentFilterOptions{})
	if err != nil {
		fmt.Printf("ERROR: Failed to get torrents: %v\n", err)
		return
	}
	fmt.Printf("Total torrents: %d\n", len(torrents))

	torrentFiles := make(map[string]*qbittorrent.TorrentFiles)
	for i, torrent := range torrents {
		if (i+1)%500 == 0 || i+1 == len(torrents) {
			fmt.Printf("  Fetching file info %d/%d...\r", i+1, len(torrents))
		}
		files, err := client.GetFilesInformation(torrent.Hash)
		if err != nil {
			continue
		}
		torrentFiles[torrent.Hash] = files
	}
	fmt.Println()

	fmt.Println("Scanning files...")
	classCfg := ClassifyConfig{
		ExcludeTags:       excludeTags,
		ExcludeCategories: excludeCategories,
		IncludeCategories: includeCategories,
		MinAgeDays:        minAgeDays,
		Now:               time.Now().Unix(),
	}
	result := classifyTorrents(torrents, torrentFiles, realStat, classCfg)

	fmt.Printf("Scanned %d files (%d inaccessible)\n", result.TotalFiles, result.SkippedFiles)
	fmt.Printf("Unique inodes: %d\n", result.UniqueInodes)

	kept := result.Kept
	removable := result.Removable
	skippedTorrents := result.Skipped

	fmt.Printf("\n%s\n", strings.Repeat("=", 60))
	fmt.Printf("  Externally linked (KEEP):        %d\n", len(kept))
	fmt.Printf("  Cross-seed only (REMOVABLE):     %d\n", len(removable))
	fmt.Printf("  No accessible files (SKIPPED):   %d\n", len(skippedTorrents))
	fmt.Printf("%s\n", strings.Repeat("=", 60))

	if len(removable) == 0 {
		fmt.Println("\nAll torrents with files are externally linked. Nothing to tag.")
		return
	}

	fmt.Println("\nRemovable torrents (no external hardlinks):")

	sort.Slice(removable, func(i, j int) bool {
		return removable[i].Size > removable[j].Size
	})

	var totalSize int64
	for _, t := range removable {
		sizeGB := float64(t.Size) / (1024 * 1024 * 1024)
		totalSize += t.Size
		cat := ""
		if t.Category != "" {
			cat = fmt.Sprintf("[%s]", t.Category)
		}
		fmt.Printf("  %8.2f GB  %-20s  %s\n", sizeGB, cat, t.Name)
	}

	totalGB := float64(totalSize) / (1024 * 1024 * 1024)
	fmt.Printf("\n  Total reclaimable: %.2f GB\n", totalGB)

	if !dryRun {
		hashes := make([]string, len(removable))
		for i, t := range removable {
			hashes[i] = t.Hash
		}
		if err := client.AddTags(hashes, tagRemovable); err != nil {
			fmt.Printf("ERROR: Failed to add tags: %v\n", err)
		} else {
			fmt.Printf("\n  Applied tag '%s' to %d torrents.\n", tagRemovable, len(removable))
		}
	} else {
		fmt.Println("\n  DRY RUN — no changes made. Set DRY_RUN=false to apply tags.")
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "run" {
		fmt.Println("Running cross-seed-cleanup...")
		run()
		return
	}

	schedule := os.Getenv("SCHEDULE")

	if schedule == "" {
		run()
		return
	}

	runOnStart := getenvBool("RUN_ON_START", true)

	fmt.Printf("Schedule: %s\n", schedule)
	if runOnStart {
		fmt.Println("RUN_ON_START=true — executing initial run...")
		run()
		fmt.Println()
	}

	c := cron.New()
	_, err := c.AddFunc(schedule, func() {
		fmt.Printf("[%s] Scheduled run starting...\n", time.Now().Format(time.DateTime))
		run()
		fmt.Printf("[%s] Scheduled run complete.\n\n", time.Now().Format(time.DateTime))
	})
	if err != nil {
		fmt.Printf("ERROR: Invalid SCHEDULE expression %q: %v\n", schedule, err)
		os.Exit(1)
	}

	c.Start()
	fmt.Printf("Cron scheduler started. Waiting for next run...\n")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Println("\nShutting down scheduler...")
	c.Stop()
}
