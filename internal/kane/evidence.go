package kane

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
)

// Limits on what is copied out of an evidence pack.
const (
	MaxShots        = 40
	MaxShotBytes    = 8 << 20
	MaxFailureBytes = 8 << 10
	MaxConsoleLines = 20
)

var stepDir = regexp.MustCompile(`^tests/[^/]+/steps/(\d+)-[^/]+/(screenshot\.png|annotated\.png|failure\.yaml)$`)
var consoleLog = regexp.MustCompile(`^tests/[^/]+/logs/[^/]+-console\.ndjson$`)
var hintLine = regexp.MustCompile("evidence serve ([^`\\s]+)")

// safeName reports whether a zip entry name stays inside its destination.
func safeName(name string) bool {
	if name == "" || strings.Contains(name, `\`) || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return false
		}
	}
	return path.Clean(name) == name || path.Clean(name)+"/" == name
}

// ReadPack copies the useful parts of an evidence pack into destDir (the
// run's artifact folder) and describes them. urlPrefix is the URL path of
// destDir, for example "/artifacts/r_x/run_3". Every part is optional, and
// entries whose names would escape destDir are refused.
func ReadPack(packPath, destDir, urlPrefix string) (*core.Evidence, error) {
	zr, err := zip.OpenReader(packPath)
	if err != nil {
		return nil, fmt.Errorf("open the evidence pack %s: %w", packPath, err)
	}
	defer zr.Close()
	ev := &core.Evidence{PackPath: packPath, Shots: []core.Shot{}, ConsoleErrors: []string{}}
	shots := map[int]*core.Shot{}
	failedStep := map[int]bool{}
	type consoleFile struct {
		name string
		f    *zip.File
	}
	var consoles []consoleFile
	for _, f := range zr.File {
		if !safeName(f.Name) {
			continue
		}
		if consoleLog.MatchString(f.Name) {
			consoles = append(consoles, consoleFile{f.Name, f})
			continue
		}
		m := stepDir.FindStringSubmatch(f.Name)
		if m == nil {
			continue
		}
		ord, _ := strconv.Atoi(m[1])
		switch m[2] {
		case "failure.yaml":
			failedStep[ord] = true
			if ev.FailureText == "" {
				if b, err := readLimited(f, MaxFailureBytes); err == nil {
					ev.FailureText = string(b)
				}
			}
		default:
			if f.UncompressedSize64 > MaxShotBytes {
				continue
			}
			s := shots[ord]
			if s == nil {
				if len(shots) >= MaxShots {
					continue
				}
				s = &core.Shot{Step: ord}
				shots[ord] = s
			}
			name := fmt.Sprintf("step-%d.png", ord)
			if m[2] == "annotated.png" {
				name = fmt.Sprintf("step-%d-annotated.png", ord)
			}
			if err := extract(f, filepath.Join(destDir, name)); err != nil {
				continue
			}
			if m[2] == "annotated.png" {
				s.AnnotatedURL = urlPrefix + "/" + name
			} else {
				s.URL = urlPrefix + "/" + name
			}
		}
	}
	ords := make([]int, 0, len(shots))
	for o, s := range shots {
		if s.URL == "" {
			s.URL = s.AnnotatedURL
		}
		if s.URL != "" {
			ords = append(ords, o)
		}
	}
	sort.Ints(ords)
	for _, o := range ords {
		s := shots[o]
		s.Failed = failedStep[o]
		ev.Shots = append(ev.Shots, *s)
	}
	sort.Slice(consoles, func(i, j int) bool { return consoles[i].name < consoles[j].name })
	for _, c := range consoles {
		if len(ev.ConsoleErrors) >= MaxConsoleLines {
			break
		}
		ev.ConsoleErrors = append(ev.ConsoleErrors, consoleErrors(c.f, MaxConsoleLines-len(ev.ConsoleErrors))...)
	}
	return ev, nil
}

func readLimited(f *zip.File, max int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, max))
}

func extract(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(rc, MaxShotBytes))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// consoleErrors returns up to max error-level messages from a console log.
func consoleErrors(f *zip.File, max int) []string {
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	var out []string
	sc := bufio.NewScanner(io.LimitReader(rc, 4<<20))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() && len(out) < max {
		var l map[string]any
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		level := strings.ToLower(firstString(l, "level", "type", "severity"))
		if level != "error" && level != "severe" {
			continue
		}
		msg := firstString(l, "text", "message", "msg", "args")
		if msg == "" {
			continue
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		out = append(out, msg)
	}
	return out
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if v != "" {
				return v
			}
		case nil:
		default:
			b, _ := json.Marshal(v)
			return string(b)
		}
	}
	return ""
}

// FindPack looks for the run's evidence pack: the stderr hint, then the
// session directory, then the newest pack in the working directory written
// after the run started. It returns "" when there is none.
func FindPack(stderr, sessionDir, workDir string, started time.Time) string {
	if ms := hintLine.FindAllStringSubmatch(stderr, -1); len(ms) > 0 {
		p := expandHome(ms[len(ms)-1][1])
		if !filepath.IsAbs(p) {
			p = filepath.Join(workDir, p)
		}
		if isFile(p) {
			return p
		}
	}
	if sessionDir != "" {
		matches, _ := filepath.Glob(filepath.Join(expandHome(sessionDir), "evidence", "*.evidence"))
		if p := newest(matches, time.Time{}); p != "" {
			return p
		}
	}
	matches, _ := filepath.Glob(filepath.Join(workDir, ".testmuai", "evidence", "*.evidence"))
	return newest(matches, started.Add(-2*time.Second))
}

func newest(paths []string, after time.Time) string {
	best, bestT := "", after
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		if info.ModTime().After(bestT) || (best == "" && after.IsZero()) {
			best, bestT = p, info.ModTime()
		}
	}
	return best
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
