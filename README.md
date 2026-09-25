# tng

IRC platform (Go). Spec: [`docs/00_SPEC.md`](docs/00_SPEC.md).

Develop on Windows 10, package a zip locally, ship to Linux prod. No commit/push required to package.

## Install `just`

- **Windows:** `winget install Casey.Just` / `scoop install just` / `cargo install just`
- **Linux:** see [just install](https://github.com/casey/just#installation) (`cargo install just`, packages, or the install script)

Also needed: **Python 3** (stage/zip), and **OpenSSH `scp`** for `ship` (Windows: Optional Features → OpenSSH Client).

## Recipes

```text
just build     # stage working tree → dist/stage
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

On the Linux box: unzip `tng.zip` into the deploy path and run from there (app entrypoints land later).

## Notes

- Packaging copies the **working tree** (excludes `.git`, `dist/`, `.env`, caches). Uncommitted local files are included.
- Windows and Linux share the same recipes; packaging uses Python’s zip, not shell-specific tools.
- When Go binaries exist, extend the `build` recipe to compile into `dist/stage` before packaging.
