# tng — build → package (zip) → ship to Linux prod
# Works on Windows 10 and Linux. Packages the working tree (no commit/push).

set dotenv-load := true
set windows-shell := ["powershell.exe", "-NoLogo", "-Command"]

name      := "tng"
dist_dir  := "dist"
stage_dir := dist_dir / "stage"
artifact  := dist_dir / (name + ".zip")

# Prefer `python` on Windows, `python3` elsewhere.
python := if os() == "windows" { "python" } else { "python3" }

# Prod target — set via env, `.env` (gitignored), or recipe args.
prod_host := env_var_or_default("TNG_PROD_HOST", "")
prod_path := env_var_or_default("TNG_PROD_PATH", "")
prod_user := env_var_or_default("TNG_PROD_USER", "")

default:
    @just --list

# Stage the working tree into dist/stage (source + docs; no git required).
build:
    {{ python }} tools/pack.py stage --out {{ stage_dir }}

# Zip dist/stage → dist/tng.zip (from local files, not a remote push).
package: build
    {{ python }} tools/pack.py zip --src {{ stage_dir }} --out {{ artifact }}

# scp dist/tng.zip to Linux prod. Needs TNG_PROD_HOST + TNG_PROD_PATH.
ship host=prod_host path=prod_path user=prod_user: package
    {{ python }} tools/pack.py ship --artifact {{ artifact }} --host "{{ host }}" --path "{{ path }}" --user "{{ user }}"

# Remove build artifacts.
clean:
    {{ python }} tools/pack.py clean --dist {{ dist_dir }}
