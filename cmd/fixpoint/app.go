package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/agent"
	"github.com/Manas-Sehgxl/fixpoint/internal/config"
	"github.com/Manas-Sehgxl/fixpoint/internal/core"
	"github.com/Manas-Sehgxl/fixpoint/internal/demo"
	"github.com/Manas-Sehgxl/fixpoint/internal/doctor"
	"github.com/Manas-Sehgxl/fixpoint/internal/events"
	"github.com/Manas-Sehgxl/fixpoint/internal/kane"
	"github.com/Manas-Sehgxl/fixpoint/internal/pipeline"
	"github.com/Manas-Sehgxl/fixpoint/internal/store"
	"github.com/Manas-Sehgxl/fixpoint/web"
)

// paths are the directories inside the data directory.
type paths struct {
	root      string
	data      string
	artifacts string
	snapshots string
	kane      string
	shop      string
}

func newPaths(root string) paths {
	return paths{root: root,
		data:      filepath.Join(root, "data"),
		artifacts: filepath.Join(root, "artifacts"),
		snapshots: filepath.Join(root, "snapshots"),
		kane:      filepath.Join(root, "kane"),
		shop:      filepath.Join(root, "shop"),
	}
}

func (p paths) make() error {
	for _, d := range []string{p.data, p.artifacts, p.snapshots, p.kane, p.shop} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	return nil
}

// app is everything `fixpoint up` wires together.
type app struct {
	cfg     config.Config
	paths   paths
	origin  string
	shopURL string
	target  pipeline.Target
	demo    bool
	stubs   bool
	store   *store.Store
	bus     *events.Bus
	worker  *pipeline.Worker
	kane    *kane.CLI
	agent   agent.Agent
	exclude []string
	// scenarios is the stub scenarios file in use, empty when none was found.
	scenarios string

	healthMu   sync.Mutex
	healthAt   time.Time
	healthBusy bool
	kaneInfo   core.ToolInfo
	agentInfo  core.ToolInfo
	reachable  bool
}

// setup builds the app: data directories, the demo shop when there is no
// target and seed is true, the runners, the store and the worker.
func setup(ctx context.Context, cfg config.Config, dataDir string, stubs, seed bool) (*app, error) {
	a := &app{cfg: cfg, paths: newPaths(dataDir), stubs: stubs, demo: cfg.Demo()}
	if err := a.paths.make(); err != nil {
		return nil, err
	}
	a.origin = config.OriginFor(cfg.Server.Addr)
	a.shopURL = config.OriginFor(cfg.Server.ShopAddr)

	a.target = pipeline.Target{Name: cfg.Target.Name, URL: cfg.Target.URL, Dir: cfg.Target.Dir,
		Notes: cfg.Target.Notes, RestartCmd: cfg.Target.RestartCmd, HealthURL: cfg.Target.HealthURL}
	if a.demo {
		// Only `up` and a reset seed: doctor must not disturb a shop that
		// another Fixpoint is working on. `up` keeps a shop that is already
		// there, because the proven reports and the suite describe its
		// patched files; `demo reset` starts both over.
		if seed && !exists(filepath.Join(a.paths.shop, "index.html")) {
			if err := demo.Seed(web.Shop(), a.paths.shop, a.origin); err != nil {
				return nil, err
			}
		}
		a.target.Name, a.target.URL, a.target.Dir = demo.Name, a.shopURL, a.paths.shop
		if a.target.Notes == "" {
			a.target.Notes = "Tapri is the built-in demo shop: a static site, plain ES modules in js/, prices in rupees. It is served straight from these files, so no restart is needed after a patch."
		}
	} else if a.target.Dir == "" {
		return nil, errors.New("target.url is set but target.dir is empty; set target.dir to the app's source directory")
	}
	absDir, err := filepath.Abs(a.target.Dir)
	if err != nil {
		return nil, fmt.Errorf("resolve target.dir: %w", err)
	}
	a.target.Dir = absDir
	if _, err := os.Stat(absDir); err != nil {
		return nil, fmt.Errorf("target.dir %s cannot be read: %w", absDir, err)
	}
	// Snapshots never follow the data directory, even if it sits inside the
	// target's own tree.
	if abs, err := filepath.Abs(a.paths.root); err == nil {
		a.exclude = []string{abs}
	}

	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	a.scenarios = scenariosPath()
	scenarios := a.scenarios
	kaneEnv := []string{}
	if stubs {
		kaneEnv = append(kaneEnv, "STUB_SCENARIOS="+scenarios, "STUB_TARGET_DIR="+a.target.Dir)
	}
	a.kane = &kane.CLI{Bin: cfg.Kane.Bin, WorkDir: a.paths.kane, TimeoutS: cfg.Kane.TimeoutS,
		MaxSteps: cfg.Kane.MaxSteps, Headless: cfg.Headless(), BugDetection: cfg.Kane.BugDetection, Env: kaneEnv}
	claude := &agent.Claude{Bin: cfg.Agent.Bin, Dir: a.target.Dir, Model: cfg.Agent.Model,
		Timeout: time.Duration(cfg.Agent.TimeoutS) * time.Second, Env: kaneEnv}
	if stubs {
		a.kane.Bin, a.kane.PrefixArgs, a.kane.Simulated = self, []string{"stub-kane"}, true
		claude.Bin, claude.PrefixArgs, claude.Simulated = self, []string{"stub-agent"}, true
	}
	a.agent = claude

	a.store, err = store.Open(a.paths.data)
	if err != nil {
		return nil, err
	}
	a.bus = events.New(a.store)
	a.worker = pipeline.New(pipeline.Options{Store: a.store, Bus: a.bus, Runner: a.kane, Agent: a.agent,
		Target: a.target, MaxAttempts: cfg.Pipeline.MaxAttempts, MaxInfoRounds: cfg.Pipeline.MaxInfoRounds,
		Autonomy: cfg.Pipeline.Autonomy, ArtifactsDir: a.paths.artifacts, SnapshotsDir: a.paths.snapshots,
		Exclude: a.exclude, Simulated: stubs})
	return a, nil
}

// scenariosPath finds the stub scenarios file: the environment wins, then
// web/dev/scenarios.json near the working directory or the binary.
func scenariosPath() string {
	if p := os.Getenv("STUB_SCENARIOS"); p != "" {
		return p
	}
	candidates := []string{
		filepath.Join("web", "dev", "scenarios.json"),
		filepath.Join("..", "web", "dev", "scenarios.json"),
	}
	if self, err := os.Executable(); err == nil {
		dir := filepath.Dir(self)
		candidates = append(candidates,
			filepath.Join(dir, "web", "dev", "scenarios.json"),
			filepath.Join(dir, "..", "web", "dev", "scenarios.json"))
	}
	for _, p := range candidates {
		if abs, err := filepath.Abs(p); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs
			}
		}
	}
	return ""
}

// snippet is the script tag an app pastes in to get the widget.
func (a *app) snippet() string {
	return fmt.Sprintf(`<script src="%s/widget.js" data-fixpoint="%s" defer></script>`, a.origin, a.origin)
}

// health reports Kane, the agent and the target. Probing them runs the real
// binaries (kane-cli whoami can take seconds), so only the first call waits:
// later calls get the last answer at once, and a stale one is refreshed in the
// background, one refresh at a time, without holding the lock while it probes.
func (a *app) health(ctx context.Context) (core.ToolInfo, core.ToolInfo, bool) {
	a.healthMu.Lock()
	if a.healthAt.IsZero() {
		defer a.healthMu.Unlock()
		a.kaneInfo, a.agentInfo, a.reachable = a.probeHealth(ctx)
		a.healthAt = time.Now()
		return a.kaneInfo, a.agentInfo, a.reachable
	}
	k, ag, reachable := a.kaneInfo, a.agentInfo, a.reachable
	if time.Since(a.healthAt) > 30*time.Second && !a.healthBusy {
		a.healthBusy = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			nk, nag, nr := a.probeHealth(ctx)
			a.healthMu.Lock()
			a.kaneInfo, a.agentInfo, a.reachable, a.healthAt, a.healthBusy = nk, nag, nr, time.Now(), false
			a.healthMu.Unlock()
		}()
	}
	a.healthMu.Unlock()
	return k, ag, reachable
}

// probeHealth runs the probes and returns what they found.
func (a *app) probeHealth(ctx context.Context) (core.ToolInfo, core.ToolInfo, bool) {
	k := a.kane.Health(ctx)
	if a.stubs {
		k.Name = "Kane CLI (simulated)"
	}
	ag := a.agent.Health(ctx)
	if a.stubs {
		ag.Name = a.agent.Name() + " (simulated)"
	}
	return k, ag, doctor.Reachable(ctx, firstNonEmpty(a.target.HealthURL, a.target.URL))
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// system builds the core.System the console reads.
func (a *app) system(ctx context.Context) core.System {
	k, ag, reachable := a.health(ctx)
	return core.System{
		Version:   Version,
		Simulated: a.stubs,
		Kane:      k,
		Agent:     ag,
		Target: core.TargetInfo{Name: a.target.Name, URL: a.target.URL, Dir: a.target.Dir,
			Reachable: reachable, Demo: a.demo},
		Queue:         a.worker.Queue(),
		MaxAttempts:   a.cfg.Pipeline.MaxAttempts,
		Autonomy:      a.cfg.Pipeline.Autonomy,
		ConsoleURL:    a.origin,
		WidgetSnippet: a.snippet(),
	}
}

// resetDemo re-seeds the shop and clears every report, the suite and the
// artifacts. The worker is held while it happens.
func (a *app) resetDemo() error {
	if !a.demo {
		return errors.New("only the built-in demo can be reset")
	}
	err := a.worker.Reset(func() error {
		if err := demo.Seed(web.Shop(), a.paths.shop, a.origin); err != nil {
			return err
		}
		if err := a.store.Reset(); err != nil {
			return err
		}
		// Kane's recorded tests go too: report numbers start again at 1, and an
		// old recording under a reused name would be replayed as the new check.
		for _, d := range []string{a.paths.artifacts, a.paths.snapshots, filepath.Join(a.paths.kane, ".testmuai", "tests")} {
			if err := os.RemoveAll(d); err != nil {
				return fmt.Errorf("clear %s: %w", d, err)
			}
			if err := os.MkdirAll(d, 0o755); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	a.bus.Publish(core.Event{Type: core.EvSystemChanged, Text: "The demo was reset: the shop, the reports and the suite are back to the start."})
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
