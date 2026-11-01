package main

import (
	"fmt"
	"io"
	"text/tabwriter"
)

func runStatus(args []string, stdout, stderr io.Writer) error {
	flags, g := newFlags("status", "Shows how much of each locale is translated, and how many translations have problems\nsuch as a missing placeholder.", stderr)
	strict := flags.Bool("strict", false, "exit with status 1 when a locale has untranslated, flagged or invalid strings")
	if err := parse(flags, args); err != nil {
		return err
	}
	// status needs only the server and key, so it also works without a configuration file.
	configured := ""
	if config, err := loadConfig(g.config); err == nil {
		configured = config.Server
	}
	client, err := g.client(configured)
	if err != nil {
		return err
	}

	project, err := client.Verify()
	if err != nil {
		return err
	}
	locales, err := client.Locales()
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%s\n\n", project.Project.Name)
	table := tabwriter.NewWriter(stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(table, "LOCALE\tTRANSLATED\tMISSING\tFLAGGED\tPROBLEMS\t")
	incomplete := false
	for _, locale := range locales {
		problems, err := client.Problems(locale.Code)
		if err != nil {
			return err
		}
		progress := locale.Progress
		total := progress.Translated + progress.Untranslated + progress.Flagged
		percent := 100
		if total > 0 {
			percent = progress.Translated * 100 / total
		}
		name := locale.Code
		if locale.Source {
			name += " (source)"
		}
		fmt.Fprintf(table, "%s\t%d%%\t%d\t%d\t%d\t\n", name, percent, progress.Untranslated, progress.Flagged, problems)
		if progress.Untranslated > 0 || progress.Flagged > 0 || problems > 0 {
			incomplete = true
		}
	}
	table.Flush()

	if *strict && incomplete {
		fmt.Fprintln(stderr, "\nSome locales are incomplete or have problems")
		return errFailed
	}
	return nil
}
