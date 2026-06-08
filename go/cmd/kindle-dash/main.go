package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"kindle-dash/internal/metrics"
	"kindle-dash/internal/render"
)

const version = "0.0.1"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		cmdRun(os.Args[2:])
	case "once":
		cmdOnce(os.Args[2:])
	case "message":
		cmdMessage(os.Args[2:])
	case "doctor":
		cmdDoctor(os.Args[2:])
	case "install":
		cmdInstall(os.Args[2:])
	case "uninstall":
		cmdUninstall(os.Args[2:])
	case "start":
		cmdStart(os.Args[2:])
	case "stop":
		cmdStop(os.Args[2:])
	case "restart":
		cmdRestart(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("kindle-dash", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: kindle-dash <command> [options]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  run [--interval ...] [--waveform ...] Foreground loop: push dashboard to Kindle")
	fmt.Fprintln(os.Stderr, "  install [--bin path]                  Register login-time autostart (user-level, no admin)")
	fmt.Fprintln(os.Stderr, "  uninstall                             Stop + remove autostart")
	fmt.Fprintln(os.Stderr, "  start | stop | restart | status       Manage the registered instance")
	fmt.Fprintln(os.Stderr, "  once [--json] [--save out.png]        Read metrics once; --save also renders dashboard PNG")
	fmt.Fprintln(os.Stderr, "  message --save out.png \"text\"          Render centered message PNG")
	fmt.Fprintln(os.Stderr, "  doctor [--host ... --identity ...]    Diagnose SSH + Kindle reachability")
	fmt.Fprintln(os.Stderr, "  version                               Print version")
	fmt.Fprintln(os.Stderr, "  help                                  Show this message")
}

func cmdOnce(args []string) {
	fs := flag.NewFlagSet("once", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output as JSON")
	savePath := fs.String("save", "", "also render dashboard to this PNG path")
	_ = fs.Parse(args)

	p := metrics.NewDefaultProvider()
	m := p.Read()

	if *asJSON {
		b, _ := json.MarshalIndent(m, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Println("--- kindle-dash metrics ---")
		fmt.Printf("CPU  %%: %s\n", fmtFloat(m.CPU))
		fmt.Printf("Mem  %%: %s\n", fmtFloat(m.Mem))
		fmt.Printf("GPU  %%: %s\n", fmtFloat(m.GPU))
		fmt.Printf("VRAM %%: %s\n", fmtFloat(m.VRAM))
		fmt.Printf("CPU  C: %s\n", fmtFloat(m.CPUTemp))
		fmt.Printf("GPU  C: %s\n", fmtFloat(m.GPUTemp))
	}

	if *savePath != "" {
		dash := render.NewDashboard()
		dash.Compose(m)
		if err := dash.SavePNG(*savePath); err != nil {
			fmt.Fprintln(os.Stderr, "save:", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "saved dashboard to", *savePath)
	}
}

func cmdMessage(args []string) {
	fs := flag.NewFlagSet("message", flag.ExitOnError)
	savePath := fs.String("save", "", "PNG path to save (required without transport)")
	_ = fs.Parse(args)
	text := strings.Join(fs.Args(), " ")
	if text == "" {
		fmt.Fprintln(os.Stderr, "message: text required")
		os.Exit(2)
	}
	png, err := render.RenderMessage(text)
	if err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
	if *savePath == "" {
		fmt.Fprintln(os.Stderr, "message: --save required (push to Kindle not wired up in this stage)")
		os.Exit(2)
	}
	if err := os.WriteFile(*savePath, png, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "saved message PNG to", *savePath)
}

func fmtFloat(v *float64) string {
	if v == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.1f", *v)
}
