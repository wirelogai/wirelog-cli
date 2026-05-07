# wl — WireLog CLI

Headless analytics from your terminal. Events in, insights out.

Alternative to PostHog, Amplitude, and Mixpanel — designed for agents instead of dashboards.

## Install

```bash
# macOS / Linux
brew install wirelogai/tap/wl

# Go
go install github.com/wirelogai/wirelog-cli@latest

# Direct download
# https://github.com/wirelogai/wirelog-cli/releases
```

## Quick Start

```bash
# Configure
wl config init

# Send events
wl track page_view --user-id u1 --prop path=/home
wl track signup --user-id u2 --prop-json '{"plan":"pro","seats":5}'

# Query data
wl query "* | last 7d | count by event_type"
wl query "page_view | last 30d | count by day" --format csv
wl query "themeSwitch | latest event_properties.theme | count by last_value | top 10"
wl query "users | count by user.theme"

# Discover events
wl inspect
wl inspect signup

# Build an agent-authored dashboard
wl dashboard init --output dashboard.yaml
wl dashboard validate --file dashboard.yaml
wl dashboard save --file dashboard.yaml --output index.html --mode report
```

## Commands

| Command | Description | Key Type |
|---|---|---|
| `wl query <dsl>` | Run analytics queries | `sk_` / `aat_` (query) |
| `wl track <event>` | Send tracking events | `pk_` / `sk_` / `aat_` (track) |
| `wl identify` | Set user profile properties | `pk_` / `sk_` / `aat_` (track) |
| `wl inspect [event]` | Discover events and properties | `sk_` / `aat_` (query) |
| `wl dashboard init\|schema\|validate\|run\|view\|save` | Create, validate, run, view, and export YAML dashboards | varies |
| `wl project list\|create\|get\|delete\|usage` | Manage projects | `ak_` (admin) |
| `wl gdpr export\|delete` | GDPR data export/deletion | `sk_` / `aat_` (admin) |
| `wl health` | Check API health | none |
| `wl config init\|set\|get\|list` | Manage CLI configuration | — |
| `wl version` | Print version | — |
| `wl completion bash\|zsh\|fish` | Shell completions | — |

## Output Formats

Default: `table` when stdout is a TTY, `json` when piped.

```bash
wl query "* | last 7d | count" --format table     # styled terminal table
wl query "* | last 7d | count" --json              # JSON (agent-friendly)
wl query "* | last 7d | count" --format csv        # CSV
wl query "* | last 7d | count" --format markdown   # Markdown (LLM-friendly)
```

## Configuration

Config file: `~/.config/wirelog/config.json`

Precedence (highest to lowest):
1. `--api-key` / `--host` flags
2. `WIRELOG_API_KEY` / `WIRELOG_HOST` environment variables
3. `.wirelog.json` in current directory (project-local)
4. `~/.config/wirelog/config.json` (global)

## Bulk Loading

Pipe JSONL events from files or other programs:

```bash
cat events.jsonl | wl track --stdin
echo '{"event_type":"click","user_id":"u1"}' | wl track --stdin

# Throttle a large import to be nice to the server (off by default).
# After each batch flush the CLI sleeps long enough to keep the average
# send rate at or below the requested events/second.
cat huge.jsonl | wl track --stdin --batch-size 100 --max-events-per-second 100
```

`wl track` retries automatically on `429` (Too Many Requests, honouring
the `Retry-After` header) and on `5xx` server errors with exponential
backoff (up to 3 attempts).

Read queries from stdin:

```bash
echo "* | last 7d | count by event_type" | wl query -
```

## Dashboards

Dashboards are YAML files that agents can create and validate. They render WireLog queries as local editable dashboards or static HTML.

```bash
wl dashboard init --output dashboard.yaml
wl dashboard init --output -                       # starter YAML to stdout
wl dashboard schema --output -                     # JSON Schema for agents
wl dashboard validate --file dashboard.yaml
wl dashboard validate --file - --json              # validate stdin
wl dashboard run --file dashboard.yaml --json      # run every query and print data
wl dashboard run --file dashboard.yaml --var range=7d --format markdown
wl dashboard view --file dashboard.yaml --open
wl dashboard view --file ./dashboards              # sidebar for every .yaml/.yml file
wl dashboard save --file dashboard.yaml --output index.html --mode report
wl dashboard save --file dashboard.yaml --output - --mode report
```

Export modes:

- `report`: fixed data, preloaded into the HTML, no key embedded.
- `interactive`: embeds an `aat_` token with query scope so date ranges and variables can re-query from the browser.

```bash
export WIRELOG_DASHBOARD_TOKEN=aat_xxx
wl dashboard save --file dashboard.yaml --output index.html --mode interactive --token-env WIRELOG_DASHBOARD_TOKEN
```

Interactive exports written to files use `0600` permissions because the HTML contains the token.

Agent workflow:

- discover events first with `wl query "inspect * | last 30d" --json`
- generate YAML from `wl dashboard schema --output -`
- validate with `wl dashboard validate --file dashboard.yaml`
- run data with `wl dashboard run --file dashboard.yaml --json`

Dashboard variables are shared anchors. Changing one variable, such as `range`, updates every card that references it with `{{range}}` or `{{platform.fragment}}`.
When viewing a dashboard directory, add root-level `order: 10` values to control sidebar order; unordered dashboards sort by filename after ordered dashboards.
Directory dashboards also get stable local routes like `/dashboard/usage.yaml`; extensionless routes like `/dashboard/usage` resolve when unambiguous.
User lookup dashboards can use submitted `type: input` email variables with named fragments like `{{subject.events_fragment}}` and `{{subject.users_fragment}}`; `*@domain.com` becomes a safe domain equality filter.
Chart cards can set `options.x`, `options.y`, and `options.series` when a result has multiple plausible columns. Cards can also set `options.calculate: ratio` to divide the first query by the second query for filtered unit-economics metrics without relying on backend formula support.

## Agent Usage

The CLI is designed for AI agent consumption:

- `--json` on every command produces machine-parseable output
- stdout is strictly for data, stderr for diagnostics
- Exit code 0 on success, 1 on error
- `--quiet` suppresses non-essential stderr output
- `--yes` skips confirmation prompts on destructive operations
- `--dry-run` on track/identify shows the request without sending

```bash
# Agent workflow
wl inspect --json                                    # discover schema
wl query "* | last 7d | count by event_type" --json  # structured results
wl track signup --user-id u1 --prop plan=pro --json   # send events
```

## Links

- [WireLog](https://wirelog.ai) — main product
- [Documentation](https://docs.wirelog.ai) — full docs
- [Go SDK](https://github.com/wirelogai/wirelog-go) — Go client library
- [Python SDK](https://github.com/wirelogai/wirelog-python) — Python client
- [TypeScript SDK](https://github.com/wirelogai/wirelog-typescript) — TypeScript client

## License

MIT
