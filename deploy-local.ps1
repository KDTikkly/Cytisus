<#
.SYNOPSIS
    One-command local deployment for Cytisus v1.

.DESCRIPTION
    Validates Docker Desktop / Docker Compose, protects the local-simulator
    boundary, checks host ports, starts the complete Docker Compose stack,
    waits for service health, and prints local URLs.

    This script is for synthetic local development only. It does not deploy
    Cytisus to production and never deletes PostgreSQL or MinIO volumes.

.EXAMPLE
    powershell -NoProfile -ExecutionPolicy Bypass -File .\deploy-local.ps1

.EXAMPLE
    powershell -NoProfile -ExecutionPolicy Bypass -File .\deploy-local.ps1 -OpenBrowser

.EXAMPLE
    powershell -NoProfile -ExecutionPolicy Bypass -File .\deploy-local.ps1 -PostgresHostPort 55432
#>

[CmdletBinding()]
param(
    [ValidateRange(1024, 65535)]
    [int]$PostgresHostPort = 5432,

    [ValidateRange(30, 1800)]
    [int]$HealthTimeoutSeconds = 300,

    [string]$EnvFile = ".env.example",

    [switch]$SkipBuild,

    [switch]$OpenBrowser,

    [switch]$StopFirst
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

function Write-Step {
    param([string]$Message)
    Write-Host ""
    Write-Host "==> $Message" -ForegroundColor Cyan
}

function Write-Success {
    param([string]$Message)
    Write-Host "[OK] $Message" -ForegroundColor Green
}

function Resolve-RepositoryRoot {
    $candidates = @(
        $PSScriptRoot,
        (Join-Path $PSScriptRoot "..")
    )

    foreach ($candidate in $candidates) {
        $resolved = Resolve-Path -LiteralPath $candidate -ErrorAction SilentlyContinue
        if ($null -eq $resolved) {
            continue
        }

        $composeCandidate = Join-Path $resolved.Path "deploy/docker-compose.yml"
        $envCandidate = Join-Path $resolved.Path ".env.example"

        if ((Test-Path -LiteralPath $composeCandidate -PathType Leaf) -and
            (Test-Path -LiteralPath $envCandidate -PathType Leaf)) {
            return $resolved.Path
        }
    }

    throw "Could not locate the Cytisus repository root. Place this script in the repository root or its scripts/ directory."
}

function Require-Command {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    if ($null -eq (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Required command '$Name' was not found. Install Docker Desktop and ensure Docker is available in PATH."
    }
}

function Get-DotEnvValue {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,

        [Parameter(Mandatory = $true)]
        [string]$Key
    )

    $escapedKey = [Regex]::Escape($Key)
    $line = Get-Content -LiteralPath $Path |
        Where-Object { $_ -match "^\s*$escapedKey\s*=" } |
        Select-Object -First 1

    if ($null -eq $line) {
        return $null
    }

    $value = ($line -split "=", 2)[1].Trim()
    return $value.Trim('"').Trim("'")
}

function Test-TcpPortAvailable {
    param(
        [Parameter(Mandatory = $true)]
        [int]$Port
    )

    $listener = $null

    try {
        $listener = [System.Net.Sockets.TcpListener]::new(
            [System.Net.IPAddress]::Loopback,
            $Port
        )
        $listener.Start()
        return $true
    }
    catch {
        return $false
    }
    finally {
        if ($null -ne $listener) {
            try {
                $listener.Stop()
            }
            catch {
                # Best-effort cleanup only.
            }
        }
    }
}

function Find-AvailablePostgresPort {
    param([int]$RequestedPort)

    if (Test-TcpPortAvailable -Port $RequestedPort) {
        return $RequestedPort
    }

    Write-Warning "PostgreSQL host port $RequestedPort is already in use. Searching for a free local port."

    foreach ($candidate in 55432..55531) {
        if (Test-TcpPortAvailable -Port $candidate) {
            return $candidate
        }
    }

    throw "No available PostgreSQL host port was found in the range 55432-55531."
}

function Invoke-Compose {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$ComposeCommand
    )

    & docker @script:ComposeBase @ComposeCommand

    if ($LASTEXITCODE -ne 0) {
        throw "Docker Compose command failed: docker $($script:ComposeBase -join ' ') $($ComposeCommand -join ' ')"
    }
}

function Show-Diagnostics {
    Write-Host ""
    Write-Warning "Deployment diagnostics follow."

    try {
        & docker @script:ComposeBase ps -a
    }
    catch {
        Write-Warning "Unable to read Compose service status."
    }

    try {
        & docker @script:ComposeBase logs --no-color --tail 150 `
            postgres redis migrate anvil rwa-deploy api worker simulator push-mock web admin-web mailpit minio
    }
    catch {
        Write-Warning "Unable to read Compose logs."
    }
}

function Wait-HttpEndpoint {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,

        [Parameter(Mandatory = $true)]
        [string]$Uri,

        [Parameter(Mandatory = $true)]
        [datetime]$Deadline
    )

    while ((Get-Date) -lt $Deadline) {
        try {
            $response = Invoke-WebRequest `
                -Uri $Uri `
                -Method Get `
                -TimeoutSec 5 `
                -UseBasicParsing

            if ($response.StatusCode -ge 200 -and $response.StatusCode -lt 400) {
                Write-Success "$Name is reachable at $Uri"
                return
            }
        }
        catch {
            # The service may still be starting.
        }

        Start-Sleep -Seconds 2
    }

    throw "$Name did not become reachable before the health-check timeout: $Uri"
}

$composeReady = $false
$repositoryRoot = Resolve-RepositoryRoot
Push-Location $repositoryRoot

try {
    Write-Host "Cytisus v1 local deployment" -ForegroundColor White
    Write-Host "Synthetic data and local simulators only. Never expose this stack as a real financial service." -ForegroundColor Yellow

    $composeFilePath = Join-Path $repositoryRoot "deploy/docker-compose.yml"

    if ([System.IO.Path]::IsPathRooted($EnvFile)) {
        $envFilePath = $EnvFile
    }
    else {
        $envFilePath = Join-Path $repositoryRoot $EnvFile
    }

    if (-not (Test-Path -LiteralPath $envFilePath -PathType Leaf)) {
        throw "Environment file not found: $envFilePath"
    }

    $cytisusEnvironment = Get-DotEnvValue -Path $envFilePath -Key "CYTISUS_ENV"
    $simulatorEnabled = Get-DotEnvValue -Path $envFilePath -Key "CYTISUS_SIMULATOR_ENABLED"

    if ($cytisusEnvironment -ne "local") {
        throw "Refusing deployment because CYTISUS_ENV must be 'local' in $envFilePath."
    }

    if ($simulatorEnabled -ne "true") {
        throw "Refusing deployment because this script expects the explicit local simulator boundary: CYTISUS_SIMULATOR_ENABLED=true."
    }

    if (-not [string]::IsNullOrWhiteSpace($env:CYTISUS_ENV) -and $env:CYTISUS_ENV -ne "local") {
        throw "Refusing deployment because the current process has CYTISUS_ENV='$($env:CYTISUS_ENV)'. Clear it or set it to 'local'."
    }

    Write-Step "Checking Docker"
    Require-Command -Name "docker"

    $dockerVersion = (& docker version --format "{{.Server.Version}}" 2>$null)
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($dockerVersion)) {
        throw "Docker Desktop is not running or its daemon is unavailable."
    }

    $composeVersion = (& docker compose version --short 2>$null)
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($composeVersion)) {
        throw "Docker Compose v2 is required."
    }

    $dockerOs = (& docker info --format "{{.OSType}}" 2>$null)
    if ($LASTEXITCODE -ne 0) {
        throw "Unable to inspect the Docker daemon."
    }

    if ($dockerOs.Trim().ToLowerInvariant() -ne "linux") {
        throw "Cytisus Compose images require Docker Desktop to use Linux containers. Current Docker OSType: $dockerOs"
    }

    Write-Success "Docker Server $dockerVersion; Compose $composeVersion; Linux containers enabled"

    $script:ComposeBase = @(
        "compose",
        "--env-file", $envFilePath,
        "-f", $composeFilePath
    )
    $composeReady = $true

    Write-Step "Checking the local deployment boundary and host ports"

    $existingContainerIds = @(
        & docker @script:ComposeBase ps -q 2>$null |
            Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    )

    if ($existingContainerIds.Count -eq 0) {
        $resolvedPostgresPort = Find-AvailablePostgresPort -RequestedPort $PostgresHostPort

        if ($resolvedPostgresPort -ne $PostgresHostPort) {
            Write-Warning "Using PostgreSQL host port $resolvedPostgresPort instead of $PostgresHostPort."
        }

        $PostgresHostPort = $resolvedPostgresPort

        $fixedPorts = @(1025, 3000, 3001, 6379, 8025, 8080, 8090, 8091, 8545, 9000, 9001)
        $busyPorts = @(
            $fixedPorts |
                Where-Object { -not (Test-TcpPortAvailable -Port $_) }
        )

        if ($busyPorts.Count -gt 0) {
            throw "Required host port(s) are already in use: $($busyPorts -join ', '). Stop the conflicting process/container and run the script again."
        }
    }
    else {
        Write-Host "An existing Cytisus Compose stack was detected; deployment will update it in place."

        $currentPostgresBinding = (& docker @script:ComposeBase port postgres 5432 2>$null | Select-Object -First 1)
        if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($currentPostgresBinding)) {
            $bindingMatch = [Regex]::Match($currentPostgresBinding.Trim(), ":(\d+)$")
            if ($bindingMatch.Success) {
                $PostgresHostPort = [int]$bindingMatch.Groups[1].Value
                Write-Host "Reusing the existing PostgreSQL host port: $PostgresHostPort"
            }
        }
    }

    $env:POSTGRES_HOST_PORT = [string]$PostgresHostPort
    Write-Success "PostgreSQL host debugging port: $PostgresHostPort"

    Write-Step "Validating Docker Compose configuration"
    Invoke-Compose -ComposeCommand @("config", "--quiet")
    Write-Success "Compose configuration is valid"

    if ($StopFirst) {
        Write-Step "Stopping the existing stack without deleting volumes"
        Invoke-Compose -ComposeCommand @("down", "--remove-orphans")
    }

    Write-Step "Building and starting the complete Cytisus stack"

    $upCommand = @(
        "up",
        "-d",
        "--wait",
        "--remove-orphans"
    )

    if (-not $SkipBuild) {
        $upCommand += "--build"
    }

    Invoke-Compose -ComposeCommand $upCommand
    Write-Success "Docker Compose reported the stack ready"

    Write-Step "Checking user-facing endpoints"
    $deadline = (Get-Date).AddSeconds($HealthTimeoutSeconds)

    Wait-HttpEndpoint -Name "API" -Uri "http://localhost:8080/healthz" -Deadline $deadline
    Wait-HttpEndpoint -Name "Provider simulator" -Uri "http://localhost:8090/healthz" -Deadline $deadline
    Wait-HttpEndpoint -Name "Customer Web" -Uri "http://localhost:3000" -Deadline $deadline
    Wait-HttpEndpoint -Name "Admin Web" -Uri "http://localhost:3001" -Deadline $deadline

    Write-Step "Deployment status"
    Invoke-Compose -ComposeCommand @("ps", "-a")

    Write-Host ""
    Write-Host "Cytisus v1 is running locally." -ForegroundColor Green
    Write-Host ""
    Write-Host "Customer Web:       http://localhost:3000"
    Write-Host "Admin Web:          http://localhost:3001"
    Write-Host "API health:         http://localhost:8080/healthz"
    Write-Host "Provider simulator: http://localhost:8090/healthz"
    Write-Host "Mailpit:            http://localhost:8025"
    Write-Host "MinIO console:      http://localhost:9001"
    Write-Host "Anvil RPC:          http://localhost:8545"
    Write-Host "PostgreSQL host:    localhost:$PostgresHostPort"
    Write-Host ""
    Write-Host "Stop without deleting data volumes:" -ForegroundColor DarkGray
    Write-Host "docker compose --env-file `"$envFilePath`" -f `"$composeFilePath`" down --remove-orphans" -ForegroundColor DarkGray

    if ($OpenBrowser) {
        try {
            Start-Process "http://localhost:3000"
            Start-Process "http://localhost:3001"
        }
        catch {
            Write-Warning "The stack is ready, but the browser could not be opened automatically."
        }
    }
}
catch {
    Write-Host ""
    Write-Host "Deployment failed: $($_.Exception.Message)" -ForegroundColor Red

    if ($composeReady) {
        Show-Diagnostics
    }

    exit 1
}
finally {
    Pop-Location
}
