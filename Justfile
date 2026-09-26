# tng — build → package (zip) → ship to Linux prod
# Windows 10 (PowerShell) + Linux (bash). Packages the working tree (no commit/push).

set dotenv-load := true
set windows-shell := ["powershell.exe", "-NoLogo", "-Command"]
set shell := ["bash", "-cu"]

name      := "tng"
dist_dir  := "dist"
stage_dir := dist_dir / "stage"
artifact  := dist_dir / (name + ".zip")

# Prod target — set via env, `.env` (gitignored), or recipe args.
prod_host := env_var_or_default("TNG_PROD_HOST", "")
prod_path := env_var_or_default("TNG_PROD_PATH", "")
prod_user := env_var_or_default("TNG_PROD_USER", "")

default:
    @just --list

# ── build: stage working tree → dist/stage ─────────────────────────────

[unix]
build:
    #!/usr/bin/env bash
    set -euo pipefail
    rm -rf "{{ stage_dir }}"
    mkdir -p "{{ stage_dir }}"
    tar -cf - \
      --exclude='./.git' \
      --exclude='./dist' \
      --exclude='./.env' \
      --exclude='./.cursor' \
      . | tar -xf - -C "{{ stage_dir }}"
    echo "staged -> {{ stage_dir }}"
    GOOS=linux GOARCH=amd64 go build -o "{{ stage_dir }}/tng-connector" ./cmd/tng-connector
    echo "linux amd64 binary -> {{ stage_dir }}/tng-connector"
    GOOS=linux GOARCH=amd64 go build -o "{{ stage_dir }}/tng-master" ./cmd/tng-master
    echo "linux amd64 binary -> {{ stage_dir }}/tng-master"

[windows]
build:
    #!powershell.exe
    $ErrorActionPreference = "Stop"
    if (Test-Path "{{ stage_dir }}") { Remove-Item -Recurse -Force "{{ stage_dir }}" }
    New-Item -ItemType Directory -Path "{{ stage_dir }}" -Force | Out-Null
    $skip = @('.git', 'dist', '.env', '.cursor')
    Get-ChildItem -Force | Where-Object { $skip -notcontains $_.Name } | ForEach-Object {
      Copy-Item -Recurse -Force $_.FullName -Destination (Join-Path "{{ stage_dir }}" $_.Name)
    }
    Write-Host "staged -> {{ stage_dir }}"
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    go build -o (Join-Path "{{ stage_dir }}" "tng-connector") ./cmd/tng-connector
    Write-Host "linux amd64 binary -> {{ stage_dir }}/tng-connector"
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    go build -o (Join-Path "{{ stage_dir }}" "tng-master") ./cmd/tng-master
    Write-Host "linux amd64 binary -> {{ stage_dir }}/tng-master"

# ── package: zip dist/stage → dist/tng.zip ─────────────────────────────

[unix]
package: build
    #!/usr/bin/env bash
    set -euo pipefail
    rm -f "{{ artifact }}"
    (cd "{{ stage_dir }}" && zip -r -q "../{{ name }}.zip" .)
    echo "wrote {{ artifact }} ($(wc -c < "{{ artifact }}") bytes)"

[windows]
package: build
    #!powershell.exe
    $ErrorActionPreference = "Stop"
    if (Test-Path "{{ artifact }}") { Remove-Item -Force "{{ artifact }}" }
    Compress-Archive -Path (Join-Path "{{ stage_dir }}" '*') -DestinationPath "{{ artifact }}"
    Write-Host "wrote {{ artifact }}"

# ── ship: scp zip to Linux prod ────────────────────────────────────────

[unix]
ship host=prod_host path=prod_path user=prod_user: package
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ -z "{{ host }}" || -z "{{ path }}" ]]; then
      echo "error: set TNG_PROD_HOST and TNG_PROD_PATH (or pass host= path=)" >&2
      exit 1
    fi
    target="{{ host }}"
    if [[ -n "{{ user }}" ]]; then target="{{ user }}@{{ host }}"; fi
    dest="${target}:{{ path }}/"
    echo "scp {{ artifact }} ${dest}"
    scp "{{ artifact }}" "${dest}"

[windows]
ship host=prod_host path=prod_path user=prod_user: package
    #!powershell.exe
    $ErrorActionPreference = "Stop"
    if (-not "{{ host }}" -or -not "{{ path }}") {
      Write-Error "set TNG_PROD_HOST and TNG_PROD_PATH (or pass host= path=)"
      exit 1
    }
    $target = if ("{{ user }}") { "{{ user }}@{{ host }}" } else { "{{ host }}" }
    $dest = "${target}:{{ path }}/"
    Write-Host "scp {{ artifact }} $dest"
    scp "{{ artifact }}" $dest

# ── run: gateway + master from repo root (dev) ─────────────────────────

[unix]
run:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ ! -f config.toml ]]; then
      echo "error: copy config.example.toml to config.toml" >&2
      exit 1
    fi
    go run ./cmd/tng-connector -config config.toml &
    gw=$!
    trap 'kill $gw 2>/dev/null || true' EXIT INT TERM
    go run ./cmd/tng-master -config config.toml

[windows]
run:
    #!powershell.exe
    $ErrorActionPreference = "Stop"
    if (-not (Test-Path "config.toml")) {
      Write-Error "copy config.example.toml to config.toml"
      exit 1
    }
    $gw = Start-Process -FilePath "go" -ArgumentList @("run","./cmd/tng-connector","-config","config.toml") -NoNewWindow -PassThru
    try {
      go run ./cmd/tng-master -config config.toml
    } finally {
      if ($gw -and -not $gw.HasExited) {
        Stop-Process -Id $gw.Id -Force -ErrorAction SilentlyContinue
      }
    }

# ── stop: leftover go run / binaries from `just run` ───────────────────

[unix]
stop:
    #!/usr/bin/env bash
    set -euo pipefail
    pkill -f 'go run ./cmd/tng-connector' 2>/dev/null || true
    pkill -f 'go run ./cmd/tng-master' 2>/dev/null || true
    pkill -x tng-connector 2>/dev/null || true
    pkill -x tng-master 2>/dev/null || true

[windows]
stop:
    #!powershell.exe
    $ErrorActionPreference = "SilentlyContinue"
    Get-CimInstance Win32_Process | Where-Object {
      $_.Name -match 'tng-connector|tng-master' -or
      $_.CommandLine -match 'cmd/tng-connector|cmd/tng-master'
    } | ForEach-Object {
      Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
    }

# ── clean ──────────────────────────────────────────────────────────────

[unix]
clean:
    #!/usr/bin/env bash
    set -euo pipefail
    rm -rf "{{ dist_dir }}"
    mkdir -p "{{ dist_dir }}"
    touch "{{ dist_dir }}/.gitkeep"
    echo "cleaned {{ dist_dir }}"

[windows]
clean:
    #!powershell.exe
    $ErrorActionPreference = "Stop"
    if (Test-Path "{{ dist_dir }}") { Remove-Item -Recurse -Force "{{ dist_dir }}" }
    New-Item -ItemType Directory -Path "{{ dist_dir }}" -Force | Out-Null
    New-Item -ItemType File -Path (Join-Path "{{ dist_dir }}" ".gitkeep") -Force | Out-Null
    Write-Host "cleaned {{ dist_dir }}"
