# lazydbm

A lightweight TUI for importing and exporting PostgreSQL and MySQL/MariaDB databases.

Run it in a project folder. It discovers connections from `.env` / docker compose, lets you pick a dump file, optionally drop+create the database, and streams success/fail logs in a lazygit-style layout.

Passwords can be saved in the OS keychain so you do not have to type them again. Passwords are never written to config or shown in logs.

## Install

```bash
go install github.com/AmirSyafiq2112/lazydbm@latest
```

Or download a binary from [GitHub Releases](https://github.com/AmirSyafiq2112/lazydbm/releases) (darwin/linux, amd64/arm64).

## Requirements

lazydbm shells out to the official clients already on your machine. It does not vendor them.

| Engine | Import | Export | Reset |
| --- | --- | --- | --- |
| PostgreSQL | `psql`, `pg_restore` | `pg_dump` | `psql` |
| MySQL / MariaDB | `mysql` | `mysqldump` | `mysql` |

If a tool is missing, the log pane says so.

## Usage

```bash
cd /path/to/your/app
lazydbm
```

### Discovery

On start, connections are collected from:

1. `.env`, `.env.*` — Laravel-style `DB_CONNECTION`, `DB_HOST`, `DB_PORT`, `DB_DATABASE`, `DB_USERNAME`, `DB_PASSWORD` (and `DATABASE_URL`)
2. `docker-compose.yml` / `compose.yaml` — `postgres` / `mysql` / `mariadb` services
3. Saved entries in `~/.config/lazydbm/config.yaml` (no passwords)

Dump files: `*.sql` in the current directory and one level of subdirectories. Postgres custom dumps (`*.dump`, `*.backup`) use `pg_restore`.

If `.env` points at a compose service name (`DB_HOST=postgres`), lazydbm rewrites it to `127.0.0.1` plus the published host port.

### Keys

| Key | Action |
| --- | --- |
| `tab` / `h` `l` / arrows | Switch panes |
| `j` `k` / arrows | Move in the focused list |
| `enter` | Select / confirm |
| `i` | Import selected file into selected database |
| `e` | Export selected database |
| `c` | Toggle clear-db (drop + create before import) |
| `p` | Set / save password |
| `r` | Refresh discovery |
| `?` | Help |
| `q` / `ctrl+c` | Quit (cancels a running job first) |

Destructive steps always ask for confirmation. One job at a time; the UI stays interactive while logs tail in the right pane.

### Passwords

If a password is missing (or auth failed), lazydbm prompts for it. You can save it to the OS keychain (macOS Keychain / libsecret). The key is `engine|host|port|user|dbname`. Config never stores passwords.

## Config

`~/.config/lazydbm/config.yaml` stores connection metadata and last-used selection only.

## Releases

Push a tag to publish binaries with GoReleaser:

```bash
git tag v0.1.0
git push origin v0.1.0
```

## Out of v1

SQLite, Homebrew tap, SSH tunnels, concurrent jobs, gzip-on-the-fly, GUI file pickers.
