# github-most-stars

A terminal UI that continuously scans public GitHub repositories for the most-starred and recently-trending projects.

## Features

- Three live panels: all-time most-starred repos, repos created in the last 7 days, and repos created in the last 30 days (each sorted by total stars)
- Background polling that keeps updating results without blocking the UI
- Keyboard navigation to browse and open repos directly in your browser

## Requirements

- Go 1.25 or later

## Installation

```bash
go install github.com/hrmeetsingh/github-most-stars@latest
```

Or build from source:

```bash
git clone https://github.com/hrmeetsingh/github-most-stars.git
cd github-most-stars
go build -o github-most-stars .
```

## Usage

```bash
go run .
```

or, if built:

```bash
./github-most-stars
```

### Keybindings

| Key         | Action                       |
|-------------|-------------------------------|
| `tab`       | Switch to next panel          |
| `shift+tab` | Switch to previous panel      |
| `up`/`down` | Move selection within a panel |
| `enter`     | Open selected repo in browser |
| `x`         | Pause/resume background scan  |
| `q`/`ctrl+c`| Quit                          |

## Notes

- Uses the unauthenticated GitHub REST search API, which is limited to 10 requests/minute. Polling is staggered across the three panels to stay within this limit.
- Each panel keeps up to 25 repos, deduplicated by full name and re-sorted by star count on every poll.
