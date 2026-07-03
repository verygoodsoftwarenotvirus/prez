// Command prez is a terminal UI for triaging GitHub pull requests you
// need to review, distinguishing PRs that genuinely need your attention from
// ones you've already reviewed where nothing has changed since.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/verygoodsoftwarenotvirus/prez/internal/config"
	"github.com/verygoodsoftwarenotvirus/prez/internal/ghauth"
	"github.com/verygoodsoftwarenotvirus/prez/internal/ghclient"
	"github.com/verygoodsoftwarenotvirus/prez/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "prez:", err)
		os.Exit(1)
	}
}

func run() error {
	defaultPath, err := config.DefaultPath()
	if err != nil {
		defaultPath = "config.yaml"
	}

	if len(os.Args) > 1 && os.Args[1] == "init" {
		return runInit(defaultPath, os.Args[2:])
	}

	path := flag.String("config", defaultPath, "path to config.yaml")
	flag.Parse()

	profiles, err := config.Load(*path)
	if err != nil {
		return err
	}

	token, err := ghauth.Token(context.Background())
	if err != nil {
		return err
	}

	client := ghclient.New(token)

	model := tui.New(profiles, client)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func runInit(defaultPath string, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	path := fs.String("config", defaultPath, "path to write the config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if err := config.Init(*path); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "prez: wrote annotated config to %s\n", *path)
	return nil
}
