package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
)

func runPush(args []string, stdout, stderr io.Writer) error {
	flags, g := newFlags("push", "Uploads the source-language files. New strings are created in the project and changed\nsource text is updated. Translations are left as they are unless -translations is given.", stderr)
	translations := flags.Bool("translations", false, "also upload every other locale's file, overwriting the project's translations with their text")
	tagNew := flags.String("tag-new", "", "tag strings that are new in the project")
	deleteAbsent := flags.Bool("delete-absent", false, "delete project strings that are not in the source file (one file only)")
	dryRun := flags.Bool("dry-run", false, "list the files that would be uploaded, without uploading")
	if err := parse(flags, args); err != nil {
		return err
	}
	config, client, err := g.setup()
	if err != nil {
		return err
	}
	if *deleteAbsent && len(config.Files) > 1 {
		// Each file would delete the strings of every other file.
		return errors.New("-delete-absent works only with a single file in stringalize.yml")
	}

	locales, err := client.Locales()
	if err != nil {
		return err
	}
	source := sourceLocale(locales)

	uploads := 0
	for _, file := range config.Files {
		if file.allLocales() {
			path := file.pathFor(*source)
			body, err := os.ReadFile(config.abs(path))
			if err != nil {
				return fmt.Errorf("source file: %w", err)
			}
			if !*translations {
				if body, err = sourceOnlyCatalog(body); err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
			}
			query := importQuery(file, true, *tagNew, *deleteAbsent)
			if err := upload(client, stdout, file.Format, path, "all locales", query, body, *dryRun); err != nil {
				return err
			}
			uploads++
			continue
		}

		for _, locale := range locales {
			if !locale.Source && !*translations {
				continue
			}
			path := file.pathFor(locale)
			body, err := os.ReadFile(config.abs(path))
			if errors.Is(err, os.ErrNotExist) && !locale.Source {
				continue // The app doesn't have this language yet.
			}
			if err != nil {
				return fmt.Errorf("source file: %w", err)
			}
			query := importQuery(file, locale.Source, *tagNew, *deleteAbsent)
			query.Set("locale", locale.Code)
			if err := upload(client, stdout, file.Format, path, locale.Code, query, body, *dryRun); err != nil {
				return err
			}
			uploads++
		}
	}

	if *dryRun {
		fmt.Fprintf(stdout, "%d files would be uploaded\n", uploads)
	}
	return nil
}

func importQuery(file FileConfig, source bool, tagNew string, deleteAbsent bool) url.Values {
	query := url.Values{}
	for name, value := range file.Import {
		query.Set(name, value)
	}
	if source {
		if tagNew != "" {
			query.Set("tag-new", tagNew)
		}
		if deleteAbsent {
			query.Set("delete-absent", "1")
		}
	} else {
		// Strings come from the source file; a translation file only fills them in,
		// and an empty translation never erases one made in Stringalize.
		query.Set("ignore-new", "1")
		query.Set("ignore-blank", "1")
	}
	if file.Tag != "" {
		query.Set("tag-all", file.Tag)
	}
	return query
}

func upload(client *Client, stdout io.Writer, format, path, label string, query url.Values, body []byte, dryRun bool) error {
	if dryRun {
		fmt.Fprintf(stdout, "would upload %s (%s)\n", path, label)
		return nil
	}
	result, err := client.Import(format, query, body)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	fmt.Fprintf(stdout, "%s (%s): %s\n", path, label, result.Message)
	for _, warning := range result.Warnings {
		fmt.Fprintf(stdout, "  warning: %s\n", warning)
	}
	return nil
}

// sourceOnlyCatalog removes every language but the source one from a String
// Catalog, so that pushing it creates and updates strings without touching
// the translations kept in Stringalize.
func sourceOnlyCatalog(data []byte) ([]byte, error) {
	var catalog map[string]any
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("not a valid String Catalog: %w", err)
	}
	language, _ := catalog["sourceLanguage"].(string)
	if language == "" {
		return nil, errors.New("the String Catalog has no sourceLanguage")
	}
	strings, _ := catalog["strings"].(map[string]any)
	for _, value := range strings {
		entry, _ := value.(map[string]any)
		localizations, _ := entry["localizations"].(map[string]any)
		for code := range localizations {
			if code != language {
				delete(localizations, code)
			}
		}
	}
	return json.Marshal(catalog)
}
