//go:build linux

package main

import (
	"path/filepath"

	qbittorrent "github.com/autobrr/go-qbittorrent"
)

type inodeKey struct {
	Dev uint64
	Ino uint64
}

type statInfo struct {
	Key   inodeKey
	Nlink uint64
}

type StatResult struct {
	Dev   uint64
	Ino   uint64
	Nlink uint64
}

type ClassifyConfig struct {
	ExcludeTags       map[string]struct{}
	ExcludeCategories map[string]struct{}
	IncludeCategories map[string]struct{}
	MinAgeDays        int
	Now               int64
}

type ClassifyResult struct {
	Removable    []qbittorrent.Torrent
	Kept         []qbittorrent.Torrent
	Skipped      []qbittorrent.Torrent
	TotalFiles   int
	SkippedFiles int
	UniqueInodes int
}

func classifyTorrents(
	torrents []qbittorrent.Torrent,
	torrentFiles map[string]*qbittorrent.TorrentFiles,
	statFn func(path string) (*StatResult, error),
	cfg ClassifyConfig,
) ClassifyResult {
	type fileKey struct {
		Hash  string
		Index int
	}

	inodeToHashes := make(map[inodeKey]map[string]struct{})
	fileInfoMap := make(map[fileKey]*statInfo)
	var totalFiles, skippedFiles int

	for _, torrent := range torrents {
		files, ok := torrentFiles[torrent.Hash]
		if !ok {
			continue
		}
		for _, f := range *files {
			totalFiles++
			filePath := filepath.Join(torrent.SavePath, f.Name)
			sr, err := statFn(filePath)
			if err != nil {
				skippedFiles++
				continue
			}
			key := inodeKey{sr.Dev, sr.Ino}
			if inodeToHashes[key] == nil {
				inodeToHashes[key] = make(map[string]struct{})
			}
			inodeToHashes[key][torrent.Hash] = struct{}{}
			fileInfoMap[fileKey{torrent.Hash, f.Index}] = &statInfo{
				Key:   key,
				Nlink: sr.Nlink,
			}
		}
	}

	res := ClassifyResult{
		TotalFiles:   totalFiles,
		SkippedFiles: skippedFiles,
		UniqueInodes: len(inodeToHashes),
	}

	for _, torrent := range torrents {
		torrentTags := splitSet(torrent.Tags)

		excluded := false
		for tag := range torrentTags {
			if _, ok := cfg.ExcludeTags[tag]; ok {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		if len(cfg.ExcludeCategories) > 0 {
			if _, ok := cfg.ExcludeCategories[torrent.Category]; ok {
				continue
			}
		}
		if len(cfg.IncludeCategories) > 0 {
			if _, ok := cfg.IncludeCategories[torrent.Category]; !ok {
				continue
			}
		}
		if cfg.MinAgeDays > 0 && (cfg.Now-torrent.AddedOn) < int64(cfg.MinAgeDays)*86400 {
			continue
		}

		files, ok := torrentFiles[torrent.Hash]
		if !ok {
			res.Skipped = append(res.Skipped, torrent)
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
			res.Skipped = append(res.Skipped, torrent)
			continue
		}

		if externallyLinked {
			res.Kept = append(res.Kept, torrent)
		} else {
			res.Removable = append(res.Removable, torrent)
		}
	}

	return res
}
