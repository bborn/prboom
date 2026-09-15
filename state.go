package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// How the picker was left in a repo: which list, how it was sorted and
// narrowed. Saved on exit, so opening prboom there again looks the same.
type viewState struct {
	Scope string `json:"scope"`
	Sort  string `json:"sort"`
	Only  string `json:"only,omitempty"`
	Query string `json:"query,omitempty"`
}

func stateFile(repo string) string {
	dir, err := os.UserCacheDir()
	where := repoKey(repo)
	if err != nil || where == "" {
		return ""
	}
	sum := sha1.Sum([]byte("state|" + where))
	return filepath.Join(dir, "prboom", "state-"+hex.EncodeToString(sum[:8])+".json")
}

func readState(path string) (viewState, bool) {
	var s viewState
	if path == "" {
		return s, false
	}
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &s) != nil {
		return viewState{}, false
	}
	return s, true
}

func writeState(path string, s viewState) {
	writeJSON(path, s)
}

// apply restores the saved view onto the options, except where a flag given
// this time says otherwise. set holds the names of the flags that were given.
func (s viewState) apply(o *options, set map[string]bool) {
	if s.Scope != "" && !set["a"] {
		for _, sc := range scopes {
			if sc.Key == s.Scope {
				o.scope = s.Scope
			}
		}
	}
	if s.Sort != "" && !set["s"] {
		for _, so := range sorts {
			if so == s.Sort {
				o.sortBy = s.Sort
			}
		}
	}
	o.only, o.query = s.Only, s.Query
}

func (m model) state() viewState {
	return viewState{Scope: scopes[m.scope].Key, Sort: m.opt.sortBy, Only: m.only, Query: m.query.Value()}
}
