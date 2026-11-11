// Package demo seeds and serves the built-in demo shop. With no target in
// fixpoint.json, Fixpoint copies the embedded shop into the data directory,
// serves it and treats that copy as the app to fix, so the planted bugs can
// really be patched without touching the binary's files.
package demo

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Placeholder is the text in the shop's HTML that is replaced with the
// Fixpoint origin when the shop is copied.
const Placeholder = "{{FIXPOINT_ORIGIN}}"

// Name and URL defaults of the demo target.
const Name = "Tapri"

// Seed copies the embedded shop into dir, replacing the placeholder in HTML
// files with origin. Anything already in dir is removed first, so a reset
// really starts again.
func Seed(src fs.FS, dir, origin string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clear the demo shop directory %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create the demo shop directory %s: %w", dir, err)
	}
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		dst := filepath.Join(dir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return fmt.Errorf("read the embedded shop file %s: %w", p, err)
		}
		if strings.HasSuffix(p, ".html") {
			b = []byte(strings.ReplaceAll(string(b), Placeholder, origin))
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return fmt.Errorf("write the demo shop file %s: %w", dst, err)
		}
		return nil
	})
}

// Handler serves dir with no caching at all, so Kane always sees the
// patched files.
func Handler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		if r.URL.Path == "/healthz" {
			io.WriteString(w, "ok")
			return
		}
		files.ServeHTTP(w, r)
	})
}
