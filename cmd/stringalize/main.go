// Command stringalize keeps an app's translation files in sync with a
// Stringalize project, through the project's public API.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// Set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `Stringalize keeps your app's translation files in sync with a Stringalize project.

Usage:
  stringalize <command> [flags]

Commands:
  init      Find the translation files in this folder and write stringalize.yml
  push      Upload the source-language files (--translations: every locale)
  pull      Download every locale into its file
  status    Show translation progress per locale
  version   Print the version

The API key is read from STRINGALIZE_KEY. Create one in the project's API dialog:
a read-only key is enough for pull and status, push needs full access.

Run "stringalize <command> -h" for a command's flags.
`

// errUsage marks a mistake in the command line, reported with exit code 2.
var errUsage = errors.New("usage")

// errFailed marks a command that ran but whose result means failure (status --strict).
var errFailed = errors.New("failed")

func main() {
	err := run(os.Args[1:], os.Stdout, os.Stderr)
	switch {
	case err == nil:
	case errors.Is(err, errUsage), errors.Is(err, flag.ErrHelp):
		os.Exit(2)
	case errors.Is(err, errFailed):
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return errUsage
	}
	command, args := args[0], args[1:]
	switch command {
	case "init":
		return runInit(args, stdout, stderr)
	case "push":
		return runPush(args, stdout, stderr)
	case "pull":
		return runPull(args, stdout, stderr)
	case "status":
		return runStatus(args, stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, "stringalize", version)
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", command, usage)
		return errUsage
	}
}

// globals are the flags every command that talks to the server accepts.
type globals struct {
	config string
	key    string
	server string
}

func newFlags(name, summary string, stderr io.Writer) (*flag.FlagSet, *globals) {
	g := &globals{}
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&g.config, "config", defaultConfig, "path of the configuration file")
	flags.StringVar(&g.key, "key", "", "project API key (default $STRINGALIZE_KEY)")
	flags.StringVar(&g.server, "server", "", "Stringalize address (default $STRINGALIZE_SERVER, then the config, then "+defaultServer+")")
	flags.Usage = func() {
		fmt.Fprintf(stderr, "Usage: stringalize %s [flags]\n\n%s\n\nFlags:\n", name, summary)
		flags.PrintDefaults()
	}
	return flags, g
}

func parse(flags *flag.FlagSet, args []string) error {
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(flags.Output(), "unexpected argument %q\n", flags.Arg(0))
		flags.Usage()
		return errUsage
	}
	return nil
}

// setup loads the configuration and builds a client from the flags and environment.
func (g *globals) setup() (*Config, *Client, error) {
	config, err := loadConfig(g.config)
	if err != nil {
		return nil, nil, err
	}
	client, err := g.client(config.Server)
	return config, client, err
}

func (g *globals) client(configured string) (*Client, error) {
	key := first(g.key, os.Getenv("STRINGALIZE_KEY"))
	if key == "" {
		return nil, errors.New("no API key: set STRINGALIZE_KEY, or pass -key. Create a key in the project's API dialog")
	}
	return newClient(first(g.server, os.Getenv("STRINGALIZE_SERVER"), configured, defaultServer), key), nil
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
