# ting — build → package (zip)
# Windows 10 (PowerShell) + Linux (bash). Packages the working tree (no commit/push).

set windows-shell := ["powershell.exe", "-NoLogo", "-Command"]
set shell := ["bash", "-cu"]

name      := "ting"
dist_dir  := "dist"
stage_dir := dist_dir / "stage"
artifact  := dist_dir / (name + ".zip")

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
    GOOS=linux GOARCH=amd64 go build -o "{{ stage_dir }}/ting-connector" ./cmd/ting-connector
    echo "linux amd64 binary -> {{ stage_dir }}/ting-connector"
    GOOS=linux GOARCH=amd64 go build -o "{{ stage_dir }}/ting-master" ./cmd/ting-master
    echo "linux amd64 binary -> {{ stage_dir }}/ting-master"

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
    go build -o (Join-Path "{{ stage_dir }}" "ting-connector") ./cmd/ting-connector
    Write-Host "linux amd64 binary -> {{ stage_dir }}/ting-connector"
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    go build -o (Join-Path "{{ stage_dir }}" "ting-master") ./cmd/ting-master
    Write-Host "linux amd64 binary -> {{ stage_dir }}/ting-master"

# ── package: zip dist/stage → dist/ting.zip ─────────────────────────────

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

# ── run: gateway + master from repo root (dev) ─────────────────────────

[unix]
run:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ ! -f config.toml ]]; then
      echo "error: copy config.example.toml to config.toml" >&2
      exit 1
    fi
    mkdir -p dist/dev
    go build -o dist/dev/ting-connector ./cmd/ting-connector
    go run ./cmd/ting-master -config config.toml -connector dist/dev/ting-connector

[windows]
run:
    #!powershell.exe
    $ErrorActionPreference = "Stop"
    if (-not (Test-Path "config.toml")) {
      Write-Error "copy config.example.toml to config.toml"
      exit 1
    }
    New-Item -ItemType Directory -Path "dist/dev" -Force | Out-Null
    go build -o (Join-Path "dist/dev" "ting-connector.exe") ./cmd/ting-connector
    go run ./cmd/ting-master -config config.toml -connector (Join-Path "dist/dev" "ting-connector.exe")

# ── stop: leftover go run / binaries from `just run` ───────────────────

[unix]
stop:
    #!/usr/bin/env bash
    set -euo pipefail
    pkill -f 'go run ./cmd/ting-connector' 2>/dev/null || true
    pkill -f 'go run ./cmd/ting-master' 2>/dev/null || true
    pkill -x ting-connector 2>/dev/null || true
    pkill -x ting-master 2>/dev/null || true

[windows]
stop:
    #!powershell.exe
    $ErrorActionPreference = "SilentlyContinue"
    Get-CimInstance Win32_Process | Where-Object {
      $_.Name -match 'ting-connector|ting-master' -or
      $_.CommandLine -match 'cmd/ting-connector|cmd/ting-master'
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
