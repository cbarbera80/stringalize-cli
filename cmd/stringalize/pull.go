package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
)

func runPull(args []string, stdout, stderr io.Writer) error {
	flags, g := newFlags("pull", "Downloads every locale of the project into its file. Files whose content is the same are\nnot rewritten. The source-language files are left alone: the repository owns them.", stderr)
	includeSource := flags.Bool("source", false, "also rewrite the source-language files")
	status := flags.String("status", "", "export only strings with this status, such as translated (overrides the config)")
	dryRun := flags.Bool("dry-run", false, "list the files that would change, without writing them")
	if err := parse(flags, args); err != nil {
		return err
	}
	config, client, err := g.setup()
	if err != nil {
		return err
	}
	locales, err := client.Locales()
	if err != nil {
		return err
	}

	changed := 0
	for _, file := range config.Files {
		query := exportQuery(file, *status)
		if file.allLocales() {
			// A String Catalog holds the source language too, so it is always written.
			data, err := client.ExportAll(file.Format, query)
			if err != nil {
				return fmt.Errorf("%s: %w", file.Path, err)
			}
			if changed, err = save(config, stdout, file.Path, data, *dryRun, changed); err != nil {
				return err
			}
			continue
		}

		for _, locale := range locales {
			if locale.Source && !*includeSource {
				continue
			}
			path := file.pathFor(locale)
			data, err := client.Export(locale.Code, file.Format, query)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if changed, err = save(config, stdout, path, data, *dryRun, changed); err != nil {
				return err
			}
		}
	}

	switch {
	case *dryRun:
		fmt.Fprintf(stdout, "%d files would change\n", changed)
	case changed == 0:
		fmt.Fprintln(stdout, "Everything is up to date")
	default:
		fmt.Fprintf(stdout, "%d files updated\n", changed)
	}
	return nil
}

func exportQuery(file FileConfig, status string) url.Values {
	query := url.Values{}
	for name, value := range file.Export {
		query.Set(name, value)
	}
	if status != "" {
		query.Set("status", status)
	}
	if file.Tag != "" {
		query.Set("filter", file.Tag)
	}
	return query
}

// save writes a file unless it already holds the same bytes, and returns the
// updated count of changed files.
func save(config *Config, stdout io.Writer, path string, data []byte, dryRun bool, changed int) (int, error) {
	target := config.abs(path)
	current, err := os.ReadFile(target)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return changed, err
	}
	if exists && bytes.Equal(current, data) {
		return changed, nil
	}

	verb := map[bool]string{true: "updated", false: "created"}[exists]
	if dryRun {
		fmt.Fprintf(stdout, "would be %s: %s\n", verb, path)
		return changed + 1, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return changed, err
	}
	// Written next to the target and renamed, so a failed write never leaves half a file.
	temporary, err := os.CreateTemp(filepath.Dir(target), ".stringalize-*")
	if err != nil {
		return changed, err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return changed, err
	}
	if err := temporary.Close(); err != nil {
		return changed, err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.Chmod(temporary.Name(), mode); err != nil {
		return changed, err
	}
	if err := os.Rename(temporary.Name(), target); err != nil {
		return changed, err
	}
	fmt.Fprintf(stdout, "%s %s\n", verb, path)
	return changed + 1, nil
}
