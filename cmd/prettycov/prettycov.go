package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	handleCommands(parseFlags())
}

func handleCommands(params flags) {
	// show usage info when no arguments or flags given.
	if flag.NFlag() == 0 && flag.NArg() == 0 {
		flag.Usage()
	}

	// show usage info when calling `prettycov -help or prettycov --help`
	if params.Help {
		flag.Usage()
	}

	// show version when calling `prettycov -version or prettycov --version`
	if params.Info {
		showVersion()
	}

	// handle commands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "help":
			showUsage()
		case "version":
			showVersion()
		default:
			showReport(params)
		}
	}
}

func failOnError(err error) {
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "An error occurred: %v\n", err)
		os.Exit(2)
	}
}
