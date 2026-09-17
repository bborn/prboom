package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// home is the installed tree: the directory holding bin/ and skills/. Resolved
// from the running binary through any symlinks, so it works whether prboom was
// installed by the curl script, by Homebrew into libexec, or by make install
// straight out of a checkout.
func home() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(filepath.Dir(exe)), nil
}

// linkSkill puts the skills (pr-walk, pr-learn) in every Claude config
// directory it finds.
// A package manager must not write to $HOME, so this is a command you run
// rather than something an install does behind your back.
func linkSkill() error {
	root, err := home()
	if err != nil {
		return err
	}
	srcs, _ := filepath.Glob(filepath.Join(root, "skills", "*"))
	if len(srcs) == 0 {
		return fmt.Errorf("no skills in %s", filepath.Join(root, "skills"))
	}

	hd, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dirs, _ := filepath.Glob(filepath.Join(hd, ".claude*"))

	n := 0
	for _, d := range dirs {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			continue
		}
		skills := filepath.Join(d, "skills")
		if err := os.MkdirAll(skills, 0o755); err != nil {
			continue
		}
		linked := true
		for _, src := range srcs {
			dst := filepath.Join(skills, filepath.Base(src))
			_ = os.RemoveAll(dst)
			if err := os.Symlink(src, dst); err != nil {
				fmt.Fprintf(os.Stderr, "prboom: %v\n", err)
				linked = false
			}
		}
		if linked {
			n++
		}
	}

	if n == 0 {
		return fmt.Errorf("found no Claude config directory under %s", hd)
	}
	fmt.Printf("linked %d skills into %d Claude config director%s\n", len(srcs), n,
		map[bool]string{true: "y", false: "ies"}[n == 1])
	return nil
}
