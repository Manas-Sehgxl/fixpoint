// Package snapshot copies the text files of a target directory aside, diffs
// the live directory against such a copy and restores the directory to it.
// This is how Fixpoint measures what the agent changed and rolls it back.
package snapshot

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
)

// MaxFileSize is the largest file that is snapshotted; bigger files are
// assumed not to be source.
const MaxFileSize = 1 << 20

var skipDirs = map[string]bool{".git": true, "node_modules": true, ".testmuai": true, ".fixpoint": true}

// textFiles returns the snapshot-worthy files under root, keyed by their
// slash-separated relative path. exclude lists absolute directories to skip
// (for example a data directory inside the target).
func textFiles(root string, exclude []string) (map[string][]byte, error) {
	out := map[string][]byte{}
	ex := map[string]bool{}
	for _, e := range exclude {
		if abs, err := filepath.Abs(e); err == nil {
			ex[abs] = true
		}
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil
		}
		if d.IsDir() {
			if p != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			if abs, err := filepath.Abs(p); err == nil && ex[abs] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > MaxFileSize {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil || bytes.IndexByte(b, 0) >= 0 {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		out[filepath.ToSlash(rel)] = b
		return nil
	})
	return out, err
}

// Take copies the text files of src into dst, replacing whatever dst held.
func Take(src, dst string, exclude ...string) error {
	files, err := textFiles(src, append(exclude, dst))
	if err != nil {
		return fmt.Errorf("read %s for a snapshot: %w", src, err)
	}
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("clear the old snapshot %s: %w", dst, err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("create the snapshot %s: %w", dst, err)
	}
	for rel, b := range files {
		p := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return fmt.Errorf("write the snapshot of %s: %w", rel, err)
		}
	}
	return nil
}

// Exists reports whether a snapshot directory is present.
func Exists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// Diff compares the live directory with a snapshot and returns one entry per
// changed file, sorted by path.
func Diff(snap, live string, exclude ...string) ([]core.FileDiff, error) {
	if !Exists(snap) {
		return nil, fmt.Errorf("snapshot %s is missing", snap)
	}
	before, err := textFiles(snap, nil)
	if err != nil {
		return nil, err
	}
	after, err := textFiles(live, append(exclude, snap))
	if err != nil {
		return nil, err
	}
	paths := map[string]bool{}
	for p := range before {
		paths[p] = true
	}
	for p := range after {
		paths[p] = true
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	out := []core.FileDiff{}
	for _, p := range sorted {
		old, inOld := before[p]
		new, inNew := after[p]
		if inOld && inNew && bytes.Equal(old, new) {
			continue
		}
		patch, adds, dels := Unified(p, string(old), string(new), inOld, inNew)
		change := "modified"
		switch {
		case !inOld:
			change = "added"
		case !inNew:
			change = "deleted"
		}
		if patch == "" {
			// An empty file was added or deleted: nothing to show line-wise.
			patch = headerOnly(p, inOld, inNew)
		}
		out = append(out, core.FileDiff{Path: p, Change: change, Additions: adds, Deletions: dels, Patch: patch})
	}
	return out, nil
}

func headerOnly(p string, inOld, inNew bool) string {
	from, to := "a/"+p, "b/"+p
	if !inOld {
		from = "/dev/null"
	}
	if !inNew {
		to = "/dev/null"
	}
	return fmt.Sprintf("--- %s\n+++ %s\n", from, to)
}

// Restore makes the live directory's text files match the snapshot: changed
// files are rewritten, added ones deleted and deleted ones recreated.
func Restore(snap, live string, exclude ...string) error {
	if !Exists(snap) {
		return fmt.Errorf("snapshot %s is missing", snap)
	}
	want, err := textFiles(snap, nil)
	if err != nil {
		return err
	}
	have, err := textFiles(live, append(exclude, snap))
	if err != nil {
		return err
	}
	var errs []error
	for rel := range have {
		if _, ok := want[rel]; !ok {
			if err := os.Remove(filepath.Join(live, filepath.FromSlash(rel))); err != nil {
				errs = append(errs, err)
			}
		}
	}
	for rel, b := range want {
		if cur, ok := have[rel]; ok && bytes.Equal(cur, b) {
			continue
		}
		p := filepath.Join(live, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("restore %s from its snapshot: %w", live, err)
	}
	return nil
}

// JoinPatches concatenates per-file patches into one patch file.
func JoinPatches(files []core.FileDiff) string {
	var b bytes.Buffer
	for _, f := range files {
		b.WriteString(f.Patch)
	}
	return b.String()
}
