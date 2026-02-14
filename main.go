//go:build linux

package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/autobrr/go-qbittorrent"
	"github.com/robfig/cron/v3"
)

// inodeKey uniquely identifies a file on a filesystem.
type inodeKey struct {
	Dev uint64
	Ino uint64
}

type statInfo struct {
	Key   inodeKey
	Nlink uint64
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	}
	return fallback
}

func splitSet(s string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, v := range strings.Split(s, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			set[v] = struct{}{}
		}
	}
	return set
}

func run() {
	// Configuration
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

	// Connect
	var host string
	var cfg qbittorrent.Config

	if qbApiKey != "" {
		host = fmt.Sprintf("http://%s:%d/proxy/%s", qbHost, qbPort, qbApiKey)
		cfg = qbittorrent.Config{
			Host: host,
		}
		fmt.Println("Using qui API key authentication")
	} else {
		host = fmt.Sprintf("http://%s:%d", qbHost, qbPort)
		cfg = qbittorrent.Config{
			Host:     host,
			Username: qbUsername,
			Password: qbPassword,
		}
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

	// Phase 1: Stat every file and build inode -> torrent hash mapping

	type fileKey struct {
		Hash  string
		Index int
	}

	inodeToHashes := make(map[inodeKey]map[string]struct{})
	fileInfoMap := make(map[fileKey]*statInfo)
	torrentFiles := make(map[string]*qbittorrent.TorrentFiles)

	skipped := 0
	totalFiles := 0

	for i, torrent := range torrents {
		if (i+1)%500 == 0 || i+1 == len(torrents) {
			fmt.Printf("  Scanning torrent %d/%d...\r", i+1, len(torrents))
		}

		files, err := client.GetFilesInformation(torrent.Hash)
		if err != nil {
			continue
		}
		torrentFiles[torrent.Hash] = files

		for _, f := range *files {
			totalFiles++
			filePath := filepath.Join(torrent.SavePath, f.Name)

			fi, err := os.Stat(filePath)
			if err != nil {
				fileInfoMap[fileKey{torrent.Hash, f.Index}] = nil
				skipped++
				continue
			}

			stat, ok := fi.Sys().(*syscall.Stat_t)
			if !ok {
				fileInfoMap[fileKey{torrent.Hash, f.Index}] = nil
				skipped++
				continue
			}

			key := inodeKey{stat.Dev, stat.Ino}
			if inodeToHashes[key] == nil {
				inodeToHashes[key] = make(map[string]struct{})
			}
			inodeToHashes[key][torrent.Hash] = struct{}{}
			fileInfoMap[fileKey{torrent.Hash, f.Index}] = &statInfo{
				Key:   key,
				Nlink: stat.Nlink,
			}
		}
	}

	fmt.Printf("\nScanned %d files across %d torrents (%d inaccessible)\n",
		totalFiles, len(torrents), skipped)
	fmt.Printf("Unique inodes: %d\n", len(inodeToHashes))

	// Phase 2: Classify each torrent

	var removable, kept, skippedTorrents []qbittorrent.Torrent
	now := time.Now().Unix()

	for _, torrent := range torrents {
		// Apply exclusion filters
		torrentTags := splitSet(torrent.Tags)

		excluded := false
		for tag := range torrentTags {
			if _, ok := excludeTags[tag]; ok {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		if len(excludeCategories) > 0 {
			if _, ok := excludeCategories[torrent.Category]; ok {
				continue
			}
		}
		if len(includeCategories) > 0 {
			if _, ok := includeCategories[torrent.Category]; !ok {
				continue
			}
		}
		if minAgeDays > 0 && (now-torrent.AddedOn) < int64(minAgeDays)*86400 {
			continue
		}

		files, ok := torrentFiles[torrent.Hash]
		if !ok {
			skippedTorrents = append(skippedTorrents, torrent)
			continue
		}

		hasFiles := false
		externallyLinked := false

		for _, f := range *files {
			info := fileInfoMap[fileKey{torrent.Hash, f.Index}]
			if info == nil {
				continue
			}

			hasFiles = true
			torrentRefs := len(inodeToHashes[info.Key])

			if info.Nlink > uint64(torrentRefs) {
				externallyLinked = true
				break
			}
		}

		if !hasFiles {
			skippedTorrents = append(skippedTorrents, torrent)
			continue
		}

		if externallyLinked {
			kept = append(kept, torrent)
		} else {
			removable = append(removable, torrent)
		}
	}

	// Phase 3: Report

	fmt.Printf("\n%s\n", strings.Repeat("=", 60))
	fmt.Printf("  Externally linked (KEEP):        %d\n", len(kept))
	fmt.Printf("  Cross-seed only (REMOVABLE):     %d\n", len(removable))
	fmt.Printf("  No accessible files (SKIPPED):   %d\n", len(skippedTorrents))
	fmt.Printf("%s\n", strings.Repeat("=", 60))

	if len(removable) == 0 {
		fmt.Println("\nAll torrents with files are externally linked. Nothing to tag.")
		return
	}

	fmt.Println("\nRemovable torrents (no external hardlinks):\n")

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
	// Handle "run" subcommand — always executes once and exits.
	if len(os.Args) > 1 && os.Args[1] == "run" {
		fmt.Println("Running cross-seed-cleanup...")
		run()
		return
	}

	schedule := os.Getenv("SCHEDULE")

	// No schedule configured — single run and exit (backwards compatible).
	if schedule == "" {
		run()
		return
	}

	// Cron mode
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

	// Block until termination signal.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Println("\nShutting down scheduler...")
	c.Stop()
}
