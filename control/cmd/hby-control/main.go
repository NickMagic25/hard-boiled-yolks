package main

import (
	"fmt"
	"log"
	"os"
)

const version = "0.1.0"

func main() {
	log.SetFlags(0)

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("hby-control: %v", err)
	}

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "run":
		cmdline := commandArgs(os.Args[2:])
		if len(cmdline) == 0 {
			log.Fatal("hby-control: run requires a command")
		}
		if !cfg.Enabled {
			os.Exit(runDirect(cfg, cmdline))
		}
		os.Exit(runSupervisor(cfg, cmdline))
	case "serve":
		if !cfg.Enabled {
			log.Print("hby-control: disabled by HBY_CONTROL_ENABLED=false")
			return
		}
		os.Exit(runSupervisor(cfg, nil))
	case "version":
		fmt.Println(version)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  hby-control run -- <command> [args...]")
	fmt.Fprintln(os.Stderr, "  hby-control serve")
	fmt.Fprintln(os.Stderr, "  hby-control version")
}

func commandArgs(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}
	return args
}
