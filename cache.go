package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The last list each scope returned, so the picker opens on something while gh
// takes its two seconds. It is only ever shown as stale and replaced.

type cachedList struct {
	At  time.Time `json:"at"`
	PRs []PR      `json:"prs"`
}

// cacheFile is keyed by where the list comes from and how it was asked for.
// Without -R that is the repo's git dir, so two checkouts of one repo share.
func cacheFile(repo, scope, author string, limit int) string {
	dir, err := os.UserCacheDir()
	where := repoKey(repo)
	if err != nil || where == "" {
		return ""
	}
	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%s|%s|%d", where, scope, author, limit)))
	return filepath.Join(dir, "prboom", hex.EncodeToString(sum[:8])+".json")
}

// repoKey names the repo a list comes from: -R as given, or else the repo's
// git dir, so two checkouts of one repo share. Empty when neither is known.
func repoKey(repo string) string {
	if repo != "" {
		return repo
	}
	out, err := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func readCache(path string) ([]PR, time.Time, bool) {
	if path == "" {
		return nil, time.Time{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, false
	}
	var c cachedList
	if json.Unmarshal(b, &c) != nil {
		return nil, time.Time{}, false
	}
	for i := range c.PRs {
		c.PRs[i].derive()
	}
	return c.PRs, c.At, true
}

// writeCache is best effort: a list that cannot be cached is still a list.
func writeCache(path string, prs []PR) {
	writeJSON(path, cachedList{At: time.Now(), PRs: prs})
}

// writeJSON replaces path whole, so a reader never sees half a file.
func writeJSON(path string, v any) {
	if path == "" {
		return
	}
	b, err := json.Marshal(v)
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}
