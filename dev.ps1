# Selket dev runner for Windows PowerShell.
#
# Equivalent of `make dev`: starts postgres via docker-compose, generates
# templ, builds the Tailwind CSS bundle, runs migrations, and launches
# the server. Anchors itself to the script's own directory so it works
# regardless of where PowerShell was started.
#
# Usage:  .\dev.ps1
#
# Prereqs (the script checks for each and points to installers):
#   - Go 1.25.11+        https://go.dev/dl/
#   - Docker Desktop     https://www.docker.com/products/docker-desktop/
#                        (or set $env:DATABASE_URL to a cloud Postgres and
#                        the script will skip the docker step)
#   - Node.js 20+        https://nodejs.org/

$ErrorActionPreference = 'Stop'

# Anchor to the script's directory - this is the only protection against
# accidentally running the script from somewhere that has no go.mod, no
# docker-compose.yml, and no package.json.
Set-Location -Path $PSScriptRoot

function Require-Command {
    param([string]$Name, [string]$InstallUrl)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        Write-Host "  missing: $Name" -ForegroundColor Red
        Write-Host "  install: $InstallUrl" -ForegroundColor Red
        exit 1
    }
}

Write-Host "==> checking prereqs..." -ForegroundColor Cyan
Require-Command go      'https://go.dev/dl/'
Require-Command node    'https://nodejs.org/'
Require-Command npx     'https://nodejs.org/'

# Docker is optional: only skip local postgres if DATABASE_URL points
# at a non-localhost host (Neon, Supabase, RDS, etc.). A localhost
# DATABASE_URL left over from a prior PowerShell session must NOT cause
# us to skip starting the container - that container is what serves the
# URL.
$useDocker = $true
if ($env:DATABASE_URL -and $env:DATABASE_URL -notmatch '(localhost|127\.0\.0\.1|\[::1\])') {
    $useDocker = $false
}
if ($useDocker) {
    Require-Command docker 'https://www.docker.com/products/docker-desktop/'
}

# Make sure templ (Go-installed binary, $(go env GOPATH)\bin) is reachable.
$goBin = Join-Path (go env GOPATH) 'bin'
if (($env:Path -split ';') -notcontains $goBin) {
    $env:Path = "$goBin;$env:Path"
}
if (-not (Get-Command templ -ErrorAction SilentlyContinue)) {
    Write-Host "==> installing templ (one-time)..." -ForegroundColor Cyan
    go install github.com/a-h/templ/cmd/templ@latest
}

if ($useDocker) {
    Write-Host "==> starting postgres..." -ForegroundColor Cyan
    docker compose up -d postgres | Out-Null

    Write-Host "==> waiting for postgres..." -ForegroundColor Cyan
    $deadline = (Get-Date).AddSeconds(30)
    while ((Get-Date) -lt $deadline) {
        docker compose exec -T postgres pg_isready -U selket *> $null
        if ($LASTEXITCODE -eq 0) { break }
        Start-Sleep -Seconds 1
    }
    if ($LASTEXITCODE -ne 0) {
        Write-Host "  postgres did not come up in 30s. Check 'docker compose logs postgres'." -ForegroundColor Red
        exit 1
    }
    # Only set DATABASE_URL if the caller didn't pre-set it (a leftover
    # localhost URL from a prior session is fine - it points at the
    # container we just started).
    if (-not $env:DATABASE_URL) {
        $env:DATABASE_URL = "postgres://selket:selket@localhost:5432/selket?sslmode=disable"
    }
} else {
    Write-Host "==> DATABASE_URL points to a non-localhost host - skipping local postgres" -ForegroundColor Cyan
}

Write-Host "==> evidence dir + htmx..." -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path tmp\evidence | Out-Null
if (-not (Test-Path assets\static\htmx.min.js)) {
    Invoke-WebRequest -UseBasicParsing `
        -Uri 'https://unpkg.com/htmx.org@2.0.3/dist/htmx.min.js' `
        -OutFile assets\static\htmx.min.js
}

if (-not (Test-Path node_modules)) {
    Write-Host "==> npm install (one-time)..." -ForegroundColor Cyan
    npm install
}

Write-Host "==> templ generate..." -ForegroundColor Cyan
templ generate

Write-Host "==> tailwindcss build..." -ForegroundColor Cyan
npx --yes tailwindcss -i assets/css/input.css -o assets/static/app.css --minify

Write-Host "==> migrations..." -ForegroundColor Cyan
go run ./cmd/migrate up

# Default port: 5173. Override by setting $env:ADDR before running.
if (-not $env:ADDR) { $env:ADDR = ':5173' }
$port = $env:ADDR.TrimStart(':')

Write-Host "==> server listening at http://localhost:$port" -ForegroundColor Green
go run ./cmd/server
