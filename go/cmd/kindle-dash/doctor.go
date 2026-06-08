package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"kindle-dash/internal/config"
	"kindle-dash/internal/transport"
)

const (
	ok   = "[OK]  "
	warn = "[--]  "
	bad  = "[!!]  "
)

func cmdDoctor(args []string) {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	cfgPath := fs.String("config", "", "path to config.json (default: OS-conventional)")
	host := fs.String("host", "", "override Kindle host")
	port := fs.Int("port", 0, "override Kindle port")
	user := fs.String("user", "", "override SSH user")
	identity := fs.String("identity", "", "override SSH private key path")
	_ = fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	if *host != "" {
		cfg.Kindle.Host = *host
	}
	if *port != 0 {
		cfg.Kindle.Port = *port
	}
	if *user != "" {
		cfg.Kindle.User = *user
	}
	if *identity != "" {
		cfg.Kindle.Identity = *identity
	}

	sectionConfig(cfg)
	sectionKeys(cfg)
	sectionAgent()
	sectionKnownHosts(cfg)

	if cfg.Kindle.Host == "" {
		fmt.Println()
		fmt.Println(bad + "no Kindle host configured — pass --host or write " + cfg.SourcePath)
		os.Exit(1)
	}

	if !sectionTCP(cfg) {
		os.Exit(1)
	}

	t, ok := sectionSSH(cfg)
	if !ok {
		os.Exit(1)
	}
	defer t.Close()

	sectionEIPS(t, cfg)
	fmt.Println("\nall checks passed.")
}

func sectionConfig(cfg config.Config) {
	fmt.Println("== config ==")
	fmt.Printf("  file:     %s\n", cfg.SourcePath)
	if _, err := os.Stat(cfg.SourcePath); err != nil {
		fmt.Printf("  %s(not present — using defaults; flags can override)\n", warn)
	} else {
		fmt.Printf("  %sfound\n", ok)
	}
	fmt.Printf("  host:     %q\n", cfg.Kindle.Host)
	fmt.Printf("  port:     %d\n", cfg.Kindle.Port)
	fmt.Printf("  user:     %q\n", cfg.Kindle.User)
	if cfg.Kindle.Identity == "" {
		fmt.Printf("  identity: (empty — auto-discover)\n")
	} else {
		fmt.Printf("  identity: %q\n", cfg.Kindle.Identity)
	}
	fmt.Printf("  eips:     %q\n", cfg.Kindle.EIPS)
}

func sectionKeys(cfg config.Config) {
	fmt.Println("\n== ssh keys (auto-discovery) ==")
	keys := transport.DiscoverKeys(cfg.Kindle.Identity)
	if len(keys) == 0 {
		fmt.Printf("  %sno keys found under ~/.ssh\n", bad)
		return
	}
	loadable := 0
	for _, k := range keys {
		tag := ok
		note := ""
		if k.Err != nil {
			tag = bad
			note = "error: " + k.Err.Error()
		} else if k.Encrypted {
			tag = warn
			note = "encrypted (needs ssh-agent or passphrase)"
		} else if !k.Loaded {
			tag = warn
			note = "not loaded"
		} else {
			loadable++
		}
		fmt.Printf("  %s%s", tag, k.Path)
		if k.Comment != "" {
			fmt.Printf("  (%s)", k.Comment)
		}
		if note != "" {
			fmt.Printf("  -- %s", note)
		}
		fmt.Println()
	}
	if loadable == 0 {
		fmt.Printf("  %sno key is usable without a passphrase; ensure ssh-agent has one loaded\n", warn)
	}
}

func sectionAgent() {
	fmt.Println("\n== ssh-agent ==")
	running, n, detail := transport.AgentStatus()
	if !running {
		fmt.Printf("  %snot running (%s)\n", warn, detail)
		fmt.Printf("        to enable on Windows (admin PowerShell):\n")
		fmt.Printf("          Set-Service ssh-agent -StartupType Automatic; Start-Service ssh-agent; ssh-add\n")
	} else {
		fmt.Printf("  %sconnected (%s); %d key(s) loaded\n", ok, detail, n)
	}
}

func sectionKnownHosts(cfg config.Config) {
	fmt.Println("\n== known_hosts ==")
	fmt.Printf("  file:     %s\n", transport.KnownHostsPath())
	if cfg.Kindle.Host == "" {
		fmt.Printf("  %sskipped (no host configured)\n", warn)
		return
	}
	present, err := transport.HasKnownHost(cfg.Kindle.Host)
	switch {
	case err != nil:
		fmt.Printf("  %sread error: %v\n", warn, err)
	case present:
		fmt.Printf("  %sentry for %s present\n", ok, cfg.Kindle.Host)
	default:
		fmt.Printf("  %sno entry for %s yet (will TOFU on first connect)\n", warn, cfg.Kindle.Host)
	}
}

func sectionTCP(cfg config.Config) bool {
	addr := net.JoinHostPort(cfg.Kindle.Host, strconv.Itoa(cfg.Kindle.Port))
	fmt.Printf("\n== tcp dial %s ==\n", addr)
	d := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		fmt.Printf("  %s%v\n", bad, err)
		return false
	}
	conn.Close()
	fmt.Printf("  %sreachable\n", ok)
	return true
}

func sectionSSH(cfg config.Config) (*transport.Client, bool) {
	addr := net.JoinHostPort(cfg.Kindle.Host, strconv.Itoa(cfg.Kindle.Port))
	fmt.Printf("\n== ssh handshake + auth (%s@%s) ==\n", cfg.Kindle.User, addr)
	ep := transport.Endpoint{
		Host:     cfg.Kindle.Host,
		Port:     cfg.Kindle.Port,
		User:     cfg.Kindle.User,
		Identity: cfg.Kindle.Identity,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	t, err := transport.Dial(ctx, ep)
	if err != nil {
		fmt.Printf("  %s%v\n", bad, err)
		return nil, false
	}
	fmt.Printf("  %sconnected and authenticated\n", ok)
	if err := t.Probe(ctx); err != nil {
		fmt.Printf("  %sprobe failed: %v\n", bad, err)
		t.Close()
		return nil, false
	}
	fmt.Printf("  %sremote exec ok\n", ok)
	return t, true
}

func sectionEIPS(t *transport.Client, cfg config.Config) {
	fmt.Printf("\n== remote eips (%s) ==\n", cfg.Kindle.EIPS)
	out, err := t.RunCommand("test -x " + cfg.Kindle.EIPS + " && echo present || echo missing")
	if err != nil {
		fmt.Printf("  %sexec error: %v\n", bad, err)
		return
	}
	got := strings.TrimSpace(out)
	if got == "present" {
		fmt.Printf("  %spresent\n", ok)
	} else {
		fmt.Printf("  %smissing (got %q)\n", bad, got)
	}
}
