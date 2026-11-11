package server

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// getWidget serves widget.js, the one file other apps load.
func (s *Server) getWidget(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(s.d.Widget, "widget.js")
	if err != nil {
		writeError(w, http.StatusNotFound, "The widget script is missing from this build.")
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(b)
}

// statusPage serves the reporter's status page for /r/{id}.
func (s *Server) statusPage(w http.ResponseWriter, r *http.Request) {
	s.serveConsoleFile(w, r, "status.html")
}

// static serves the console's files, and its index.html for every other
// non-API GET path, because the console routes in the browser.
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "No such endpoint.")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "Only GET is allowed here.")
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		s.serveConsoleFile(w, r, "index.html")
		return
	}
	if f, err := s.d.Console.Open(name); err == nil {
		info, serr := f.Stat()
		f.Close()
		if serr == nil && !info.IsDir() {
			s.serveConsoleFile(w, r, name)
			return
		}
	}
	// A path that looks like a file but is not there is a 404; anything
	// else is a console route.
	if ext := path.Ext(name); ext != "" && ext != ".html" {
		writeError(w, http.StatusNotFound, "No such file.")
		return
	}
	s.serveConsoleFile(w, r, "index.html")
}

var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".svg":   "image/svg+xml",
	".json":  "application/json; charset=utf-8",
	".png":   "image/png",
	".woff2": "font/woff2",
	".ico":   "image/x-icon",
	".txt":   "text/plain; charset=utf-8",
	".map":   "application/json; charset=utf-8",
}

func (s *Server) serveConsoleFile(w http.ResponseWriter, r *http.Request, name string) {
	f, err := s.d.Console.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeError(w, http.StatusNotFound, "The console files are missing from this build: "+name)
			return
		}
		writeError(w, http.StatusInternalServerError, "The console file could not be read.")
		return
	}
	defer f.Close()
	if ct, ok := contentTypes[path.Ext(name)]; ok {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "no-cache")
	if rs, ok := f.(io.ReadSeeker); ok {
		if info, err := f.Stat(); err == nil {
			http.ServeContent(w, r, name, info.ModTime(), rs)
			return
		}
	}
	io.Copy(w, f)
}
