package main

import (
	"flag"
	"fmt"
	"os"

	"kindle-dash/internal/service"
)

func cmdInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	binPath := fs.String("bin", "", "path to kindle-dash binary to register (default: this executable)")
	_ = fs.Parse(args)

	bp := *binPath
	if bp == "" {
		exe, err := os.Executable()
		if err != nil {
			fatal("locate self: %v", err)
		}
		bp = exe
	}

	s := service.New()
	if err := s.Install(bp); err != nil {
		fatal("install: %v", err)
	}
	fmt.Printf("kindle-dash: registered autostart (%s).\n", service.TaskName)
	fmt.Printf("            binary:    %s\n", bp)
	fmt.Printf("            triggers:  on user login\n")
	fmt.Printf("            next step: `kindle-dash start` to launch now, or just log out and back in.\n")
}

func cmdUninstall(args []string) {
	s := service.New()
	// Best-effort stop before uninstall; ignore errors (it might not be running).
	_ = s.Stop()
	if err := s.Uninstall(); err != nil {
		fatal("uninstall: %v", err)
	}
	fmt.Println("kindle-dash: autostart removed and any running instance stopped.")
}

func cmdStart(args []string) {
	s := service.New()
	if err := s.Start(); err != nil {
		fatal("start: %v", err)
	}
	fmt.Println("kindle-dash: started.")
}

func cmdStop(args []string) {
	s := service.New()
	if err := s.Stop(); err != nil {
		fatal("stop: %v", err)
	}
	fmt.Println("kindle-dash: stopped.")
}

func cmdRestart(args []string) {
	s := service.New()
	_ = s.Stop()
	if err := s.Start(); err != nil {
		fatal("restart: %v", err)
	}
	fmt.Println("kindle-dash: restarted.")
}

func cmdStatus(args []string) {
	s := service.New()
	st, err := s.Status()
	if err != nil {
		fatal("status: %v", err)
	}
	fmt.Printf("installed: %v\n", st.Installed)
	fmt.Printf("running:   %v\n", st.Running)
	if st.Detail != "" {
		fmt.Println("---")
		fmt.Println(st.Detail)
	}
}
