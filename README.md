# Stringalize CLI

**Keep your app's translation files in sync with [Stringalize](https://stringalize.app), from your terminal or your CI.**

`stringalize push` uploads the strings you add in code. `stringalize pull` writes every translation back into the files your app already uses: `.xcstrings`, `.strings` and `.stringsdict`, Android `strings.xml`, Flutter ARB, JSON, XLIFF, PO, CSV.

```bash
stringalize init     # find the translation files and write stringalize.yml
stringalize push     # upload the source-language files
stringalize pull     # download every translation into its file
stringalize status   # progress per locale
```

- [Installation](#installation)
- [Getting started](#getting-started)
- [Configuration](#configuration)
- [Commands](#commands)
- [In CI](#in-ci)
- [FAQ](#faq)

## Installation

Download the binary for your system from the [latest release](https://github.com/cbarbera80/stringalize-cli/releases/latest). On macOS or Linux:

```bash
# macOS on Apple silicon; replace darwin_arm64 with darwin_amd64, linux_amd64 or linux_arm64
curl -fsSL https://github.com/cbarbera80/stringalize-cli/releases/latest/download/stringalize_darwin_arm64.tar.gz | tar xz stringalize
sudo mv stringalize /usr/local/bin/
```

With Go 1.24 or later:

```bash
go install github.com/cbarbera80/stringalize-cli/cmd/stringalize@latest
```

## Getting started

### 1. Create an API key

In Stringalize, open the project and choose **API**. Create a key with full access, and make it available to the CLI:

```bash
export STRINGALIZE_KEY=sgz_…
```

A read-only key is enough for `pull` and `status`. Keep the key out of the repository: use your shell profile, a `.envrc` with [direnv](https://direnv.net), or your CI's secrets.

### 2. Write the configuration

In the root of your repository:

```bash
stringalize init
```

`init` looks for translation files in the source language (`en.lproj/Localizable.strings`, `res/values/strings.xml`, `app_en.arb`, `Localizable.xcstrings`, `locales/en.json`) and writes `stringalize.yml`. Check it and commit it.

### 3. Upload your strings

```bash
stringalize push
```

The first time, if your app is already translated, upload the translations too:

```bash
stringalize push --translations
```

### 4. Download translations

Whenever translations change in Stringalize:

```bash
stringalize pull
```

Each locale's file is written where your app expects it. Files with no changes are not rewritten, so `git diff` shows only what changed.

## Configuration

`stringalize.yml` lists the translation files. Paths are relative to it.

```yaml
files:
  # iOS
  - path: MyApp/{locale}.lproj/Localizable.strings
    tag: ios
  - path: MyApp/{locale}.lproj/Localizable.stringsdict
    tag: ios

  # Android: the source language lives in values/, without a qualifier
  - path: app/src/main/res/values-{android_locale}/strings.xml
    source: app/src/main/res/values/strings.xml
    tag: android

  # A String Catalog holds every language in one file
  - path: Widget/Localizable.xcstrings
    tag: widget

  # JSON for a web app, with missing translations filled from English
  - path: web/src/locales/{locale}.json
    tag: web
    export:
      fallback: en
```

| Key | |
|---|---|
| `path` | Where each locale's file lives. Placeholders: `{locale}` (`pt-BR`), `{locale_underscore}` (`pt_BR`), `{android_locale}` (`pt-rBR`, `b+sr+Latn`), `{language}` (`pt`). A `.xcstrings` file holds every locale, so its path has none. |
| `source` | The source language's file, when it doesn't follow `path`: Android's `values/strings.xml`, or iOS's `Base.lproj`. |
| `format` | The file format, when the extension doesn't tell it: `json`, `strings`, `stringsdict`, `xml`, `xlf`, `po`, `csv`, `xcstrings`, `arb`. |
| `tag` | Keeps this file's strings apart from other files in the same project. `push` tags every string in the file, `pull` writes only strings with the tag. |
| `export` | Extra parameters for downloads, the same the export API takes: `fallback`, `status`, `no-expand`, `printf`, `filter`, `order`. |
| `import` | Extra parameters for uploads: `tag-new`, `tag-updated`, `ignore-blank`, `index`. |
| `server` | Your own Stringalize server, if you run one. |

### Several files in one project

A project is one set of strings, and `pull` writes the project's strings into every file. When files hold different strings (an iOS app and an Android app with their own keys, or `Localizable` and `InfoPlist` tables), give each a `tag`. `init` does this for you when it finds more than one file.

Files that share their strings, such as the `.strings` and `.stringsdict` of one table, take the same tag. Strings created in the Stringalize editor need the tag too, or `pull` leaves them out.

## Commands

### `stringalize push`

Uploads the source-language file of each entry. New strings are created, changed source text is updated (and, if the project is set up for it, its translations are marked fuzzy). Strings removed from the file stay in the project.

| Flag | |
|---|---|
| `--translations` | Also upload every other locale's file. Their text replaces the project's translations, so use it for the first upload, or when the files are where translators work. Empty translations never erase existing ones, and strings that aren't in the source are not created. |
| `--tag-new NAME` | Tag strings that are new in the project, for instance `needs-translation`. |
| `--delete-absent` | Delete project strings that are not in the source file. Only with a single file in the configuration. Deleted strings can be restored from the project's Activity tab. |
| `--dry-run` | List the files that would be uploaded. |

For a String Catalog, `push` sends only the source language, unless `--translations` is given.

### `stringalize pull`

Downloads every locale into its file and creates the folders it needs.

The source-language files are not touched: they belong to the repository. A String Catalog is the exception, since it holds every language in one file.

| Flag | |
|---|---|
| `--source` | Also rewrite the source-language files. |
| `--status translated` | Export only strings with this status. Overrides `export.status` in the configuration. |
| `--dry-run` | List the files that would change. |

iOS `.strings` and Android XML leave untranslated strings out, so the app falls back to its base language. For JSON, ARB and other formats, set `fallback` under `export` if empty strings would show.

### `stringalize status`

```
My App

LOCALE        TRANSLATED   MISSING   FLAGGED   PROBLEMS
en (source)   100%         0         0         0
de            92%          14        2         1
it            100%         0         0         0
```

`PROBLEMS` counts translations that fail validation, such as a missing placeholder or plural form. With `--strict` the command exits with status 1 when any locale has missing, flagged or invalid strings, which can block a release in CI.

### Common flags

Every command accepts `--config` (default `stringalize.yml`), `--key` (default `$STRINGALIZE_KEY`) and `--server` (default `$STRINGALIZE_SERVER`, then the configuration, then `https://stringalize.app`).

Exit status is 0 on success, 1 on failure, 2 for a mistake in the command line.

## In CI

GitHub Actions, uploading new strings on every push to `main`:

```yaml
- name: Push strings to Stringalize
  run: |
    curl -fsSL https://github.com/cbarbera80/stringalize-cli/releases/latest/download/stringalize_linux_amd64.tar.gz | tar xz stringalize
    ./stringalize push
  env:
    STRINGALIZE_KEY: ${{ secrets.STRINGALIZE_KEY }}
```

Downloading translations before a release build, and failing if any are missing:

```bash
stringalize pull
stringalize status --strict
```

## FAQ

**Does `pull` overwrite my source files?**
No. Only with `--source`, and never for anything but the source-language files you list.

**What happens to new languages?**
`pull` writes a file for every locale in the project, creating `values-pt-rBR/` or `pt-BR.lproj/` as needed. In Xcode, check the new language appears in the project's localizations.

**What does the CLI send?**
Your translation files, to the Stringalize server in the configuration, authenticated with your API key. Nothing else.

## License

MIT. See [LICENSE](LICENSE).
