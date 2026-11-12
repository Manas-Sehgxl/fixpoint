# Demo script: three minutes

For the Kane CLI hackathon submission video. The judged moment is the first one: a real
Kane run that fails, a real patch, and a real Kane run that passes. That has to land
inside the first 60 seconds, so everything else in this script is subordinate to it.

**One honesty rule for the whole recording.** Every Kane run and every patch on screen is
real, from one unbroken session. Waiting is cut, never faked: where the recording jumps
forward, the words "sped up" and the elapsed time stay on screen, and you say so out loud
once. Nothing in the video may be recorded with `--stubs`.

## Pre-flight

Do all of this before the camera rolls. Allow twenty minutes.

- [ ] `kane-cli whoami` exits 0. If not, `kane-cli login`.
- [ ] Enough Kane credits. Each run reports what it used (`credits_consumed` in
      `run_end`); the demo needs two runs, and you will want four or five retakes, so
      budget for about ten. Check your balance where you signed in before you start.
- [ ] `claude --version` prints a version, and Claude Code is signed in.
- [ ] `go run ./cmd/fixpoint doctor` says "Everything Fixpoint needs is here."
- [ ] Fixpoint stopped, then `go run ./cmd/fixpoint demo reset --yes`. That re-seeds the
      shop with its five bugs and clears every report, the suite and the artifacts. Do it
      with Fixpoint **stopped**; a running one keeps its reports in memory. (While it is
      running, use "Reset the demo" on the Setup page instead.)
- [ ] `go run ./cmd/fixpoint up`. Leave the terminal running, off camera.
- [ ] **Do a full dry run first, and time it.** File the report, let it reach Proven, note
      how long triage, the reproduce run, the agent and the verify run each took. Those
      numbers decide where your cuts go; this script cannot know them, because they depend
      on your machine, your model and the check Claude Code writes. Then reset again.
- [ ] After the dry run: Fixpoint stopped, `demo reset --yes`, Fixpoint started again, so
      the board is empty and the suite is empty. The board must show the empty state,
      "Report a problem to see the loop run".
- [ ] Record at 1920x1080. Two browser windows, no tabs but the one, bookmarks bar hidden,
      no notifications:
      - left, 820 px wide: the shop, <http://localhost:4000>, scrolled to the menu.
      - right, 1100 px wide: the console, <http://localhost:7777>, on the board.
- [ ] Zoom the console to 110% so the status words and the five-node track read on a
      phone screen.
- [ ] Have the case file URL ready to paste, and have `kane/` and the Suite page as the
      two places you go at the end.
- [ ] Clipboard empty. You will type the report by hand, on camera.

## The script

Clock is video time. Every line in quotes is what you say.

### 0:00-0:12 - the problem, in the app

**On screen:** the shop, left window, full frame.

"This is Tapri, a chai stall. Watch the total."

Click **Add to cart** on **Masala Chai**, then on **Elaichi Chai**. The cart shows
Subtotal, Delivery and Total. Click **Remove** on the Elaichi Chai line.

The line disappears. **Total does not move.** Let that sit for one beat with the cursor
next to the Total figure.

"The item is gone and the bill still says the same thing. That is a real bug in this
shop's cart code, and I have not told anything about it yet."

### 0:12-0:24 - the report

Press **Report a problem**, bottom right. Type this, exactly, slowly enough to read:

```
I removed an item but the total didn't change
```

"That is all a user gives you. No file, no steps, no stack trace."

Click **Send the report**. The panel turns into a status view: "Report #1 received",
"Report received".

### 0:24-0:40 - red: Kane reproduces it

**On screen:** cut to both windows, console on the right taking the eye.

The board shows report #1. "Claude Code is reading the report to write a check." Then the
case file opens (click the report title) and the Check written section appears with the
objective in it.

"Claude Code read the report and wrote one plain-English check. Kane CLI is running it in
a real browser right now - those steps are arriving live."

Kane's steps arrive in the live panel. The run ends red.

"Kane reproduced it. That is the point of this first run: **a failure here is the
evidence.** Kane captured the value the page showed, a screenshot of the failing step and
the console log, and all of it goes straight into the next prompt."

Point at **Failed at step N** and at the captured **Value seen**.

> If the real run is longer than this slot, cut here and keep "sped up" and the elapsed
> time on screen. Say it once: "sped up from here."

### 0:40-0:52 - the patch

The case file grows an **Attempt 1 of 3** section: root cause, one-sentence summary, the
file changed, and the unified diff.

"Claude Code got Kane's evidence and patched the app. One file, a handful of lines, and it
names the cause: the remove path never recomputed the bill."

Open the diff. Scroll it once, slowly. Do not read it out.

### 0:52-1:00 - green: Kane verifies it

"Now the same check runs again, unchanged, against the patched app."

The verify run goes green. The status reaches **Proven** and the cobweb plot closes on the
fixed point.

"Kane says it passes. Red, patch, green - and nobody touched it."

**Hard gate: the green run must be on screen by 1:00.** If the dry run says it will not
be, cut earlier and more, not later.

### 1:00-1:25 - the proof

Scroll to the **Proven** section. The failing and passing screenshots are side by side
with a draggable divider; drag it once across.

"Here is what closes the report: Kane's failing run, the patch, and Kane's passing run.
The before and after are Kane's own screenshots, not mine."

Click **Download the patch** so the file lands visibly. Then, back in the shop window,
reload: the total updates when you remove an item now.

"And the fix is in the app. That was the user's own words to a working fix, with proof."

### 1:25-1:50 - Kane's three jobs

**On screen:** the **Suite** page.

"Kane does three different jobs in this loop, and that layering is the whole design."

"One, reproducer: the first run has to fail, or there is nothing to fix. Two, fix
verifier: the identical check runs again after the patch. Three, regression suite - this
page. Every check that ever proved a fix is kept and re-run after every later patch, so a
fix that breaks an earlier fix never counts as a fix."

Show test 1 on the page, "Proved the fix for report #1", "Passed". Expand **Show the
check** so the objective is visible.

"`--name` made Kane record that run as a replayable test, so re-running it replays the
recording instead of re-authoring the flow."

### 1:50-2:15 - the second report, to show the suite bite

File a second report from the shop, on a different bug, and send it:

```
the coupon chai10 says it is not valid
```

"Second report, different bug, same loop. The difference is at the end."

Skip to the **Earlier fixes re-checked** section of the new report.

"Before this one is allowed to be called proven, Kane re-runs the first fix's check. Both
have to pass. That is the suite doing its job on its first day."

> This whole beat needs a real second loop. Record it separately, before the main take,
> and cut it in; or cut from the moment the report is filed straight to the
> "Earlier fixes re-checked" section with "sped up" on screen.

### 2:15-2:40 - what happens when it does not work

**On screen:** the board, then a case file in `escalated` if you have one; otherwise the
loop diagram in the README.

"It does not always work, and it does not pretend to. Three attempts, each one given
Kane's newest evidence. If none of them makes the check pass, every patch is rolled back
from a snapshot taken before the first attempt, and the report goes to a person with the
check, the runs, the screenshots and every diff attached. And if Kane's first run passes,
the bug is not reproduced, so the reporter gets one question instead of a wrong fix.
Fixpoint never shows a result Kane did not produce."

### 2:40-3:00 - how to run it

**On screen:** a terminal, large type.

```
go run ./cmd/fixpoint up
```

"One command. It starts the console, the demo shop and the loop. Kane CLI and Claude
Code have to be installed and signed in, and `fixpoint doctor` tells you which one is
not. Point it at your own app with one script tag and two lines of config."

Last frame: the Proven case file, the before and after side by side, held still for two
seconds.

## If a live run fails on camera: the 30-second fallback

Keep a finished, **real** proven report from an earlier session in a second data directory
(`--data .fixpoint-keep`), so you can start Fixpoint on it and the board already shows it.
Nothing in the fallback is simulated.

1. **0:00-0:08.** The shop, with the bug: remove an item, the total does not move. Say
   the sentence from 0:00.
2. **0:08-0:20.** Cut to the kept case file, already Proven. Scroll from Kane's failing
   run to the diff to Kane's passing run in one movement. "Kane failed it, Claude Code
   patched it, Kane passed it. Red, patch, green."
3. **0:20-0:26.** The Proven section: before and after screenshots, drag the divider.
   "Kane's own screenshots."
4. **0:26-0:30.** The Suite page: "and the check stays, and re-runs after every later
   patch."

Say once, plainly: "This run was recorded earlier; it is a real Kane run, not a
simulation." Then offer the live one again at the end if the clock allows.

**Do not** fall back to `--stubs` on camera, in any circumstance. The console would carry
"Simulated runners are on. No browser is being driven and nothing here is real
verification" on every page, which is the correct thing for it to say and the wrong thing
to submit.
