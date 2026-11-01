package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Folders that hold build output, dependencies or tooling, never the app's own files.
var skipped = map[string]bool{
	".git": true, ".build": true, ".gradle": true, ".idea": true, ".dart_tool": true, ".swiftpm": true,
	"build": true, "DerivedData": true, "Pods": true, "Carthage": true, "node_modules": true, "vendor": true, "dist": true,
	"intermediates": true, "generated": true,
}

// Folders whose JSON files are usually one locale each.
var jsonFolders = map[string]bool{"locales": true, "locale": true, "i18n": true, "l10n": true, "lang": true, "langs": true, "translations": true, "messages": true}

func runInit(args []string, stdout, stderr io.Writer) error {
	flags, g := newFlags("init", "Looks for translation files under the current folder and writes stringalize.yml.\nWith STRINGALIZE_KEY set, the project's source locale tells which files are the source.", stderr)
	sourceCode := flags.String("source-locale", "", "source locale of the app, when no API key is set (default en)")
	force := flags.Bool("force", false, "overwrite an existing configuration")
	if err := parse(flags, args); err != nil {
		return err
	}
	if _, err := os.Stat(g.config); err == nil && !*force {
		return fmt.Errorf("%s already exists; pass -force to replace it", g.config)
	}

	source := *sourceCode
	if source == "" {
		if client, err := g.client(""); err == nil {
			locales, err := client.Locales()
			if err != nil {
				return err
			}
			source = sourceLocale(locales).Code
		} else {
			source = "en"
			fmt.Fprintln(stdout, "No API key set, assuming the source locale is en (change it with -source-locale)")
		}
	}

	root := filepath.Dir(g.config)
	files, err := detect(root, source)
	if err != nil {
		return err
	}
	if err := os.WriteFile(g.config, []byte(renderConfig(files, g.server)), 0o644); err != nil {
		return err
	}

	if len(files) == 0 {
		fmt.Fprintf(stdout, "No translation files found. Edit %s to add them.\n", g.config)
		return nil
	}
	fmt.Fprintf(stdout, "Wrote %s with %d files:\n", g.config, len(files))
	for _, file := range files {
		fmt.Fprintf(stdout, "  %s\n", file.Path)
	}
	fmt.Fprintln(stdout, "\nCheck it, then run `stringalize push` to upload the source strings.")
	return nil
}

// detect finds the source-language translation files under root and returns
// a file entry for each, with the locale replaced by a placeholder.
func detect(root, source string) ([]FileConfig, error) {
	lproj := source + ".lproj"
	arb := regexp.MustCompile(`^(.*[_-])` + regexp.QuoteMeta(strings.ReplaceAll(source, "-", "_")) + `\.arb$`)
	var found []FileConfig
	seen := map[string]bool{}
	add := func(file FileConfig) {
		if !seen[file.Path] {
			seen[file.Path] = true
			found = append(found, file)
		}
	}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != root && (skipped[name] || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		dir, parent := filepath.ToSlash(filepath.Dir(relative)), filepath.Base(filepath.Dir(path))
		inside := func(folder string) string {
			return strings.TrimPrefix(filepath.ToSlash(filepath.Join(filepath.Dir(dir), folder, name)), "./")
		}

		switch ext := filepath.Ext(name); {
		case ext == ".xcstrings":
			add(FileConfig{Path: relative})
		case (ext == ".strings" || ext == ".stringsdict") && parent == lproj:
			add(FileConfig{Path: inside("{locale}.lproj")})
		case (ext == ".strings" || ext == ".stringsdict") && parent == "Base.lproj":
			// A storyboard project keeps its source strings in Base.lproj.
			if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(path)), lproj, name)); err != nil {
				add(FileConfig{Path: inside("{locale}.lproj"), Source: relative})
			}
		case name == "strings.xml" && parent == "values" && filepath.Base(filepath.Dir(filepath.Dir(path))) == "res":
			add(FileConfig{Path: inside("values-{android_locale}"), Source: relative})
		case ext == ".arb" && arb.MatchString(name):
			add(FileConfig{Path: dir + "/" + arb.FindStringSubmatch(name)[1] + "{locale_underscore}.arb"})
		case ext == ".json" && name == source+".json" && jsonFolders[parent]:
			add(FileConfig{Path: dir + "/{locale}.json"})
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrPermission) {
		return nil, err
	}
	for i := range found {
		found[i].Path = strings.TrimPrefix(found[i].Path, "./")
	}
	slices.SortFunc(found, func(a, b FileConfig) int { return strings.Compare(a.Path, b.Path) })
	assignTags(found)
	return found, nil
}

// assignTags gives each file its own tag when there are several, so that
// pull writes into each file only the strings pushed from it. An iOS
// .strings and .stringsdict of the same table share one.
func assignTags(files []FileConfig) {
	if len(files) < 2 {
		return
	}
	owner := map[string]string{}
	for i := range files {
		base, group := tagFor(files[i].Path)
		tag := base
		for n := 2; owner[tag] != "" && owner[tag] != group; n++ {
			tag = fmt.Sprintf("%s-%d", base, n)
		}
		owner[tag] = group
		files[i].Tag = tag
	}
}

// tagFor returns a file's tag and what identifies the strings it holds.
func tagFor(path string) (tag, group string) {
	name := filepath.Base(path)
	table := strings.TrimSuffix(name, filepath.Ext(name))
	switch filepath.Ext(name) {
	case ".strings", ".stringsdict", ".xcstrings":
		group = filepath.Dir(filepath.Dir(path)) + "/" + table
		if filepath.Ext(name) == ".xcstrings" {
			group = filepath.Dir(path) + "/" + table
		}
		if table == "Localizable" {
			return "ios", group
		}
		return "ios-" + strings.ToLower(table), group
	case ".xml":
		return "android", path
	case ".arb":
		return "flutter", path
	default:
		return "web", path
	}
}

func renderConfig(files []FileConfig, server string) string {
	var out strings.Builder
	out.WriteString("# Stringalize CLI configuration: https://github.com/cbarbera80/stringalize-cli\n")
	out.WriteString("# The API key is read from STRINGALIZE_KEY. Don't write it here.\n\n")
	if server != "" && server != defaultServer {
		fmt.Fprintf(&out, "server: %s\n\n", server)
	}
	out.WriteString("# Paths are relative to this file. Placeholders: {locale} (pt-BR), {locale_underscore} (pt_BR),\n")
	out.WriteString("# {android_locale} (pt-rBR), {language} (pt). `source` is the source-language file when it\n")
	out.WriteString("# doesn't follow `path`. `export` and `import` take the API's parameters, such as fallback.\n")
	if len(files) == 0 {
		out.WriteString("files:\n  # - path: MyApp/{locale}.lproj/Localizable.strings\n  # - path: app/src/main/res/values-{android_locale}/strings.xml\n  #   source: app/src/main/res/values/strings.xml\n  - path: \n")
		return out.String()
	}
	if len(files) > 1 {
		out.WriteString("#\n# Each file has a tag, so that pull writes into it only the strings pushed from it. Give\n")
		out.WriteString("# files the same tag, or remove the tags, when they really share their strings.\n")
	}
	out.WriteString("files:\n")
	for _, file := range files {
		fmt.Fprintf(&out, "  - path: %s\n", file.Path)
		if file.Source != "" {
			fmt.Fprintf(&out, "    source: %s\n", file.Source)
		}
		if file.Tag != "" {
			fmt.Fprintf(&out, "    tag: %s\n", file.Tag)
		}
	}
	return out.String()
}
