package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	defaultConfig = "stringalize.yml"
	defaultServer = "https://stringalize.app"
)

// Config is the content of stringalize.yml.
type Config struct {
	Server string       `yaml:"server"`
	Files  []FileConfig `yaml:"files"`

	// The directory holding the configuration; file paths are relative to it.
	dir string
}

// FileConfig maps one translation file in the repository to the project.
type FileConfig struct {
	// Path of each locale's file, with placeholders such as {locale}. A String
	// Catalog holds every locale, so its path has none.
	Path string `yaml:"path"`
	// Path of the source locale's file, when it doesn't follow Path
	// (Android's res/values/strings.xml, iOS's Base.lproj).
	Source string `yaml:"source,omitempty"`
	// File format, when the extension doesn't tell it (".json" is always JSON).
	Format string `yaml:"format,omitempty"`
	// Tag that keeps this file's strings apart when several files with
	// different strings share a project: push tags them, pull exports only them.
	Tag string `yaml:"tag,omitempty"`
	// Extra parameters for the export and import API, as documented in the
	// Stringalize README (fallback, status, no-expand, tag-new, ...).
	Export map[string]string `yaml:"export,omitempty"`
	Import map[string]string `yaml:"import,omitempty"`
}

var (
	placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)
	formats     = map[string]bool{"json": true, "strings": true, "stringsdict": true, "xml": true, "xlf": true, "xliff": true, "po": true, "csv": true, "xcstrings": true, "arb": true}
	variables   = map[string]bool{"locale": true, "locale_underscore": true, "android_locale": true, "language": true}
)

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s not found. Run `stringalize init` to create it", path)
	}
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	config.dir = filepath.Dir(absolute)

	if len(config.Files) == 0 {
		return nil, fmt.Errorf("%s lists no files", path)
	}
	for i := range config.Files {
		if err := config.Files[i].validate(); err != nil {
			return nil, fmt.Errorf("%s, file %d: %w", path, i+1, err)
		}
	}
	return &config, nil
}

func (f *FileConfig) validate() error {
	if f.Path == "" {
		return errors.New("path is missing")
	}
	if f.Format == "" {
		f.Format = strings.TrimPrefix(strings.ToLower(filepath.Ext(f.Path)), ".")
	}
	if !formats[f.Format] {
		return fmt.Errorf("unknown format %q; set format to one of json, strings, stringsdict, xml, xlf, po, csv, xcstrings, arb", f.Format)
	}
	for _, path := range []string{f.Path, f.Source} {
		for _, match := range placeholder.FindAllStringSubmatch(path, -1) {
			if !variables[match[1]] {
				return fmt.Errorf("unknown placeholder {%s} in %s; use {locale}, {locale_underscore}, {android_locale} or {language}", match[1], path)
			}
		}
	}
	if f.Source != "" && placeholder.MatchString(f.Source) {
		return fmt.Errorf("source %s names one file and can't contain placeholders", f.Source)
	}
	if f.allLocales() != (f.Format == "xcstrings") {
		if f.Format == "xcstrings" {
			return errors.New("a String Catalog holds every locale, so its path takes no placeholders")
		}
		return fmt.Errorf("path %s needs a placeholder such as {locale}, so each locale has its own file", f.Path)
	}
	return nil
}

// allLocales reports whether one file holds every locale.
func (f *FileConfig) allLocales() bool {
	return !placeholder.MatchString(f.Path)
}

// pathFor returns where a locale's file lives, relative to the configuration.
func (f *FileConfig) pathFor(locale Locale) string {
	if locale.Source && f.Source != "" {
		return f.Source
	}
	return placeholder.ReplaceAllStringFunc(f.Path, func(token string) string {
		switch token {
		case "{locale}":
			return locale.Code
		case "{locale_underscore}":
			return strings.ReplaceAll(locale.Code, "-", "_")
		case "{android_locale}":
			return androidQualifier(locale.Code)
		case "{language}":
			return strings.SplitN(locale.Code, "-", 2)[0]
		}
		return token
	})
}

var region = regexp.MustCompile(`^([A-Z]{2}|\d{3})$`)

// androidQualifier turns a locale code into the qualifier of a values folder:
// it, pt-rBR, b+sr+Latn. It matches the server's Android archive paths.
func androidQualifier(code string) string {
	parts := strings.Split(code, "-")
	switch {
	case len(parts) == 1:
		return parts[0]
	case len(parts) == 2 && region.MatchString(parts[1]):
		return parts[0] + "-r" + parts[1]
	default:
		return "b+" + strings.Join(parts, "+")
	}
}

func (c *Config) abs(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(c.dir, filepath.FromSlash(path))
}
