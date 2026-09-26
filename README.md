# tng

IRC platform (Go). Spec: [`docs/00_SPEC.md`](docs/00_SPEC.md).

Develop on Windows 10, package a zip locally, ship to Linux prod. No commit/push required to package.

## Install `just`

- **Windows:** `winget install Casey.Just` / `scoop install just` / `cargo install just`
- **Linux:** see [just install](https://github.com/casey/just#installation) (`cargo install just`, packages, or the install script)

Also needed:

- **Linux:** `zip`, `tar`, OpenSSH `scp`
- **Windows:** PowerShell 5+ (built-in), OpenSSH Client optional feature (`scp`)

## Recipes

```text
just run       # build connector + start master (needs config.toml)
just stop      # kill leftover connector/master from `just run`
just build     # stage working tree + linux amd64 binaries → dist/stage
just package   # zip → dist/tng.zip (runs build)
just ship      # scp zip to prod (runs package)
just clean     # wipe dist/
```

### Ship to prod

Set host/path (and optional user). Do not commit secrets.

```text
TNG_PROD_HOST=prod.example.com
TNG_PROD_PATH=/opt/tng
TNG_PROD_USER=deploy          # optional
```

Env vars, a gitignored `.env` in the repo root, or:

```text
just ship host=prod.example.com path=/opt/tng user=deploy
```

On the Linux box: unzip `tng.zip` into the deploy path, copy `config.example.toml` to `config.toml`, edit identity and `control.token`, then run `./tng-master`. Master starts `tng-connector` children for each enabled server in `tng.db`. Add servers in the admin UI.

## Run locally

```text
copy config.example.toml config.toml   # then edit token and identity
just run
```

Opens admin UI at `http://127.0.0.1:8080` (token from `control.token`). Ctrl+C stops master and its connector children.

Debug a connector alone: `tng-connector -listen 127.0.0.1:7391 -token x -spec spec.json`.

## Notes

- Packaging copies the **working tree** (excludes `.git`, `dist/`, `.env`, `.cursor`). Uncommitted local files are included.
- Justfile uses `[unix]` (bash) and `[windows]` (PowerShell) recipe variants; same recipe names on both.
- Linux stages with `tar`, zips with `zip`. Windows copies with PowerShell, zips with `Compress-Archive`.
- `just build` copies the working tree, then `GOOS=linux GOARCH=amd64 go build` into `dist/stage/tng-connector` and `dist/stage/tng-master` (prod is Linux). Local Windows runs use `just run`.
