# Fixpoint

Bug reports that close themselves.

A web app embeds one script tag. A user clicks "Report a problem" and types what went
wrong. Fixpoint then runs a loop with nobody in it: a coding agent writes one
plain-English browser check from the report, **Kane CLI runs that check in a real browser
and reproduces the bug**, the agent patches the app's source, **Kane re-runs the same
check and the check of every fix that came before it**, and the report closes with the
failing run, the patch and the passing run side by side. The check stays behind as a
regression test. Three failed attempts roll every patch back and hand the report to a
person with everything attached.

The name is the idea: iterate until the result stops changing.

```
go run ./cmd/fixpoint up
```

That starts the console, the demo shop and the loop. Nothing else to configure.

## Prerequisites

| | |
|---|---|
| Go 1.24 or newer | to build and run Fixpoint. Nothing else: standard library only, no `go get`. |
| Kane CLI | `npm install -g @testmuai/kane-cli`, then `kane-cli login`. Every check is run by Kane, in a real browser, on your machine. |
| Claude Code | installed and signed in. It writes the checks and the patches. |
| Chrome | Kane drives it. |

`fixpoint doctor` tells you which of those is missing and what to type:

```
$ go run ./cmd/fixpoint doctor
fixpoint 0.1.0

no  Kane CLI     Kane CLI is not installed.
              Install Kane CLI: `npm install -g @testmuai/kane-cli`, then `kane-cli login`.
ok  Claude Code  2.1.322 (Claude Code) at /opt/node22/bin/claude
ok  Chrome       /usr/bin/google-chrome
ok  Target       the built-in demo shop, served by `fixpoint up` at http://localhost:4000

Console would run at http://localhost:7777

Something required is missing; the lines above say what to do.
```

It exits 1 while something required is missing, so it fits in a setup script.

## Run it

```
go run ./cmd/fixpoint up                 # console, demo shop and the loop
go run ./cmd/fixpoint up --stubs         # no credentials needed; nothing is verified
go run ./cmd/fixpoint doctor             # is Kane CLI, the agent, Chrome and the target ready
go run ./cmd/fixpoint init               # write a starter fixpoint.json for your own app
go run ./cmd/fixpoint demo reset --yes   # re-seed the demo shop, clear reports and the suite
go run ./cmd/fixpoint version
```

`up` prints where everything is and whether the two tools it depends on are ready. This
transcript is from a machine with no Kane CLI, so it was taken with `--stubs` (and on spare
ports, because another Fixpoint held the defaults):

```
$ go run ./cmd/fixpoint up --stubs --addr 127.0.0.1:7821 --shop-addr 127.0.0.1:7822

Fixpoint 0.1.0

  Console   http://localhost:7821
  Demo shop http://localhost:7822   (report a problem there with the button at the bottom right)

  Kane CLI (simulated)       stub-kane 0.0.0, signed in, simulated
  Claude Code (simulated)    stub-agent 0.0.0 (Claude Code stand-in), signed in, simulated

  Simulated runners are on: no browser is being driven and nothing here is
  real verification. Leave --stubs off to use Kane CLI and the coding agent.

Press Ctrl+C to stop.
```

With the real runners the two tool lines carry Kane CLI's and Claude Code's own versions,
there is no simulated notice, and the addresses are `http://localhost:7777` and
`http://localhost:4000`.

Flags: `--addr` (console, default `127.0.0.1:7777`), `--shop-addr` (demo shop, default
`127.0.0.1:4000`), `--data` (working directory, default `.fixpoint`), `--stubs`,
`--autonomy auto|ask`. `demo reset --yes` is for a stopped Fixpoint; while one is running,
use "Reset the demo" on the console's Setup page, which also clears what the running
process holds in memory.

## The first two minutes

1. `go run ./cmd/fixpoint up`. Open the console at <http://localhost:7777> and the demo
   shop, **Tapri**, at <http://localhost:4000>. Put them side by side.
2. In the shop, add a couple of pouches of chai to the cart, then remove one. The list
   changes; the **Total** does not. That is a real bug in `js/cart.js`, one of five
   planted in the shop.
3. Press **Report a problem** at the bottom right and type, in your own words:
   `I removed an item but the total didn't change`. Send it.
4. Watch the console. The report appears on the board, Claude Code writes a check, Kane
   runs it and fails it, Claude Code patches `js/cart.js`, Kane runs the same check again
   and it passes. The case file at `/reports/{id}` shows the whole thing: the steps Kane
   took, its screenshots, the unified diff, and the failing and passing screenshots side
   by side.
5. Reload the shop. The total updates now. The check you caused is on the **Suite** page,
   and it will be re-run after every later patch.

## How the loop works

The status names below are the real ones, from `internal/core/types.go`.

```
  report --> triaging --> reproducing --> fixing --> verifying --> regressing --> proven
             (agent)        (Kane)       (agent)      (Kane)        (Kane)
                |              |                         |              |
    decision    |       exit 0 | not reproduced   exit 1 | still   exit | an earlier
    "ask"       |              v                         | fails   1    | fix broke
                |         needs_info                     |              |
                |      (one question to                  v              v
                +---->  the reporter)           +----------------------------+
                              |                 |  attempt n+1: the agent is |
                              | reply,          |  given Kane's newest run   |
                              |  re-queued      |  and tries again           |
                              v                 +----------------------------+
                        queued, triaging                   |         |
                                                 fixing <--+         | after 3
                                                                     | attempts
                                                                     v
                                                     restore the baseline snapshot,
                                                              escalated
```

- `queued` -> `triaging`: the agent reads the report and answers `check` or `ask`. `ask`
  becomes `needs_info` with one question for the reporter; an answer re-queues the report,
  and triage runs again with the whole conversation in the prompt.
- `reproducing`: Kane runs the check. **A failure is the good outcome** - it means the bug
  is real and reproduced, with evidence. A pass means not reproduced, so the reporter gets
  the follow-up question the agent prepared. Two unanswered or unreproduced rounds close
  the report as `unreproduced`.
- `fixing`: the target directory is snapshotted, then the agent patches it. If it changed
  nothing, the attempt is `no_change`. If the check itself was wrong it may answer
  `revise_check` once, before any patch has landed, and the new check goes back to
  `reproducing`. `give_up` escalates.
- `verifying`: Kane runs **the same check** against the patched app. Still failing means
  the next attempt gets this run's evidence in its prompt.
- `regressing`: Kane re-runs the saved check of **every earlier proven fix**. One failure
  means the attempt `regressed` and does not count as a fix.
- `proven`: the check is saved as a suite test, the patch is written to
  `.fixpoint/artifacts/<report>/fix.patch`, and `Proof` records the failing run, the
  passing run, the whole diff from baseline, and the before and after screenshots.
- Three attempts without a pass: the baseline snapshot is restored, so the app is exactly
  as it was, and the status is `escalated` with everything Kane and the agent found.
- With `--autonomy ask`, a reproduced report waits in `awaiting_approval` until you press
  "Approve the fix".

One report is worked on at a time, because fixes change one working tree. The rest queue.

## How Kane CLI is used

Kane has three distinct jobs in Fixpoint, and the layering is the point: the same check is
the reproducer, then the verifier, then a permanent regression test.

**1. Reproducer.** The check the agent just wrote is run against the live app. Exit 1 is
what Fixpoint wants here.

```
KANE_CLI_USER_AGENT=fixpoint kane-cli run "<objective>" --agent --headless \
  --url <start url> --timeout 240 --max-steps 30 --name fp-7-r1 [--bug-detection continue]
```

**2. Fix verifier.** After the agent patches the source, the identical objective runs
again. Exit 0 is what Fixpoint wants here; anything else feeds the next attempt.

```
KANE_CLI_USER_AGENT=fixpoint kane-cli run "<objective>" --agent --headless \
  --url <start url> --timeout 240 --max-steps 30 --name fp-7-v1 [--bug-detection continue]
```

**3. Regression suite.** Every proven check is kept. After any later patch verifies, each
earlier check runs again. `--name` on the runs above made Kane record a replayable test at
`.fixpoint/kane/.testmuai/tests/<slug>_test.md`, so the re-run replays the recording
instead of re-authoring the flow:

```
KANE_CLI_USER_AGENT=fixpoint kane-cli testmd run <slug>_test.md --agent --headless --timeout 240
```

When no saved test exists the suite run falls back to the `run` form above. The console's
"Run all checks" runs the whole suite the same way.

Details Fixpoint relies on:

- `--agent` switches Kane's stdout to NDJSON, one JSON object per line, and stdin must not
  be a terminal. Fixpoint parses `stream_start`, the untyped `{"step":n,"status":...}`
  progress lines (those are the steps you see arriving live in the console), `run_end` and
  the `test_md_*` stream, and ignores event types it does not know.
- `--bug-detection continue` is passed only if `kane-cli run --help` lists it, which is
  probed once, the first time Kane is run.
- Kane runs with its working directory set to `.fixpoint/kane/`, so its `.testmuai/` and
  its evidence packs land inside Fixpoint's data directory and not in your repo.
- The `.evidence` pack is opened for each run: step screenshots become
  `.fixpoint/artifacts/<report>/<run>/step-<n>.png`, the first `failure.yaml` is kept as
  text, and up to 20 error-level browser console lines are collected. A run with no pack
  is still a valid run.
- Credits, duration, `test_url` and Kane's own bug verdict, when it confirms one, are
  recorded per run and shown in the case file.

### The closed loop

```
   report text ---> agent (triage) ---> objective ---> Kane run ---> exit code
                                                                     + evidence
                                                                         |
        patch <--- agent (fix) <--- prompt carrying Kane's steps, <------+
          |                         reason, captured values, failure
          |                         record and browser console errors
          v
    Kane run (the same objective, unchanged) ---> exit code ---> proven,
                                                                or round again
```

Nothing in between is a person. The agent's output *is* Kane's input (the objective), and
Kane's output *is* the agent's input (the evidence block of the fix prompt). What each
exit code means to the pipeline:

| Exit | Kane | Reproduce run | Verify run | Regression run |
|---|---|---|---|---|
| 0 | passed | not reproduced -> `needs_info`, with the agent's follow-up question | the fix holds -> `regressing` | this earlier fix still holds -> next test |
| 1 | failed | **reproduced** -> `fixing` (or `awaiting_approval`) | still failing -> attempt `still_failing`, next attempt gets this evidence | attempt `regressed`; the patch does not count |
| 2 | error before or outside the run | `error`, retryable, with the remedy: sign in with `kane-cli login`, check Chrome | same | same |
| 3 | timeout or cancelled | `error`, retryable, suggesting a larger `kane.timeout_s` | same | same |

For `testmd run` there is no exit 3: a timeout ends as exit 1, failed.

## Point it at your own app

Run this in your own app's directory, not in the Fixpoint repo (a `fixpoint.json` with a
`target.url` in the repo root turns the demo off):

```
go run ./cmd/fixpoint init
```

It writes a commented `fixpoint.json` and prints the script tag. Every key is optional:

```json
{
  "target":   { "name": "My app", "url": "http://localhost:3000", "dir": ".",
                "notes": "Anything the agent should know: test data, where prices come from.",
                "restart_cmd": "", "health_url": "" },
  "kane":     { "bin": "kane-cli", "timeout_s": 240, "max_steps": 30,
                "bug_detection": "continue", "headless": true },
  "agent":    { "preset": "claude", "bin": "claude", "model": "", "timeout_s": 600 },
  "pipeline": { "max_attempts": 3, "autonomy": "auto", "max_info_rounds": 2 },
  "server":   { "addr": "127.0.0.1:7777", "shop_addr": "127.0.0.1:4000" }
}
```

- `target.url` is what Kane opens; `target.dir` is the source the agent patches. Setting
  `target.url` turns the demo off. Leave both empty for the demo shop.
- `target.notes` goes into both prompts. Use it for anything a new developer would need.
- `target.restart_cmd` runs after every patch and every rollback, then Fixpoint waits for
  `target.health_url` (or `target.url`) to answer before calling Kane. A static site or a
  dev server with hot reload needs neither.
- `autonomy: "ask"` holds each reproduced report at `awaiting_approval` until you approve.

Then add the widget to every page, just before `</body>`:

```html
<script src="http://localhost:7777/widget.js" data-fixpoint="http://localhost:7777" defer></script>
```

It renders in a shadow root, so it cannot touch your styles and your styles cannot touch
it. It sends the report text, the page address, the reporter's last 20 actions in plain
English and any errors the page logged, and it shows the reporter exactly what will be
sent before they send it. Fields that look like a password, email, phone, name, address or
card are recorded as `Typed in "Phone"`, never with their value.

## Architecture

```
cmd/fixpoint/          the CLI: up, doctor, init, demo reset, and the hidden stub runners
internal/core/         the shared types; their JSON encoding is the HTTP API contract
internal/pipeline/     the worker and the loop: triage, reproduce, fix, verify, regress
internal/kane/         running Kane CLI, parsing its NDJSON stream, reading evidence packs
internal/agent/        the Claude Code adapter and the two prompts, behind an interface
internal/snapshot/     copy, unified diff and restore of the target directory
internal/store/        one JSON file per report, the suite, atomic writes
internal/server/       the console API, the public API and both event streams
internal/{config,demo,doctor,events,proc,stub,view}/
web/console/           the console and the public status page: HTML, CSS, ES modules
web/widget/widget.js   the one file an app embeds
web/shop/              Tapri, the demo shop, with five planted bugs
web/dev/scenarios.json what the simulated runners know about those bugs
kane/                  Kane checks of Fixpoint's own console (see below)
scripts/kane-verify.sh runs them and reports failures back to a coding agent
docs/SPEC.md           the full build spec
```

Working data lives in `.fixpoint/` (git-ignored): `data/` the reports and the suite,
`artifacts/` screenshots and patches, `snapshots/` the baseline and per-attempt copies,
`kane/` Kane's working directory, `shop/` the demo shop copy that actually gets patched.

### Checking Fixpoint with Kane

`kane/` holds four checks of Fixpoint's own console, written as plain `.md` objective
files: a one-line title, a `Start URL:` line, a `Needs:` line saying what state the console
has to be in, and the objective itself as the last paragraph.
They are not Kane `_test.md` recordings, because a recording cannot be committed and
replayed on another machine. `scripts/kane-verify.sh` runs each one with the `run` form
above and prints what failed; it also works as a Claude Code Stop hook
(`.claude/settings.json`) so an agent editing the console is told when it broke the board.
The hook skips itself when `FIXPOINT_AGENT=1`, so Fixpoint's own agent can never trigger
it, skips when nothing is listening and when Kane CLI is absent, and gives up after three
blocks.

## Honesty, and what `--stubs` is not

`--stubs` swaps in stand-in runners for development: no browser is driven and no model is
called. It exists so the loop can be worked on and tested without credentials, and because
the tests must be hermetic. With it on, the demo shop really is patched and really is
fixed, so the loop is faithful - but the browser and the model are stand-ins. With them on,
the demo report in "the first two minutes" above reaches `proven` in about 11 seconds, with
`js/cart.js` really patched (+1 -4) and the check really saved as suite test 1.

**`--stubs` is not verification.** A simulated run proves nothing about your app. Fixpoint
says so everywhere it could be mistaken: `System.Simulated` and every `Run.Simulated` are
true, the console carries a notice on every page, each run block says "Simulated run: the
stub runner produced this, not a real browser", and the reporter's status view says
"Simulated run". Real verification needs your own machine, with Kane CLI signed in and
Claude Code installed, and `--stubs` left off.

## Deliberately out of scope

Accounts and login. More than one target at a time. More than one report at a time.
Mobile targets. Deploying the patch, committing it or opening a pull request - Fixpoint
writes the diff and leaves it in your working tree. A database: reports are JSON files.
Any agent other than Claude Code, though the adapter is behind an interface so another
preset is a small job. No secrets are stored: Kane and the agent use their own sign-ins.

## Licence

MIT. See `LICENSE`.
