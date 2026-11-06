// Command jptranslate runs the local Japanese translation API, a small
// client, and the pinned official model downloader.
package main

import (
	"fmt"
	"os"
	"strings"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "jptranslate:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return fmt.Errorf("a command is required")
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "translate":
		return runTranslate(args[1:])
	case "model":
		return runModel(args[1:])
	case "version", "--version", "-version":
		fmt.Println(version)
		return nil
	case "help", "--help", "-h":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, strings.TrimSpace(`
local-jp-translate: local Japanese translation API backed by PLaMo 3 Translate

Usage:
  jptranslate serve [flags]              Start the loopback HTTP API
  jptranslate translate [text] [flags]   Translate one text through the API
  jptranslate model info                 Show pinned model metadata and license
  jptranslate model verify --dir DIR     Verify a downloaded model file
  jptranslate model download --dir DIR --acknowledge-license
  jptranslate version

Run "jptranslate <command> --help" for command flags.
`)+"\n")
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}
