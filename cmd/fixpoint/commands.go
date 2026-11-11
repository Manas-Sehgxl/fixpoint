package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/config"
	"github.com/Manas-Sehgxl/fixpoint/internal/doctor"
	"github.com/Manas-Sehgxl/fixpoint/internal/store"
)

func runDoctor(args []string) int {
	fs, addr, shopAddr, data, stubs, autonomy := flags("doctor", args)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadConfig(*addr, *shopAddr, *autonomy)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	a, err := setup(ctx, cfg, *data, *stubs, false)
	if err != nil {
		return fail(err)
	}
	sys := a.system(ctx)
	rep := doctor.Report{Checks: []doctor.Check{
		doctor.FromTool(sys.Kane, true),
		doctor.FromTool(sys.Agent, true),
		doctor.Chrome(ctx),
		doctor.Target(ctx, sys.Target),
	}}
	fmt.Printf("fixpoint %s\n\n", Version)
	rep.Write(os.Stdout)
	fmt.Printf("\nConsole would run at %s\n", sys.ConsoleURL)
	if rep.Problems() {
		fmt.Println("\nSomething required is missing; the lines above say what to do.")
		return 1
	}
	fmt.Println("\nEverything Fixpoint needs is here. Run `fixpoint up`.")
	return 0
}

func runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	force := fs.Bool("force", false, "overwrite an existing fixpoint.json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if _, err := os.Stat(ConfigFile); err == nil && !*force {
		return fail(fmt.Errorf("%s already exists; pass --force to replace it", ConfigFile))
	}
	if err := store.WriteFileAtomic(ConfigFile, []byte(config.Starter)); err != nil {
		return fail(fmt.Errorf("write %s: %w", ConfigFile, err))
	}
	cfg, err := config.Load(ConfigFile)
	if err != nil {
		cfg = config.Default()
	}
	origin := config.OriginFor(cfg.Server.Addr)
	fmt.Printf("Wrote %s. Set target.url and target.dir to your own app.\n\n", ConfigFile)
	fmt.Println("Add this to your app's HTML, just before </body>:")
	fmt.Printf("\n  <script src=\"%s/widget.js\" data-fixpoint=\"%s\" defer></script>\n\n", origin, origin)
	fmt.Println("Then run `fixpoint doctor` and `fixpoint up`.")
	return 0
}

func runDemo(args []string) int {
	if len(args) == 0 || args[0] != "reset" {
		return fail(errors.New("the only demo command is `fixpoint demo reset --yes`"))
	}
	fs, addr, shopAddr, data, stubs, autonomy := flags("demo reset", args[1:])
	yes := fs.Bool("yes", false, "confirm: this deletes every report")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if !*yes {
		return fail(errors.New("this deletes every report, the suite and the artifacts; pass --yes to confirm"))
	}
	cfg, err := loadConfig(*addr, *shopAddr, *autonomy)
	if err != nil {
		return fail(err)
	}
	if !cfg.Demo() {
		return fail(errors.New("this project has its own target, so there is no demo to reset; delete target.url in fixpoint.json to use the demo"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	a, err := setup(ctx, cfg, *data, *stubs, false)
	if err != nil {
		return fail(err)
	}
	if err := a.resetDemo(); err != nil {
		return fail(err)
	}
	fmt.Println("The demo shop was re-seeded and the data directory cleared.")
	return 0
}
