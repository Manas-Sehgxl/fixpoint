package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/config"
	"github.com/Manas-Sehgxl/fixpoint/internal/core"
	"github.com/Manas-Sehgxl/fixpoint/internal/demo"
	"github.com/Manas-Sehgxl/fixpoint/internal/server"
	"github.com/Manas-Sehgxl/fixpoint/web"
)

// ConfigFile is the settings file read from the current directory.
const ConfigFile = "fixpoint.json"

func flags(name string, args []string) (*flag.FlagSet, *string, *string, *string, *bool, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	addr := fs.String("addr", "", "console address, for example 127.0.0.1:7777")
	shopAddr := fs.String("shop-addr", "", "demo shop address, for example 127.0.0.1:4000")
	data := fs.String("data", ".fixpoint", "data directory")
	stubs := fs.Bool("stubs", false, "use the simulated runners (development only)")
	autonomy := fs.String("autonomy", "", "auto or ask")
	return fs, addr, shopAddr, data, stubs, autonomy
}

func loadConfig(addr, shopAddr, autonomy string) (config.Config, error) {
	cfg, err := config.Load(ConfigFile)
	if err != nil {
		return cfg, err
	}
	if addr != "" {
		cfg.Server.Addr = addr
	}
	if shopAddr != "" {
		cfg.Server.ShopAddr = shopAddr
	}
	if autonomy != "" {
		cfg.Pipeline.Autonomy = autonomy
	}
	return cfg, cfg.Validate()
}

func runUp(args []string) int {
	fs, addr, shopAddr, data, stubs, autonomy := flags("up", args)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadConfig(*addr, *shopAddr, *autonomy)
	if err != nil {
		return fail(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := setup(ctx, cfg, *data, *stubs, true)
	if err != nil {
		return fail(err)
	}

	// The demo shop's own server, so the target is reachable with no setup.
	var shopSrv *http.Server
	if a.demo {
		shopSrv = &http.Server{Addr: cfg.Server.ShopAddr, Handler: demo.Handler(a.paths.shop),
			ReadHeaderTimeout: 10 * time.Second}
		ln, err := net.Listen("tcp", cfg.Server.ShopAddr)
		if err != nil {
			return fail(fmt.Errorf("listen on %s for the demo shop: %w (is something else using that port? pass --shop-addr)", cfg.Server.ShopAddr, err))
		}
		go serve(shopSrv, ln, "demo shop")
	}

	reset := a.resetDemo
	if !a.demo {
		reset = nil
	}
	h := server.New(server.Deps{Store: a.store, Bus: a.bus, Worker: a.worker,
		System: func() core.System { return a.system(ctx) }, ArtifactsDir: a.paths.artifacts,
		Console: web.Console(), Widget: web.Widget(), ResetDemo: reset})
	srv := &http.Server{Addr: cfg.Server.Addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	ln, err := net.Listen("tcp", cfg.Server.Addr)
	if err != nil {
		return fail(fmt.Errorf("listen on %s for the console: %w (is Fixpoint already running? pass --addr)", cfg.Server.Addr, err))
	}
	go serve(srv, ln, "console")

	a.worker.Recover(ctx)
	workerDone := make(chan struct{})
	go func() { a.worker.Run(ctx); close(workerDone) }()

	printStatus(ctx, a)

	<-ctx.Done()
	fmt.Println("\nStopping: finishing the current step, then closing.")
	stop()
	shutCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	select {
	case <-workerDone:
	case <-shutCtx.Done():
	}
	_ = srv.Shutdown(shutCtx)
	if shopSrv != nil {
		_ = shopSrv.Shutdown(shutCtx)
	}
	fmt.Println("Stopped.")
	return 0
}

func serve(s *http.Server, ln net.Listener, what string) {
	if err := s.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "fixpoint: the %s server stopped: %v\n", what, err)
	}
}

// printStatus is the whole of `fixpoint up`'s output: where to go, and
// whether the two tools it depends on are ready.
func printStatus(ctx context.Context, a *app) {
	sys := a.system(ctx)
	fmt.Printf("\nFixpoint %s\n\n", Version)
	fmt.Printf("  Console   %s\n", sys.ConsoleURL)
	if a.demo {
		fmt.Printf("  Demo shop %s   (report a problem there with the button at the bottom right)\n", a.target.URL)
	} else {
		state := "answers"
		if !sys.Target.Reachable {
			state = "did not answer"
		}
		fmt.Printf("  Target    %s   (%s; source in %s)\n", a.target.URL, state, a.target.Dir)
	}
	fmt.Println()
	fmt.Printf("  %s\n", toolLine(sys.Kane))
	fmt.Printf("  %s\n", toolLine(sys.Agent))
	if !sys.Target.Reachable {
		fmt.Printf("  Target    %s did not answer. Start the app, or correct target.url in fixpoint.json.\n", a.target.URL)
	}
	if sys.Simulated {
		fmt.Println("\n  Simulated runners are on: no browser is being driven and nothing here is")
		fmt.Println("  real verification. Leave --stubs off to use Kane CLI and the coding agent.")
	}
	if a.cfg.Pipeline.Autonomy == "ask" {
		fmt.Println("\n  Autonomy is \"ask\": each reproduced report waits for you to approve the fix.")
	}
	fmt.Println("\nPress Ctrl+C to stop.")
}

func toolLine(t core.ToolInfo) string {
	name := fmt.Sprintf("%-26s", t.Name)
	if t.Problem != "" {
		return fmt.Sprintf("%s %s\n                             %s", name, t.Problem, t.Remedy)
	}
	bits := []string{}
	if t.Version != "" {
		bits = append(bits, t.Version)
	}
	if t.SignedIn == "yes" {
		bits = append(bits, "signed in")
	}
	if t.Stub {
		bits = append(bits, "simulated")
	}
	if len(bits) == 0 {
		bits = append(bits, "ready")
	}
	return name + " " + strings.Join(bits, ", ")
}
