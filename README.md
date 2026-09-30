# TiNG

IRC platform (Go). Spec: [`docs/00_SPEC.md`](docs/00_SPEC.md).

Develop on Windows 10, package a zip locally. No commit/push required to package.

## Install `just`

- **Windows:** `winget install Casey.Just` / `scoop install just` / `cargo install just`
- **Linux:** see [just install](https://github.com/casey/just#installation) (`cargo install just`, packages, or the install script)

Also needed:

- **Linux:** `zip`, `tar`
- **Windows:** PowerShell 5+ (built-in)

## Recipes

```text
just run       # build connector + start master (needs config.toml)
just stop      # kill leftover connector/master from `just run`
just build     # stage working tree + linux amd64 binaries → dist/stage
just package   # zip → dist/ting.zip (runs build)
just clean     # wipe dist/
```

On a Linux box: unzip `ting.zip` into the deploy path, copy `config.example.toml` to `config.toml`, edit identity and `control.token`, then run `./ting-master`. Master starts `ting-connector` children for each enabled server in `ting.db`. Add servers in the admin UI.

## Run locally

```text
copy config.example.toml config.toml   # then edit token and identity
just run
```

Opens admin UI at `http://127.0.0.1:8080` (token from `control.token`). Ctrl+C stops master only; connectors stay on IRC. `just stop` kills leftovers. Cycle in the admin UI restarts a connector on purpose.

Channel URLs are resolved on master (Twitter/X, Bluesky, YouTube, Reddit, generic titles). Enable/disable per server on the server tab. The connector only keeps the IRC socket and forwards events. Channel karma is t3b-style (`phrase++` / `--` / `+d` / `+N..M`, public `.karma`). Public `.link` / `.l` searches the log (`.more` / `.m` pages). Import old t3b `links-*.log` and `karma-*.db` files on the **import** tab (drop onto the selected server). Browse links on the **links** tab.

OpenRouter chat is on master. Set `[ai] api_key` in `config.toml` (empty key = off). The **ai** tab holds the system prompt, model list, sampling, and an optional channel-memory window. The bot replies when its nick is mentioned in a channel, or when someone queries it.

Debug a connector alone: `ting-connector -listen 127.0.0.1:7391 -token x -spec spec.json`.

## Notes

- Packaging copies the **working tree** (excludes `.git`, `dist/`, `.env`, `.cursor`). Uncommitted local files are included.
- Justfile uses `[unix]` (bash) and `[windows]` (PowerShell) recipe variants; same recipe names on both.
- Linux stages with `tar`, zips with `zip`. Windows copies with PowerShell, zips with `Compress-Archive`.
- `just build` copies the working tree, then `GOOS=linux GOARCH=amd64 go build` into `dist/stage/ting-connector` and `dist/stage/ting-master` (prod is Linux). Local Windows runs use `just run`.
