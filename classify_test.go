//go:build linux

package main

import (
	"errors"
	"testing"

	qbittorrent "github.com/autobrr/go-qbittorrent"
)

func mkTorrent(hash, savePath, category, tags string, addedOn int64) qbittorrent.Torrent {
	return qbittorrent.Torrent{
		Hash:     hash,
		SavePath: savePath,
		Category: category,
		Tags:     tags,
		AddedOn:  addedOn,
	}
}

func mkFiles(entries ...struct{ index int; name string }) *qbittorrent.TorrentFiles {
	f := make(qbittorrent.TorrentFiles, len(entries))
	for i, e := range entries {
		f[i].Index = e.index
		f[i].Name = e.name
	}
	return &f
}

func oneFile(index int, name string) *qbittorrent.TorrentFiles {
	return mkFiles(struct{ index int; name string }{index, name})
}

func staticStat(results map[string]*StatResult) func(string) (*StatResult, error) {
	return func(path string) (*StatResult, error) {
		r, ok := results[path]
		if !ok {
			return nil, errors.New("no such file")
		}
		return r, nil
	}
}

func emptyCfg() ClassifyConfig {
	return ClassifyConfig{
		ExcludeTags:       map[string]struct{}{},
		ExcludeCategories: map[string]struct{}{},
		IncludeCategories: map[string]struct{}{},
		Now:               1_000_000,
	}
}

func TestClassify_ExternallyLinked(t *testing.T) {
	torrent := mkTorrent("abc", "/data", "movies", "", 0)
	files := map[string]*qbittorrent.TorrentFiles{"abc": oneFile(0, "movie.mkv")}
	stat := staticStat(map[string]*StatResult{
		"/data/movie.mkv": {Dev: 1, Ino: 1, Nlink: 2},
	})

	res := classifyTorrents([]qbittorrent.Torrent{torrent}, files, stat, emptyCfg())

	if len(res.Kept) != 1 || res.Kept[0].Hash != "abc" {
		t.Errorf("expected KEPT: kept=%d removable=%d", len(res.Kept), len(res.Removable))
	}
}

func TestClassify_CrossSeedOnly(t *testing.T) {
	torrent := mkTorrent("abc", "/data", "movies", "", 0)
	files := map[string]*qbittorrent.TorrentFiles{"abc": oneFile(0, "movie.mkv")}
	stat := staticStat(map[string]*StatResult{
		"/data/movie.mkv": {Dev: 1, Ino: 1, Nlink: 1},
	})

	res := classifyTorrents([]qbittorrent.Torrent{torrent}, files, stat, emptyCfg())

	if len(res.Removable) != 1 || res.Removable[0].Hash != "abc" {
		t.Errorf("expected REMOVABLE: kept=%d removable=%d", len(res.Kept), len(res.Removable))
	}
}

func TestClassify_TwoCrossSeedNoExternalLink(t *testing.T) {
	t1 := mkTorrent("hash1", "/data/orig", "movies", "", 0)
	t2 := mkTorrent("hash2", "/data/cs", "movies", "", 0)
	files := map[string]*qbittorrent.TorrentFiles{
		"hash1": oneFile(0, "movie.mkv"),
		"hash2": oneFile(0, "movie.mkv"),
	}
	stat := staticStat(map[string]*StatResult{
		"/data/orig/movie.mkv": {Dev: 1, Ino: 42, Nlink: 2},
		"/data/cs/movie.mkv":   {Dev: 1, Ino: 42, Nlink: 2},
	})

	res := classifyTorrents([]qbittorrent.Torrent{t1, t2}, files, stat, emptyCfg())

	if len(res.Removable) != 2 {
		t.Errorf("expected both REMOVABLE: kept=%d removable=%d", len(res.Kept), len(res.Removable))
	}
}

func TestClassify_TwoCrossSeedWithExternalLink(t *testing.T) {
	t1 := mkTorrent("hash1", "/data/orig", "movies", "", 0)
	t2 := mkTorrent("hash2", "/data/cs", "movies", "", 0)
	files := map[string]*qbittorrent.TorrentFiles{
		"hash1": oneFile(0, "movie.mkv"),
		"hash2": oneFile(0, "movie.mkv"),
	}
	stat := staticStat(map[string]*StatResult{
		"/data/orig/movie.mkv": {Dev: 1, Ino: 42, Nlink: 3},
		"/data/cs/movie.mkv":   {Dev: 1, Ino: 42, Nlink: 3},
	})

	res := classifyTorrents([]qbittorrent.Torrent{t1, t2}, files, stat, emptyCfg())

	if len(res.Kept) != 2 {
		t.Errorf("expected both KEPT: kept=%d removable=%d", len(res.Kept), len(res.Removable))
	}
}

func TestClassify_AllFilesInaccessible(t *testing.T) {
	torrent := mkTorrent("abc", "/data", "movies", "", 0)
	files := map[string]*qbittorrent.TorrentFiles{"abc": oneFile(0, "movie.mkv")}
	stat := func(string) (*StatResult, error) { return nil, errors.New("permission denied") }

	res := classifyTorrents([]qbittorrent.Torrent{torrent}, files, stat, emptyCfg())

	if len(res.Skipped) != 1 {
		t.Errorf("expected SKIPPED: kept=%d removable=%d skipped=%d", len(res.Kept), len(res.Removable), len(res.Skipped))
	}
}

func TestClassify_NoFilesEntry(t *testing.T) {
	torrent := mkTorrent("abc", "/data", "movies", "", 0)
	stat := func(string) (*StatResult, error) { return nil, errors.New("unreachable") }

	res := classifyTorrents([]qbittorrent.Torrent{torrent}, map[string]*qbittorrent.TorrentFiles{}, stat, emptyCfg())

	if len(res.Skipped) != 1 {
		t.Errorf("expected SKIPPED: kept=%d removable=%d skipped=%d", len(res.Kept), len(res.Removable), len(res.Skipped))
	}
}

func TestClassify_ExcludeByTag(t *testing.T) {
	torrent := mkTorrent("abc", "/data", "movies", "pinned,hd", 0)
	files := map[string]*qbittorrent.TorrentFiles{"abc": oneFile(0, "movie.mkv")}
	stat := staticStat(map[string]*StatResult{"/data/movie.mkv": {Dev: 1, Ino: 1, Nlink: 1}})

	cfg := emptyCfg()
	cfg.ExcludeTags = splitSet("pinned,keep")

	res := classifyTorrents([]qbittorrent.Torrent{torrent}, files, stat, cfg)

	if total := len(res.Kept) + len(res.Removable) + len(res.Skipped); total != 0 {
		t.Errorf("excluded torrent should not appear in any list, total=%d", total)
	}
}

func TestClassify_ExcludeByCategory(t *testing.T) {
	torrent := mkTorrent("abc", "/data", "movies", "", 0)
	files := map[string]*qbittorrent.TorrentFiles{"abc": oneFile(0, "movie.mkv")}
	stat := staticStat(map[string]*StatResult{"/data/movie.mkv": {Dev: 1, Ino: 1, Nlink: 1}})

	cfg := emptyCfg()
	cfg.ExcludeCategories = splitSet("movies")

	res := classifyTorrents([]qbittorrent.Torrent{torrent}, files, stat, cfg)

	if total := len(res.Kept) + len(res.Removable) + len(res.Skipped); total != 0 {
		t.Errorf("excluded torrent should not appear in any list, total=%d", total)
	}
}

func TestClassify_IncludeFilter(t *testing.T) {
	t1 := mkTorrent("hash1", "/data", "movies", "", 0)
	t2 := mkTorrent("hash2", "/data", "tv", "", 0)
	files := map[string]*qbittorrent.TorrentFiles{
		"hash1": oneFile(0, "movie.mkv"),
		"hash2": oneFile(0, "show.mkv"),
	}
	stat := staticStat(map[string]*StatResult{
		"/data/movie.mkv": {Dev: 1, Ino: 1, Nlink: 1},
		"/data/show.mkv":  {Dev: 1, Ino: 2, Nlink: 1},
	})

	cfg := emptyCfg()
	cfg.IncludeCategories = splitSet("tv")

	res := classifyTorrents([]qbittorrent.Torrent{t1, t2}, files, stat, cfg)

	if total := len(res.Kept) + len(res.Removable) + len(res.Skipped); total != 1 {
		t.Errorf("expected only 1 torrent processed (tv), got %d", total)
	}
	if len(res.Removable) != 1 || res.Removable[0].Hash != "hash2" {
		t.Errorf("expected hash2 (tv) REMOVABLE")
	}
}

func TestClassify_MinAgeDays(t *testing.T) {
	const now = int64(1_000_000)
	recent := mkTorrent("recent", "/data", "", "", now-86400*3)
	old := mkTorrent("old", "/data", "", "", now-86400*10)
	files := map[string]*qbittorrent.TorrentFiles{
		"recent": oneFile(0, "new.mkv"),
		"old":    oneFile(0, "old.mkv"),
	}
	stat := staticStat(map[string]*StatResult{
		"/data/new.mkv": {Dev: 1, Ino: 1, Nlink: 1},
		"/data/old.mkv": {Dev: 1, Ino: 2, Nlink: 1},
	})

	cfg := emptyCfg()
	cfg.Now = now
	cfg.MinAgeDays = 7

	res := classifyTorrents([]qbittorrent.Torrent{recent, old}, files, stat, cfg)

	if len(res.Removable) != 1 || res.Removable[0].Hash != "old" {
		t.Errorf("expected only 'old' processed: kept=%d removable=%d", len(res.Kept), len(res.Removable))
	}
}

func TestClassify_MultiFile_AnyExternalLinkKeepsTorrent(t *testing.T) {
	torrent := mkTorrent("abc", "/data", "tv", "", 0)
	f := qbittorrent.TorrentFiles{
		{Index: 0, Name: "ep1.mkv"},
		{Index: 1, Name: "ep2.mkv"},
	}
	files := map[string]*qbittorrent.TorrentFiles{"abc": &f}
	stat := staticStat(map[string]*StatResult{
		"/data/ep1.mkv": {Dev: 1, Ino: 1, Nlink: 1},
		"/data/ep2.mkv": {Dev: 1, Ino: 2, Nlink: 2},
	})

	res := classifyTorrents([]qbittorrent.Torrent{torrent}, files, stat, emptyCfg())

	if len(res.Kept) != 1 {
		t.Errorf("expected KEPT (one file externally linked): kept=%d removable=%d", len(res.Kept), len(res.Removable))
	}
}

func TestClassify_Stats(t *testing.T) {
	t1 := mkTorrent("hash1", "/data", "", "", 0)
	t2 := mkTorrent("hash2", "/data", "", "", 0)
	files := map[string]*qbittorrent.TorrentFiles{
		"hash1": oneFile(0, "movie.mkv"),
		"hash2": oneFile(0, "show.mkv"),
	}
	stat := staticStat(map[string]*StatResult{
		"/data/show.mkv": {Dev: 1, Ino: 1, Nlink: 1},
	})

	res := classifyTorrents([]qbittorrent.Torrent{t1, t2}, files, stat, emptyCfg())

	if res.TotalFiles != 2 {
		t.Errorf("TotalFiles: got %d, want 2", res.TotalFiles)
	}
	if res.SkippedFiles != 1 {
		t.Errorf("SkippedFiles: got %d, want 1", res.SkippedFiles)
	}
	if res.UniqueInodes != 1 {
		t.Errorf("UniqueInodes: got %d, want 1", res.UniqueInodes)
	}
}
