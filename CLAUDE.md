# Working in this repo

Fixpoint turns a plain-English bug report into a verified fix: a coding agent writes a
browser check, Kane CLI runs it, the agent patches the app, Kane re-runs the check and
every earlier fix's check. `docs/SPEC.md` is the full build spec and the reference for
anything this file leaves out.

## Build and test

```
go build -o /tmp/fixpoint ./cmd/fixpoint   # build
go vet ./... && go test ./...              # the whole check, must be clean
go run ./cmd/fixpoint up --stubs           # run it with the simulated runners
```

There is no npm step, no bundler and no generated code. `go test ./...` is hermetic: it
never starts a browser and never calls a model.

## Rules

- **Standard library only.** No third-party Go modules; `go.mod` has no `require` block
  and must keep none. Browser code is plain HTML, CSS and ES modules: no framework, no
  CDN, no request to another host.
- **The data contract lives in `internal/core/types.go`.** Its JSON encoding *is* the
  HTTP API, so the console, the widget and the engine agree by construction. Do not edit
  that file, `docs/SPEC.md` or `web/web.go`; if one of them is wrong, say so instead.
- **Web files are embedded** by `web/web.go` with `go:embed`. A change under
  `web/console/`, `web/shop/` or `web/widget/` is invisible until the binary is rebuilt,
  and the demo shop is re-seeded from the embedded copy into `.fixpoint/shop/` on
  `fixpoint up` and on `demo reset`.
- **Honesty.** Fixpoint must never show a result Kane did not produce. The stub runners
  exist for development and tests, are off unless `--stubs` is passed, and every surface
  says runs are simulated when they are on.
- **Style.** Idiomatic Go, small packages, doc comments on exported names saying what and
  why, table-driven tests where they fit. Errors say what failed and what to do. Sentence
  case in every string a person reads. No emoji anywhere.

## Ports

| Port | What |
|---|---|
| 7777 | the console and both APIs (`server.addr`) |
| 4000 | the demo shop (`server.shop_addr`) |
| 7801-7809 | spare, for console work against `web/dev/mock/server.mjs` |
| 7811-7819 | spare, for shop work |

Both defaults are overridable: `--addr`, `--shop-addr`, or `server` in `fixpoint.json`.
Another Fixpoint may already hold 7777 and 4000; pick a spare pair rather than killing it.

## Commits

Commit with the helper, naming only the paths you touched:

```
/home/claude/bin/fxcommit "Short imperative message" path1 path2
```

Do not run `git add`, `git commit`, `git stash`, `git reset`, `git checkout` or any branch
command. Messages are short, imperative, sentence case, no `feat:` prefix. **No
co-author or attribution lines** in the message; the helper adds what is needed.
