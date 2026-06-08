package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"kindle-dash/internal/config"
	"kindle-dash/internal/lock"
	"kindle-dash/internal/metrics"
	"kindle-dash/internal/render"
	"kindle-dash/internal/transport"
)

func cmdRun(args []string) {
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
		png, rerr := render.RenderMessage(strings.Join(cfg.Messages.Welcome, "\n"))
		if rerr != nil {
			fmt.Fprintln(os.Stderr, "welcome render:", rerr)
		} else if perr := cli.FullRefresh(png, "gc16", true); perr != nil {
			fmt.Fprintln(os.Stderr, "welcome push:", perr)
		}
		if !sleepCtx(ctx, secs(cfg.Loop.WelcomeSecs)) {
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
		png, rerr := render.RenderMessage(strings.Join(cfg.Messages.Farewell, "\n"))
		if rerr == nil {
			if perr := cli.FullRefresh(png, "gc16", true); perr != nil {
				fmt.Fprintln(os.Stderr, "farewell push:", perr)
			}
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
