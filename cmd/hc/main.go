package main

import (
	"flag"
	"fmt"
	"os"

	"harnesscode-go/internal/commands"
)

func usage() {
	fmt.Println("HarnessCode Go CLI")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  hc [init|start|status|restore|uninstall]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  init       Initialize project configuration")
	fmt.Println("  start      Start development loop")
	fmt.Println("  status     Show project status and metrics")
	fmt.Println("  restore    Restore config files from backup")
	fmt.Println("  uninstall  Remove harnesscode data and agents")
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() < 1 {
		usage()
		os.Exit(1)
	}

	cmd := flag.Arg(0)

	switch cmd {
	case "init":
		if err := commands.Init(); err != nil {
			fmt.Fprintln(os.Stderr, "init error:", err)
			os.Exit(1)
		}
	case "start":
		if err := commands.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "start error:", err)
			os.Exit(1)
		}
	case "status":
		if err := commands.Status(); err != nil {
			fmt.Fprintln(os.Stderr, "status error:", err)
			os.Exit(1)
		}
	case "restore":
		if err := commands.Restore(); err != nil {
			fmt.Fprintln(os.Stderr, "restore error:", err)
			os.Exit(1)
		}
	case "uninstall":
		if err := commands.Uninstall(); err != nil {
			fmt.Fprintln(os.Stderr, "uninstall error:", err)
			os.Exit(1)
		}
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(1)
	}
}
