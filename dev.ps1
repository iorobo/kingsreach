# Kingsreach local dev: starts the portable PostgreSQL (if needed), builds the
# Babylon.js client when the bundle is missing, then runs the game server on
# http://localhost:8080. Ctrl+C stops the server; stop the database with:
#   .local\pgsql\bin\pg_ctl.exe -D .local\pgdata stop
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path

& "$root\.local\pgsql\bin\pg_ctl.exe" -D "$root\.local\pgdata" status 2>$null | Out-Null
if ($LASTEXITCODE -ne 0) {
  & "$root\.local\pgsql\bin\pg_ctl.exe" -D "$root\.local\pgdata" -l "$root\.local\pg.log" -o "-p 5433" -w start
}

if (-not (Test-Path "$root\web\kingsreach.js")) {
  Write-Host "Building the client bundle..."
  Push-Location "$root\client"
  if (-not (Test-Path "node_modules")) { npm install --no-audit --no-fund }
  npm run build
  Pop-Location
}

$env:DATABASE_URL = "postgres://kingsreach:kingsreach@localhost:5433/kingsreach?sslmode=disable"
$env:STATIC_DIR = "$root\web"
# Dev convenience: every skin and environment available without grinding wins.
# Unset (or set to 0) to play with the real unlock requirements.
$env:KINGSREACH_UNLOCK_ALL = "1"
# Steam sends players back here after signing in. Steam itself must be able to
# reach this address, so a real sign-in only completes from a public host —
# locally the button is there but the round trip will not finish.
$env:PUBLIC_URL = "http://localhost:8080"
# A short move clock makes the countdown easy to see while working on it;
# 150 seconds is the real default. Set to 0 to switch the clock off entirely.
$env:KINGSREACH_MOVE_SECONDS = "150"
Set-Location "$root\server"
go run ./cmd/server
