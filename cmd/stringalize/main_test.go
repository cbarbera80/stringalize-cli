package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestAndroidQualifier(t *testing.T) {
	cases := map[string]string{"it": "it", "pt-BR": "pt-rBR", "es-419": "es-r419", "sr-Latn": "b+sr+Latn", "zh-Hans-CN": "b+zh+Hans+CN"}
	for code, want := range cases {
		if got := androidQualifier(code); got != want {
			t.Errorf("androidQualifier(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestPathFor(t *testing.T) {
	file := FileConfig{Path: "app/res/values-{android_locale}/strings.xml", Source: "app/res/values/strings.xml"}
	if got := file.pathFor(Locale{Code: "pt-BR"}); got != "app/res/values-pt-rBR/strings.xml" {
		t.Errorf("translation path = %q", got)
	}
	if got := file.pathFor(Locale{Code: "en", Source: true}); got != "app/res/values/strings.xml" {
		t.Errorf("source path = %q", got)
	}
	arb := FileConfig{Path: "lib/l10n/app_{locale_underscore}.arb"}
	if got := arb.pathFor(Locale{Code: "pt-BR"}); got != "lib/l10n/app_pt_BR.arb" {
		t.Errorf("arb path = %q", got)
	}
	web := FileConfig{Path: "locales/{language}/{locale}.json"}
	if got := web.pathFor(Locale{Code: "pt-BR"}); got != "locales/pt/pt-BR.json" {
		t.Errorf("json path = %q", got)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		file  FileConfig
		error string
	}{
		{FileConfig{Path: "en.json"}, "needs a placeholder"},
		{FileConfig{Path: "{locale}/Localizable.xcstrings"}, "takes no placeholders"},
		{FileConfig{Path: "{lang}.json"}, "unknown placeholder {lang}"},
		{FileConfig{Path: "{locale}.txt"}, "unknown format"},
		{FileConfig{Path: "values-{android_locale}/strings.xml", Source: "values-{locale}/strings.xml"}, "can't contain placeholders"},
	}
	for _, c := range cases {
		err := c.file.validate()
		if err == nil || !strings.Contains(err.Error(), c.error) {
			t.Errorf("validate(%+v) = %v, want an error containing %q", c.file, err, c.error)
		}
	}

	ok := []FileConfig{{Path: "Localizable.xcstrings"}, {Path: "{locale}.lproj/Localizable.strings"}, {Path: "i18n/{locale}.txt", Format: "json"}}
	for _, file := range ok {
		if err := file.validate(); err != nil {
			t.Errorf("validate(%+v) = %v", file, err)
		}
	}
}

func TestSourceOnlyCatalog(t *testing.T) {
	catalog := `{"sourceLanguage":"en","version":"1.0","strings":{
		"Hello":{"localizations":{"en":{"stringUnit":{"state":"translated","value":"Hello"}},"it":{"stringUnit":{"state":"translated","value":"Ciao"}}}},
		"Bare":{}}}`
	data, err := sourceOnlyCatalog([]byte(catalog))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Ciao") || !strings.Contains(string(data), `"value":"Hello"`) || !strings.Contains(string(data), `"Bare"`) {
		t.Errorf("unexpected catalog: %s", data)
	}
	if _, err := sourceOnlyCatalog([]byte(`{"strings":{}}`)); err == nil {
		t.Error("a catalog without sourceLanguage was accepted")
	}
}

func TestDetect(t *testing.T) {
	root := t.TempDir()
	write(t, root, "ios/App/en.lproj/Localizable.strings", "")
	write(t, root, "ios/App/en.lproj/Localizable.stringsdict", "")
	write(t, root, "ios/App/it.lproj/Localizable.strings", "")
	write(t, root, "ios/Legacy/Base.lproj/Main.strings", "")
	write(t, root, "ios/Catalog/Localizable.xcstrings", "")
	write(t, root, "android/app/src/main/res/values/strings.xml", "")
	write(t, root, "android/app/src/main/res/values-it/strings.xml", "")
	write(t, root, "android/app/build/intermediates/res/values/strings.xml", "")
	write(t, root, "flutter/lib/l10n/app_en.arb", "")
	write(t, root, "flutter/lib/l10n/app_it.arb", "")
	write(t, root, "web/src/locales/en.json", "")
	write(t, root, "web/node_modules/lib/locales/en.json", "")
	write(t, root, "web/package.json", "")

	files, err := detect(root, "en")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, file := range files {
		got = append(got, strings.TrimSpace(file.Tag+" "+file.Path+" "+file.Source))
	}
	want := []string{
		"android android/app/src/main/res/values-{android_locale}/strings.xml android/app/src/main/res/values/strings.xml",
		"flutter flutter/lib/l10n/app_{locale_underscore}.arb",
		"ios ios/App/{locale}.lproj/Localizable.strings",
		"ios ios/App/{locale}.lproj/Localizable.stringsdict",
		"ios-2 ios/Catalog/Localizable.xcstrings",
		"ios-main ios/Legacy/{locale}.lproj/Main.strings ios/Legacy/Base.lproj/Main.strings",
		"web web/src/locales/{locale}.json",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("detected:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, file := range files {
		if err := file.validate(); err != nil {
			t.Errorf("detected file %s is invalid: %v", file.Path, err)
		}
	}
}

func TestInitWritesAValidConfig(t *testing.T) {
	root := t.TempDir()
	write(t, root, "App/en.lproj/Localizable.strings", "")
	server := fakeServer(t)
	config := filepath.Join(root, "stringalize.yml")

	out := runOK(t, "init", "-config", config, "-key", "k", "-server", server.URL)
	if !strings.Contains(out, "App/{locale}.lproj/Localizable.strings") {
		t.Errorf("unexpected output: %s", out)
	}
	loaded, err := loadConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server != server.URL || len(loaded.Files) != 1 {
		t.Errorf("unexpected config: %+v", loaded)
	}
	if _, err := runCommand("init", "-config", config, "-key", "k", "-server", server.URL); err == nil {
		t.Error("init replaced an existing configuration")
	}
}

func TestPushUploadsTheSourceOnly(t *testing.T) {
	server := fakeServer(t)
	root := project(t, server.URL, `
  - path: App/{locale}.lproj/Localizable.strings
    tag: app
  - path: res/values-{android_locale}/strings.xml
    source: res/values/strings.xml
    import:
      tag-updated: changed`)
	write(t, root, "App/en.lproj/Localizable.strings", `"a" = "A";`)
	write(t, root, "App/it.lproj/Localizable.strings", `"a" = "Ah";`)
	write(t, root, "res/values/strings.xml", "<resources/>")

	runOK(t, "push", "-config", filepath.Join(root, "stringalize.yml"), "-key", "k", "-tag-new", "new")
	got := server.imports()
	want := []string{
		`strings locale=en&tag-all=app&tag-new=new "a" = "A";`,
		`xml locale=en&tag-new=new&tag-updated=changed <resources/>`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("imports:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestPushTranslations(t *testing.T) {
	server := fakeServer(t)
	root := project(t, server.URL, `
  - path: App/{locale}.lproj/Localizable.strings`)
	write(t, root, "App/en.lproj/Localizable.strings", `"a" = "A";`)
	write(t, root, "App/it.lproj/Localizable.strings", `"a" = "Ah";`)
	// pt-BR has no file in the app yet, so it is skipped.

	runOK(t, "push", "-config", filepath.Join(root, "stringalize.yml"), "-key", "k", "-translations")
	want := []string{
		`strings locale=en "a" = "A";`,
		`strings ignore-blank=1&ignore-new=1&locale=it "a" = "Ah";`,
	}
	if got := server.imports(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("imports:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestPushFailsWithoutTheSourceFile(t *testing.T) {
	server := fakeServer(t)
	root := project(t, server.URL, `
  - path: App/{locale}.lproj/Localizable.strings`)
	_, err := runCommand("push", "-config", filepath.Join(root, "stringalize.yml"), "-key", "k")
	if err == nil || !strings.Contains(err.Error(), "en.lproj/Localizable.strings") {
		t.Errorf("err = %v", err)
	}
}

func TestPushRefusesDeleteAbsentWithSeveralFiles(t *testing.T) {
	server := fakeServer(t)
	root := project(t, server.URL, `
  - path: a/{locale}.json
  - path: b/{locale}.json`)
	_, err := runCommand("push", "-config", filepath.Join(root, "stringalize.yml"), "-key", "k", "-delete-absent")
	if err == nil || !strings.Contains(err.Error(), "single file") {
		t.Errorf("err = %v", err)
	}
	if len(server.imports()) != 0 {
		t.Error("something was uploaded")
	}
}

func TestPushStripsTranslationsFromACatalog(t *testing.T) {
	server := fakeServer(t)
	root := project(t, server.URL, `
  - path: Localizable.xcstrings`)
	write(t, root, "Localizable.xcstrings", `{"sourceLanguage":"en","strings":{"a":{"localizations":{"en":{"stringUnit":{"value":"A"}},"it":{"stringUnit":{"value":"Ah"}}}}}}`)

	runOK(t, "push", "-config", filepath.Join(root, "stringalize.yml"), "-key", "k")
	got := server.imports()
	if len(got) != 1 || strings.Contains(got[0], "Ah") || !strings.HasPrefix(got[0], "xcstrings  {") {
		t.Errorf("imports: %v", got)
	}
}

func TestPull(t *testing.T) {
	server := fakeServer(t)
	root := project(t, server.URL, `
  - path: res/values-{android_locale}/strings.xml
    source: res/values/strings.xml
    tag: android
    export:
      fallback: en`)
	write(t, root, "res/values/strings.xml", "mine")
	config := filepath.Join(root, "stringalize.yml")

	out := runOK(t, "pull", "-config", config, "-key", "k")
	if !strings.Contains(out, "created res/values-it/strings.xml") || !strings.Contains(out, "2 files updated") {
		t.Errorf("unexpected output: %s", out)
	}
	if got := read(t, root, "res/values-pt-rBR/strings.xml"); got != "xml pt-BR fallback=en&filter=android" {
		t.Errorf("pt-BR file = %q", got)
	}
	if got := read(t, root, "res/values/strings.xml"); got != "mine" {
		t.Errorf("the source file was rewritten: %q", got)
	}

	if out := runOK(t, "pull", "-config", config, "-key", "k"); !strings.Contains(out, "Everything is up to date") {
		t.Errorf("second pull: %s", out)
	}
	if out := runOK(t, "pull", "-config", config, "-key", "k", "-source", "-status", "translated", "-dry-run"); !strings.Contains(out, "3 files would change") {
		t.Errorf("dry run: %s", out)
	}
	if got := read(t, root, "res/values/strings.xml"); got != "mine" {
		t.Errorf("the dry run wrote a file: %q", got)
	}
}

func TestPullCatalog(t *testing.T) {
	server := fakeServer(t)
	root := project(t, server.URL, `
  - path: App/Localizable.xcstrings`)
	runOK(t, "pull", "-config", filepath.Join(root, "stringalize.yml"), "-key", "k")
	if got := read(t, root, "App/Localizable.xcstrings"); got != "xcstrings all " {
		t.Errorf("catalog = %q", got)
	}
}

func TestStatus(t *testing.T) {
	server := fakeServer(t)
	out := runOK(t, "status", "-config", filepath.Join(t.TempDir(), "none.yml"), "-key", "k", "-server", server.URL)
	for _, want := range []string{"Demo", "en (source)", "it ", "50%", "pt-BR"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output lacks %q:\n%s", want, out)
		}
	}
	if _, err := runCommand("status", "-key", "k", "-server", server.URL, "-strict"); !errors.Is(err, errFailed) {
		t.Errorf("strict status = %v", err)
	}
}

func TestErrors(t *testing.T) {
	server := fakeServer(t)
	if _, err := runCommand("status", "-key", "wrong", "-server", server.URL); err == nil || !strings.Contains(err.Error(), "Invalid or missing API key (HTTP 401)") {
		t.Errorf("err = %v", err)
	}
	t.Setenv("STRINGALIZE_KEY", "")
	if _, err := runCommand("status", "-server", server.URL); err == nil || !strings.Contains(err.Error(), "STRINGALIZE_KEY") {
		t.Errorf("err = %v", err)
	}
	if _, err := runCommand("push", "-config", filepath.Join(t.TempDir(), "missing.yml"), "-key", "k"); err == nil || !strings.Contains(err.Error(), "stringalize init") {
		t.Errorf("err = %v", err)
	}
	if _, err := runCommand("frobnicate"); !errors.Is(err, errUsage) {
		t.Errorf("err = %v", err)
	}
}

// server is a stand-in for the Stringalize public API.
type server struct {
	*httptest.Server
	mu       sync.Mutex
	received []string
}

func (s *server) imports() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.received...)
}

func fakeServer(t *testing.T) *server {
	s := &server{}
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, value any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(value)
	}
	mux.HandleFunc("GET /api/auth/verify", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"project": map[string]any{"name": "Demo", "slug": "demo"}, "key": map[string]any{"access": "full"}})
	})
	mux.HandleFunc("GET /api/locales", func(w http.ResponseWriter, r *http.Request) {
		reply(w, []map[string]any{
			{"code": "en", "source": true, "progress": map[string]int{"translated": 4}},
			{"code": "it", "progress": map[string]int{"translated": 2, "untranslated": 2}},
			{"code": "pt-BR", "progress": map[string]int{"translated": 4}},
		})
	})
	mux.HandleFunc("GET /api/locales/{code}/errors", func(w http.ResponseWriter, r *http.Request) {
		reply(w, []any{})
	})
	mux.HandleFunc("GET /api/export/locale/{file}", func(w http.ResponseWriter, r *http.Request) {
		code, format, _ := strings.Cut(r.PathValue("file"), ".")
		io.WriteString(w, format+" "+code+" "+r.URL.RawQuery)
	})
	mux.HandleFunc("GET /api/export/all.xcstrings", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "xcstrings all "+r.URL.RawQuery)
	})
	mux.HandleFunc("POST /api/import/{format}", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.received = append(s.received, r.PathValue("format")+" "+r.URL.RawQuery+" "+string(body))
		s.mu.Unlock()
		reply(w, map[string]any{"message": "1 assets created", "warnings": []string{}})
	})

	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Loco k" {
			w.WriteHeader(http.StatusUnauthorized)
			reply(w, map[string]any{"status": 401, "error": "Invalid or missing API key"})
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func project(t *testing.T, server, files string) string {
	root := t.TempDir()
	write(t, root, "stringalize.yml", "server: "+server+"\nfiles:"+files+"\n")
	return root
}

func runCommand(args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	err := run(args, &stdout, &stderr)
	return stdout.String() + stderr.String(), err
}

func runOK(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runCommand(args...)
	if err != nil {
		t.Fatalf("stringalize %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func write(t *testing.T, root, path, content string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
