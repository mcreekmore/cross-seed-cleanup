package cleanup

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

type fileEntry struct {
	index int
	name  string
	size  int64
}

func mkFiles(entries ...fileEntry) *qbittorrent.TorrentFiles {
	f := make(qbittorrent.TorrentFiles, len(entries))
	for i, e := range entries {
		f[i].Index = e.index
		f[i].Name = e.name
		f[i].Size = e.size
	}
	return &f
}

func oneFile(index int, name string) *qbittorrent.TorrentFiles {
	return mkFiles(fileEntry{index, name, 0})
}

func oneFileSize(index int, name string, size int64) *qbittorrent.TorrentFiles {
	return mkFiles(fileEntry{index, name, size})
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

func assertBuckets(t *testing.T, res ClassifyResult, kept, removable, skipped int) {
	t.Helper()
	if len(res.Kept) != kept || len(res.Removable) != removable || len(res.Skipped) != skipped {
		t.Errorf("buckets: got kept=%d removable=%d skipped=%d; want kept=%d removable=%d skipped=%d",
			len(res.Kept), len(res.Removable), len(res.Skipped), kept, removable, skipped)
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

	assertBuckets(t, res, 1, 0, 0)
	if res.Kept[0].Hash != "abc" {
		t.Errorf("expected hash abc in KEPT, got %q", res.Kept[0].Hash)
	}
}

func TestClassify_CrossSeedOnly(t *testing.T) {
	torrent := mkTorrent("abc", "/data", "movies", "", 0)
	files := map[string]*qbittorrent.TorrentFiles{"abc": oneFile(0, "movie.mkv")}
	stat := staticStat(map[string]*StatResult{
		"/data/movie.mkv": {Dev: 1, Ino: 1, Nlink: 1},
	})

	res := classifyTorrents([]qbittorrent.Torrent{torrent}, files, stat, emptyCfg())

	assertBuckets(t, res, 0, 1, 0)
	if res.Removable[0].Hash != "abc" {
		t.Errorf("expected hash abc in REMOVABLE, got %q", res.Removable[0].Hash)
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

	assertBuckets(t, res, 0, 2, 0)
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

	assertBuckets(t, res, 2, 0, 0)
}

func TestClassify_AllFilesInaccessible(t *testing.T) {
	torrent := mkTorrent("abc", "/data", "movies", "", 0)
	files := map[string]*qbittorrent.TorrentFiles{"abc": oneFile(0, "movie.mkv")}
	stat := func(string) (*StatResult, error) { return nil, errors.New("permission denied") }

	res := classifyTorrents([]qbittorrent.Torrent{torrent}, files, stat, emptyCfg())

	assertBuckets(t, res, 0, 0, 1)
	if res.TotalFiles != 1 || res.SkippedFiles != 1 {
		t.Errorf("counters: got total=%d skipped=%d; want total=1 skipped=1", res.TotalFiles, res.SkippedFiles)
	}
}

func TestClassify_NoFilesEntry(t *testing.T) {
	torrent := mkTorrent("abc", "/data", "movies", "", 0)
	stat := func(string) (*StatResult, error) { return nil, errors.New("unreachable") }

	res := classifyTorrents([]qbittorrent.Torrent{torrent}, map[string]*qbittorrent.TorrentFiles{}, stat, emptyCfg())

	assertBuckets(t, res, 0, 0, 1)
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
	boundary := mkTorrent("boundary", "/data", "", "", now-86400*7)
	old := mkTorrent("old", "/data", "", "", now-86400*10)
	files := map[string]*qbittorrent.TorrentFiles{
		"recent":   oneFile(0, "new.mkv"),
		"boundary": oneFile(0, "boundary.mkv"),
		"old":      oneFile(0, "old.mkv"),
	}
	stat := staticStat(map[string]*StatResult{
		"/data/new.mkv":      {Dev: 1, Ino: 1, Nlink: 1},
		"/data/boundary.mkv": {Dev: 1, Ino: 2, Nlink: 1},
		"/data/old.mkv":      {Dev: 1, Ino: 3, Nlink: 1},
	})

	cfg := emptyCfg()
	cfg.Now = now
	cfg.MinAgeDays = 7

	res := classifyTorrents([]qbittorrent.Torrent{recent, boundary, old}, files, stat, cfg)

	assertBuckets(t, res, 0, 2, 0)
	for _, tr := range res.Removable {
		if tr.Hash == "recent" {
			t.Error("recent torrent (below MinAgeDays) should not be processed")
		}
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

	assertBuckets(t, res, 1, 0, 0)
}

func TestReclaimable_SingleRemovable(t *testing.T) {
	torrent := mkTorrent("abc", "/data", "", "", 0)
	files := map[string]*qbittorrent.TorrentFiles{"abc": oneFileSize(0, "movie.mkv", 10_000)}
	stat := staticStat(map[string]*StatResult{
		"/data/movie.mkv": {Dev: 1, Ino: 1, Nlink: 1},
	})

	res := classifyTorrents([]qbittorrent.Torrent{torrent}, files, stat, emptyCfg())

	if res.ReclaimableBytes != 10_000 {
		t.Errorf("ReclaimableBytes: got %d, want 10000", res.ReclaimableBytes)
	}
}

func TestReclaimable_TwoRemovableSameInode_CountsOnce(t *testing.T) {
	t1 := mkTorrent("hash1", "/data/orig", "", "", 0)
	t2 := mkTorrent("hash2", "/data/cs", "", "", 0)
	files := map[string]*qbittorrent.TorrentFiles{
		"hash1": oneFileSize(0, "movie.mkv", 10_000),
		"hash2": oneFileSize(0, "movie.mkv", 10_000),
	}
	stat := staticStat(map[string]*StatResult{
		"/data/orig/movie.mkv": {Dev: 1, Ino: 42, Nlink: 2},
		"/data/cs/movie.mkv":   {Dev: 1, Ino: 42, Nlink: 2},
	})

	res := classifyTorrents([]qbittorrent.Torrent{t1, t2}, files, stat, emptyCfg())

	if res.ReclaimableBytes != 10_000 {
		t.Errorf("ReclaimableBytes: got %d, want 10000 (file counted once)", res.ReclaimableBytes)
	}
}

func TestReclaimable_RemovableAndKeptSameInode_Zero(t *testing.T) {
	t1 := mkTorrent("hash1", "/data/orig", "", "", 0)
	t2 := mkTorrent("hash2", "/data/cs", "", "", 0)
	files := map[string]*qbittorrent.TorrentFiles{
		"hash1": oneFileSize(0, "movie.mkv", 10_000),
		"hash2": oneFileSize(0, "movie.mkv", 10_000),
	}
	stat := staticStat(map[string]*StatResult{
		"/data/orig/movie.mkv": {Dev: 1, Ino: 42, Nlink: 3},
		"/data/cs/movie.mkv":   {Dev: 1, Ino: 42, Nlink: 3},
	})

	res := classifyTorrents([]qbittorrent.Torrent{t1, t2}, files, stat, emptyCfg())

	if res.ReclaimableBytes != 0 {
		t.Errorf("ReclaimableBytes: got %d, want 0 (inode held by kept torrent)", res.ReclaimableBytes)
	}
}

func TestReclaimable_ExcludedTorrentHoldsInode_Zero(t *testing.T) {
	excluded := mkTorrent("hash1", "/data/orig", "", "pinned", 0)
	removable := mkTorrent("hash2", "/data/cs", "", "", 0)
	files := map[string]*qbittorrent.TorrentFiles{
		"hash1": oneFileSize(0, "movie.mkv", 10_000),
		"hash2": oneFileSize(0, "movie.mkv", 10_000),
	}
	stat := staticStat(map[string]*StatResult{
		"/data/orig/movie.mkv": {Dev: 1, Ino: 42, Nlink: 2},
		"/data/cs/movie.mkv":   {Dev: 1, Ino: 42, Nlink: 2},
	})

	cfg := emptyCfg()
	cfg.ExcludeTags = splitSet("pinned")

	res := classifyTorrents([]qbittorrent.Torrent{excluded, removable}, files, stat, cfg)

	if res.ReclaimableBytes != 0 {
		t.Errorf("ReclaimableBytes: got %d, want 0 (excluded torrent still holds inode)", res.ReclaimableBytes)
	}
}

func TestReclaimable_MultiFile_PartiallyShared(t *testing.T) {
	cross := mkTorrent("cs", "/data/cs", "", "", 0)
	keeper := mkTorrent("kp", "/data/kp", "", "", 0)
	csFiles := mkFiles(
		fileEntry{0, "shared.mkv", 5_000},
		fileEntry{1, "unique.mkv", 3_000},
	)
	files := map[string]*qbittorrent.TorrentFiles{
		"cs": csFiles,
		"kp": oneFileSize(0, "shared.mkv", 5_000),
	}
	stat := staticStat(map[string]*StatResult{
		"/data/cs/shared.mkv": {Dev: 1, Ino: 10, Nlink: 3},
		"/data/kp/shared.mkv": {Dev: 1, Ino: 10, Nlink: 3},
		"/data/cs/unique.mkv": {Dev: 1, Ino: 20, Nlink: 1},
	})

	res := classifyTorrents([]qbittorrent.Torrent{cross, keeper}, files, stat, emptyCfg())

	if res.ReclaimableBytes != 0 {
		t.Errorf("ReclaimableBytes: got %d, want 0 (cross shares inode with kept torrent)", res.ReclaimableBytes)
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
