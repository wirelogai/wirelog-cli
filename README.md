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

# Discover events
wl inspect
wl inspect signup
```

## Commands

| Command | Description | Key Type |
|---|---|---|
| `wl query <dsl>` | Run analytics queries | `sk_` / `aat_` (query) |
| `wl track <event>` | Send tracking events | `pk_` / `sk_` / `aat_` (track) |
| `wl identify` | Set user profile properties | `pk_` / `sk_` / `aat_` (track) |
| `wl inspect [event]` | Discover events and properties | `sk_` / `aat_` (query) |
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
```

Read queries from stdin:

```bash
echo "* | last 7d | count by event_type" | wl query -
```

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
