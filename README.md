# lazydbm

A lightweight TUI for importing and exporting PostgreSQL and MySQL/MariaDB databases.

Run it in a project folder. It discovers **servers** from `.env` / docker compose, lists databases after you connect, lets you pick a dump file, optionally drop+create the selected database, and streams success/fail logs along the bottom.

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

Layout: **servers** | **databases** | **files**, with the job **log** full-width along the bottom.

### Discovery

On start, servers (engine, host, port, user) are collected from:

1. `.env`, `.env.*` — Laravel-style `DB_CONNECTION`, `DB_HOST`, `DB_PORT`, `DB_DATABASE`, `DB_USERNAME`, `DB_PASSWORD` (and `DATABASE_URL`)
2. `docker-compose.yml` / `compose.yaml` — `postgres` / `mysql` / `mariadb` services
3. Saved entries in `~/.config/lazydbm/config.yaml` (no passwords)

A discovered database name is a suggestion: after you connect, that name is pre-selected in the middle pane if it exists on the server.

Dump files: `*.sql` in the current directory and one level of subdirectories. Postgres custom dumps (`*.dump`, `*.backup`) use `pg_restore`.

If `.env` points at a compose service name (`DB_HOST=postgres`), lazydbm rewrites it to `127.0.0.1` plus the published host port.

### Keys

| Key | Action |
| --- | --- |
| `tab` / `h` `l` | Servers → databases → files (`tab` also reaches the log) |
| `j` `k` / arrows | Move in the focused pane |
| `enter` | On a server: connect, test, list databases. On a file: import |
| `i` | Import selected file into selected database (confirm + optional clear). Creates the database if it is missing. Postgres skips owners and grants (same as `pg_restore --no-owner --no-acl`). |
| `e` | Export selected database |
| `n` | Create a database on the connected server (or select it if it already exists) |
| `a` | Add a saved server (engine, host, port, user, password — no database field) |
| `E` | Edit a saved server (or clone a discovered one) |
| `d` | Delete a saved server |
| `c` | Toggle clear-db (drop + create before import) |
| `p` | Set / save password for the server |
| `r` | Re-list databases on the current server and refresh files |
| `?` | Help |
| `q` / `ctrl+c` | Quit (cancels a running job first) |

Destructive steps always ask for confirmation. One job at a time; the UI stays interactive while the log tails. Failed connects show in the log and as a notice; the databases pane shows the error until listing works.

### Passwords

If a password is missing (or auth failed), lazydbm prompts for it. Press enter with an empty field for trust/peer auth (no password). You can remember that as `no_password` in config, or save a real password to the OS keychain (macOS Keychain / libsecret). The key is `engine|host|port|user` (server identity, not a database name). Config never stores password values.

Import creates the target database if it does not already exist. You do not need to `CREATE DATABASE` first. Use `n` if you want to name a database that is not in the list yet, then import into it.

Postgres import skips `OWNER TO` / `GRANT` / `REVOKE` so a dump from another server does not fail on missing roles. Objects belong to the user you connected as. Custom dumps already pass `--no-owner --no-acl` to `pg_restore`.

## Config

`~/.config/lazydbm/config.yaml` stores server metadata, last-used server, last-used database per server, and an optional `no_password` flag for trust auth. Add or edit saved servers from the TUI with `a` / `E`; passwords still go in the OS keychain, never this file.

## Test

Go 1.24+ on PATH is enough. Nothing else to install for unit tests (they fake `psql`/`mysql` and the OS keychain).

```bash
./scripts/test.sh
```

Or:

```bash
go test ./... -count=1 -cover
```

Live import/export still needs the official DB clients on the machine (`psql`, `pg_dump`, `mysql`, `mysqldump`). Those are not required to run the test script.

## Releases

Push a tag to publish binaries with GoReleaser:

```bash
git tag v0.1.0
git push origin v0.1.0
```

## Out of v1

SQLite, Homebrew tap, SSH tunnels, concurrent jobs, gzip-on-the-fly, GUI file pickers.
