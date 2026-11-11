// Package web embeds the browser-side files into the fixpoint binary: the
// console, the bug-report widget and the demo shop's seed copy.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:console
var consoleFS embed.FS

//go:embed all:widget
var widgetFS embed.FS

//go:embed all:shop
var shopFS embed.FS

func sub(f embed.FS, dir string) fs.FS {
	s, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return s
}

// Console is the console single-page app (index.html) and the public status
// page (status.html).
func Console() fs.FS { return sub(consoleFS, "console") }

// Widget holds widget.js, the script other apps embed.
func Widget() fs.FS { return sub(widgetFS, "widget") }

// Shop is the demo shop as shipped, planted bugs included. It is copied into
// the data directory before use, so the embedded copy never changes.
func Shop() fs.FS { return sub(shopFS, "shop") }
