package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"kindle-dash/internal/config"
	"kindle-dash/internal/lock"
	"kindle-dash/internal/metrics"
	"kindle-dash/internal/render"
	"kindle-dash/internal/transport"
)

// redirectLogsIfDaemon redirects stdout/stderr to log files when not connected
// to a terminal. Required when launched by Task Scheduler with -H windowsgui:
// the process has no console, so output would be silently discarded.
func redirectLogsIfDaemon() {
	fi, err := os.Stdout.Stat()
	if err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return // connected to a real terminal — no redirect needed
	}
	var logDir string
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
		}
		logDir = filepath.Join(base, "kindle-dash")
	} else {
		home, _ := os.UserHomeDir()
		logDir = filepath.Join(home, ".cache", "kindle-dash")
	}
	_ = os.MkdirAll(logDir, 0o700)
	if lf, err := os.OpenFile(filepath.Join(logDir, "run.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		os.Stdout = lf
	}
	if ef, err := os.OpenFile(filepath.Join(logDir, "run.err"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		os.Stderr = ef
	}
}

func cmdRun(args []string) {
	redirectLogsIfDaemon()
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	cfgPath := fs.String("config", "", "path to config.json (default: OS-conventional)")
	interval := fs.Float64("interval", 0, "override loop interval seconds")
	waveform := fs.String("waveform", "", "override fast-refresh waveform")
	flushEvery := fs.Int("flush-every", -1, "override how many rounds between gc16 full refreshes (0 = never)")
	noFarewell := fs.Bool("no-farewell", false, "skip the farewell screen on exit")
	_ = fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fatal("config: %v", err)
	}
	if *interval > 0 {
		cfg.Loop.IntervalSec = *interval
	}
	if *waveform != "" {
		cfg.Loop.Waveform = *waveform
	}
	if *flushEvery >= 0 {
		cfg.Loop.FlushEvery = *flushEvery
	}
	if *noFarewell {
		cfg.Loop.NoFarewell = true
	}
	if cfg.Kindle.Host == "" {
		fatal("no Kindle host configured — set kindle.host in %s", cfg.SourcePath)
	}

	// single-instance lock
	h, err := lock.Acquire(lock.DefaultPath())
	if err != nil {
		fatal("%v", err)
	}
	defer h.Release()

	// signal-driven graceful exit
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	ep := transport.Endpoint{
		Host:      cfg.Kindle.Host,
		Port:      cfg.Kindle.Port,
		User:      cfg.Kindle.User,
		Identity:  cfg.Kindle.Identity,
		EIPS:      cfg.Kindle.EIPS,
		RemotePNG: cfg.Kindle.RemotePNG,
	}
	fmt.Printf("kindle-dash: connecting to %s@%s:%d...\n", ep.User, ep.Host, ep.Port)
	cli, err := transport.Dial(ctx, ep)
	if err != nil {
		fatal("dial: %v", err)
	}
	defer cli.Close()
	fmt.Println("kindle-dash: connected.")

	if cfg.Loop.WelcomeSecs > 0 && len(cfg.Messages.Welcome) > 0 {
		frames, rerr := render.CockpitIntroFrames(cfg.Messages.Welcome, secs(cfg.Loop.WelcomeSecs))
		if rerr != nil {
			fmt.Fprintln(os.Stderr, "welcome render:", rerr)
		} else if !playFrames(ctx, cli, frames) {
			return // ctx cancelled during welcome — skip loop, go to farewell
		}
	}

	dash := render.NewDashboard()
	prov := metrics.NewDefaultProvider(metrics.Options{
		LHMUrl:      cfg.Temp.LHMUrl,
		LHMCacheTTL: time.Duration(cfg.Temp.CacheTTL * float64(time.Second)),
	})
	fmt.Printf("kindle-dash: loop (interval=%.1fs, waveform=%q, flush_every=%d)\n",
		cfg.Loop.IntervalSec, cfg.Loop.Waveform, cfg.Loop.FlushEvery)

	for round := 0; ctx.Err() == nil; round++ {
		t0 := time.Now()
		m := prov.Read()
		cols, nums, wrapped := dash.Compose(m)
		tag, err := pushTick(cli, dash, cols, nums, wrapped, round, cfg.Loop)
		dur := time.Since(t0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[error] round %d %s after %v: %v\n", round, tag, dur, err)
		} else {
			fmt.Printf("round %d  cpu=%s mem=%s gpu=%s vram=%s  push=%s  %v\n",
				round, ps(m.CPU), ps(m.Mem), ps(m.GPU), ps(m.VRAM), tag, dur.Round(time.Millisecond))
		}
		if !sleepCtx(ctx, secs(cfg.Loop.IntervalSec)) {
			break
		}
	}

	if !cfg.Loop.NoFarewell && len(cfg.Messages.Farewell) > 0 {
		frames, rerr := render.CockpitOutroFrames(cfg.Messages.Farewell)
		if rerr == nil {
			playFrames(ctx, cli, frames)
		}
	}
	fmt.Println("kindle-dash: exited cleanly.")
}

// pushTick performs the appropriate refresh strategy for this tick:
//   - first tick OR every flushEvery rounds: gc16 full refresh
//   - sweep wrapped: gc16 on chart areas + waveform on number bands
//   - otherwise: waveform on the new column + waveform on changed number bands
func pushTick(cli *transport.Client, dash *render.Dashboard, cols, nums []render.Region, wrapped bool, round int, lp config.Loop) (string, error) {
	if round == 0 || (lp.FlushEvery > 0 && round%lp.FlushEvery == 0) {
		png, err := dash.EncodePNG()
		if err != nil {
			return "encode", err
		}
		return "gc16 full", cli.FullRefresh(png, "gc16", false)
	}
	if wrapped {
		for _, r := range dash.ChartRegions() {
			p, err := dash.CropPNG(r)
			if err != nil {
				return "crop chart", err
			}
			if err := cli.PushRegion(p, r.X0, r.Y0, "gc16"); err != nil {
				return "push chart", err
			}
		}
		for _, r := range nums {
			p, err := dash.CropPNG(r)
			if err != nil {
				return "crop nums", err
			}
			if err := cli.PushRegion(p, r.X0, r.Y0, lp.Waveform); err != nil {
				return "push nums", err
			}
		}
		return fmt.Sprintf("wrap-reset + %d numbers", len(nums)), nil
	}
	for _, r := range cols {
		p, err := dash.CropPNG(r)
		if err != nil {
			return "crop col", err
		}
		if err := cli.PushRegion(p, r.X0, r.Y0, lp.Waveform); err != nil {
			return "push col", err
		}
	}
	for _, r := range nums {
		p, err := dash.CropPNG(r)
		if err != nil {
			return "crop nums", err
		}
		if err := cli.PushRegion(p, r.X0, r.Y0, lp.Waveform); err != nil {
			return "push nums", err
		}
	}
	return fmt.Sprintf("%d cols + %d numbers [%s]", len(cols), len(nums), lp.Waveform), nil
}

// playFrames pushes each animation frame and sleeps its Hold duration.
// Returns false if ctx is cancelled before the sequence finishes.
func playFrames(ctx context.Context, cli *transport.Client, frames []render.Frame) bool {
	for _, f := range frames {
		if err := cli.FullRefresh(f.PNG, f.Waveform, f.Clear); err != nil {
			fmt.Fprintln(os.Stderr, "animation push:", err)
		}
		if f.Hold > 0 && !sleepCtx(ctx, f.Hold) {
			return false
		}
	}
	return ctx.Err() == nil
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func secs(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}

func ps(v *float64) string {
	if v == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.0f%%", *v)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "kindle-dash: "+format+"\n", args...)
	os.Exit(1)
}
