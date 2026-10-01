<#
.SYNOPSIS
  Compila e roda o simulacra.

.EXAMPLE
  .\run.ps1              # compila tudo e abre http://127.0.0.1:7777
  .\run.ps1 -Dev         # backend + Vite com hot reload em http://localhost:5173
  .\run.ps1 -Test        # testes do Go (completos) e checagem de tipos do front
  .\run.ps1 -Port 8080 -NoBrowser
#>
param(
    [switch]$Dev,
    [switch]$Test,
    [switch]$NoBrowser,
    [int]$Port = 7777
)

$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
Set-Location $root

function Step($msg) { Write-Host "`n==> $msg" -ForegroundColor Green }
function Need($cmd, $hint) {
    if (-not (Get-Command $cmd -ErrorAction SilentlyContinue)) {
        Write-Host "Faltando: $cmd. $hint" -ForegroundColor Red
        exit 1
    }
}

Need go   'Instale Go 1.26+ em https://go.dev/dl/'
Need node 'Instale Node 22+ em https://nodejs.org/'
Need npm  'O npm vem junto com o Node.'

Push-Location web
try {
    if (-not (Test-Path node_modules)) {
        Step 'Instalando dependências do frontend'
        npm install --no-audit --no-fund
        if ($LASTEXITCODE) { throw 'npm install falhou' }
    }
} finally { Pop-Location }

if ($Test) {
    Step 'Testes do Go'
    go vet ./...
    if ($LASTEXITCODE) { exit $LASTEXITCODE }
    go test ./...
    if ($LASTEXITCODE) { exit $LASTEXITCODE }
    Step 'Checagem de tipos do frontend'
    Push-Location web; npm run typecheck; $code = $LASTEXITCODE; Pop-Location
    exit $code
}

if ($Dev) {
    Step "Backend em http://127.0.0.1:$Port (janela separada)"
    $backend = Start-Process -PassThru -FilePath go -ArgumentList @('run', './cmd/simulacra', 'serve', '-addr', "127.0.0.1:$Port") -WorkingDirectory $root
    try {
        Step 'Frontend com hot reload em http://localhost:5173'
        if (-not $NoBrowser) { Start-Process 'http://localhost:5173' }
        Push-Location web
        npm run dev
    } finally {
        Pop-Location
        # go run spawns a child process: kill the whole tree.
        if ($backend -and -not $backend.HasExited) { taskkill /PID $backend.Id /T /F | Out-Null }
    }
    exit 0
}

Step 'Compilando o frontend'
Push-Location web
try {
    npm run build
    if ($LASTEXITCODE) { throw 'build do frontend falhou' }
} finally { Pop-Location }

Step 'Compilando o binário (frontend embutido)'
New-Item -ItemType Directory -Force bin | Out-Null
go build -trimpath -ldflags '-s -w' -o bin/simulacra.exe ./cmd/simulacra
if ($LASTEXITCODE) { throw 'go build falhou' }

$url = "http://127.0.0.1:$Port"
Step "simulacra rodando em $url  (Ctrl+C para parar)"
if (-not $NoBrowser) {
    Start-Job -ScriptBlock {
        param($u)
        for ($i = 0; $i -lt 40; $i++) {
            try { Invoke-WebRequest "$u/api/health" -UseBasicParsing -TimeoutSec 1 | Out-Null; Start-Process $u; return } catch { Start-Sleep -Milliseconds 250 }
        }
    } -ArgumentList $url | Out-Null
}
& "$root\bin\simulacra.exe" serve -addr "127.0.0.1:$Port" -data "$root\data"
