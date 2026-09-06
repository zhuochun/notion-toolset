# Configuration reference

The YAML structure and tags are defined in [internal/config/config.go](../internal/config/config.go).
Commands validate settings and apply defaults; decoding itself does neither. Only
the selected command's section is validated. Runtime uses permissive `yaml.Unmarshal`: unknown
fields are ignored, omitted/null scalar values remain zero, and malformed/type-
incompatible YAML errors are returned. Canonical example tests use stricter schema
checks to catch typos; they do not change runtime decoding.

Start with [example/configs](../example/configs/). Single mode takes one mapping.
For multi mode, make a YAML list containing those same mappings and invoke
`--multi --config=...`; `--idx` and `--repeat` select/repeat whole mappings.
Paths are relative to the current working directory. Loading `--one` with no
config returns a zero config; export applies useful direct-page defaults, while
LLM still needs a prompt.

## Environment

| Variable | Consumer and timing |
| --- | --- |
| `NOTION_TOKEN` | Required by app before reading config or executing any command |
| `DOT_OPENAI_KEY` | Required by LLM validation after its prompt check |
| `DOT_OPENAI_URL` | Optional OpenAI-compatible base URL, applied when constructing the LLM client; absent uses the SDK default endpoint |

Set these variables in the process environment; the CLI does not load dotenv
files. Avoid sharing debug output containing private configuration.

## Journals

Both `dailyJournal` and `weeklyJournal` accept these fields. See
[daily example](../example/configs/journal-daily.yaml) and
[weekly example](../example/configs/journal-weekly.yaml).

| Field | Meaning/default |
| --- | --- |
| `databaseID` | Target database; empty is passed through and normally fails on API access |
| `limit` | Future dates/weeks considered; default 0 creates nothing, but the existing-page query still runs |
| `pageQuery` | Optional JSON query template using `.Date` for today's local date; empty means unfiltered query |
| `pageProperties` | JSON database properties template used for each creation; must be valid when a page is created |

There is no blanket required-field check in journal Validate. Page template fields
are `.Title`, `.Date`, `.DateEnd` (weekly only), and `.DatabaseID`. Daily title/date
is `YYYY-MM-DD`; weekly title is `YYYY-MM-DD/YYYY-MM-DD`, Monday through Sunday.

## Flashback

| Field | Meaning/default |
| --- | --- |
| `databaseID`, `databaseQuery` | Source database and optional query template; `.Date` is chosen lookback date, `.Today` is local today |
| `oldestTimestamp` | RFC3339 timestamp; should be at least one hour in the past for the random-hour range |
| `flashbackNum` | At selection time values below 1 become 1, values above results become result count |
| `flashbackPageID` | Destination page; required unless a journal database is supplied |
| `flashbackJournalID` | Optional daily journal database; today's first matching page overrides `flashbackPageID` |
| `flashbackTextBlock` | Required paragraph-object JSON template using `.PageID` and `.Date` |
| `flashbackChainFile` | Optional output filename; writes selected page IDs, one per LF-terminated line, in unspecified order |

## Duplicate detection

Section: `duplicateChecker`.

| Field | Meaning/default |
| --- | --- |
| `databaseID`, `databaseQuery` | Source database and optional query template (empty builder fields) |
| `checkProperties` | List of property names; OR across nonempty title/rich-text/URL values. Empty list uses page title |
| `brokenURLproperty` | Exact spelling/case; optional URL property to check with HTTP |
| `duplicateDumpID` | Report destination block/page |
| `duplicateDumpTextBlock` | Required paragraph-object JSON template using `.PageID` and `.Date` |

Reports are deduplicated within the current run, not across past runs.

## Collection

Section: `collector`.

| Field | Meaning/default |
| --- | --- |
| `databaseID`, `databaseQuery` | Source database and optional query template |
| `collectionIDs` | Roots to scan for already-collected page mentions; empty means no existing collected IDs |
| `collectDumpID` | Destination block/page |
| `collectDumpTextBlock` | Required paragraph-object JSON template using `.PageID`; `.Date`/`.Content` remain empty |

## Export and upload

Both use `exporter`. Export-specific scan/cleanup/filename settings are not applied
as upload actions. See the [commands reference](commands.md) for their effects.

| Field | Meaning/default and owner |
| --- | --- |
| `databaseID`, `databaseQuery` | Export source and optional query template using `.Date`; ignored by upload discovery |
| `lookbackDays` | Positive sets export query `.Date` to local today minus days and suppresses cleanup; otherwise `.Date` is empty. The template must actually use it to filter |
| `directory` | Output/input directory; export requires it except `--one`, upload defaults it to working directory; validation creates missing directories |
| `assetDirectory` | Optional local asset directory, created during validation if specified |
| `cleanupDeleted` | Default false; full export only, with boundaries in commands reference |
| `useTitleAsFilename` | Default false; export title slugs instead of page IDs |
| `replaceTitle` | Exactly two strings cause a literal replacement before slugging; other lengths do nothing |
| `markdown` | Rendering settings below; shared by export/upload |
| `exportSpeed` | Validation maps values below 1 to 2.8 and above 3 to 3. Reader rate/burst and export/download worker counts derive from this value |
| `debugLimit` | Positive caps export database scan results; 0 means unlimited; independent of `--debug` |
| `debugCache` | Default false; export writes diagnostic JSON under `temp/` if available; errors are logged |

`exportSpeed` is intended to be finite. The internal render-session constructor
requires a normalized value and rejects NaN before workers start. It does not
normalize configuration itself.

`exporter.markdown` uses the public
[transformer.MarkdownConfig](../transformer/markdown.go):

| Field | Default / effect |
| --- | --- |
| `noAlias` | false; emit the compact page ID as `aliases` unless disabled |
| `indexAliasPath` | empty; otherwise scan Markdown files recursively for an `aliases: ` line among the first three lines to resolve links |
| `noFrontMatters` | false; suppress property frontmatter when true; alias handling remains separate, including existing output quirks |
| `frontMatters` | empty selects eligible properties sorted by name; a list specifies fields/order |
| `noMetadata` | false; suppress Markdown metadata when true |
| `metadata` | empty selects remaining supported metadata properties sorted by name; a list specifies fields/order |
| `titleToH1` | false; render title as H1 and omit title property from frontmatter; upload interprets a leading H1 as a title update |
| `selectToTags` | false; render select/multi-select values as metadata tags when enabled |
| `plainText` | false; suppress links/images/styles in supported content paths |

Only export's direct-page path replaces an entirely zero Markdown config with
`noAlias`, `noFrontMatters`, `noMetadata`, `titleToH1`, and `plainText` all true.
Individual options do not acquire defaults when any Markdown setting is nonzero.

## LLM

Section: `llm`. Input precedence is `--one`, then `chainFile`, then database query.

| Field | Meaning/default |
| --- | --- |
| `databaseID`, `databaseQuery` | Source and optional template using `.Date`/`.Today` |
| `lookbackDays` | Positive sets `.Date` to today minus days; otherwise empty. `.Today` always has today's local date |
| `chainFile` | Optional newline-separated IDs; CRLF normalized, empty lines ignored, hyphens removed from IDs |
| `groupExec` | false; true combines eligible page contents into one completion |
| `groupJournalID` | Optional group-output journal database; configured missing target is an error |
| `prompt` | Required system message, checked before LLM credentials |
| `model` | Empty uses SDK `gpt-3.5-turbo` when composing the request |
| `temperature` | Optional pointer; omitted/null differs from explicit zero in config. Value is passed to SDK request serialization |
| `respJSON` | false; requests JSON object response mode when true; prompt must instruct the model appropriately |
| `respTextBlock` | Optional paragraph template for text; required when JSON output is written, where it is an array of paragraph objects using response JSON fields |
| `taskSpeed` | LLM validation: below 1 becomes 2.8, above 3 becomes 3; controls Notion reads and per-page worker count |
| `pageMinChars` | Default 0; skip rendered contents shorter than this byte length |
| `pageMaxChars` | Default 0 means unlimited; positive skips longer contents by byte length |

The “Chars” fields use Go string length (UTF-8 bytes), not Unicode character count.
They apply separately to each rendered page before grouping. Workflow Markdown
for LLM is fixed plain text/H1 with aliases, metadata and frontmatter disabled.

## Templates and failure timing

Templates are parsed with Go `html/template`, then decoded as JSON into SDK
structures by [internal/notionops](../internal/notionops/). Existing HTML escaping
is intentional compatibility behavior: quotes/ampersands can become entities.
This is not a JSON-specific escaping system. Follow the canonical examples,
including `T00:00:00Z` where date-query SDK fields need RFC3339 values.

Query builders expose `.Date`, `.Today`, `.Title`; fields not supplied by a
workflow are empty. Journal property builders expose `.Title`, `.Date`,
`.DateEnd`, `.DatabaseID`. Paragraph builders expose `.Date`, `.Content`,
`.PageID`. LLM text templates receive escaped `.Content` and today's `.Date`;
LLM JSON templates receive the decoded response map instead.

Template errors occur when the selected path executes, not during general YAML
loading. Export, LLM, flashback, duplicate and collector retain query panic paths;
journals return query errors. Required template checks do not validate every
possible expansion in advance. Consult each command's documented continuation
policy before interpreting a successful exit as complete remote success.
