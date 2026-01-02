#requires -Version 5.1
<#
.SYNOPSIS
    Build winres-gen on Windows.
.DESCRIPTION
    Targets:
      build  (default)  compile winres-gen.exe for the host platform
      clean             remove build output
      test              gofmt check, go vet, go test
      all               clean, test, then build
    Set VERSION to stamp the release name onto cross-built binaries (cross target).
.EXAMPLE
    .\build.ps1 all
#>
[CmdletBinding()]
param(
    [ValidateSet('build', 'clean', 'test', 'all', 'cross')]
    [string]$Target = 'build'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Root = $PSScriptRoot
$Out = Join-Path $Root 'winres-gen.exe'
$Dist = Join-Path $Root 'dist'

function Invoke-Step {
    # go writes progress to stderr, so failures are detected by exit code only.
    param([string]$Label, [scriptblock]$Body)
    Write-Host "==> $Label"
    & $Body
    if ($LASTEXITCODE -ne 0) { throw "$Label failed (exit $LASTEXITCODE)" }
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error 'go was not found on PATH.'
    exit 1
}

function Step-Clean {
    Write-Host '==> clean'
    if (Test-Path $Out) { Remove-Item -Force $Out }
    if (Test-Path $Dist) { Remove-Item -Recurse -Force $Dist }
    Invoke-Step 'go clean' { go clean }
}

function Step-Test {
    Write-Host '==> gofmt'
    $unformatted = & gofmt -l $Root
    if ($unformatted) {
        Write-Host 'These files need gofmt:'
        $unformatted | ForEach-Object { Write-Host "  $_" }
        throw 'gofmt check failed'
    }
    Invoke-Step 'go vet' { go vet ./... }
    Invoke-Step 'go test' { go test ./... }
}

function Step-Build {
    Invoke-Step 'build' { go build -trimpath -ldflags '-s -w' -o $Out . }
    Write-Host "built $Out"
}

function Step-Cross {
    # Same matrix the release workflow builds, for reproducing CI locally.
    $targets = @(
        @{ os = 'windows'; arch = 'amd64'; ext = '.exe' }
        @{ os = 'windows'; arch = 'arm64'; ext = '.exe' }
        @{ os = 'linux'; arch = 'amd64'; ext = '' }
        @{ os = 'linux'; arch = 'arm64'; ext = '' }
        @{ os = 'darwin'; arch = 'amd64'; ext = '' }
        @{ os = 'darwin'; arch = 'arm64'; ext = '' }
    )
    New-Item -ItemType Directory -Force -Path $Dist | Out-Null
    foreach ($t in $targets) {
        $name = "winres-gen-$($t.os)-$($t.arch)$($t.ext)"
        $env:GOOS = $t.os
        $env:GOARCH = $t.arch
        $env:CGO_ENABLED = '0'
        try {
            Invoke-Step $name { go build -trimpath -ldflags '-s -w' -o (Join-Path $Dist $name) . }
        }
        finally {
            Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
        }
    }
    Write-Host "built $((Get-ChildItem $Dist).Count) binaries in $Dist"
}

try {
    switch ($Target) {
        'clean' { Step-Clean }
        'test' { Step-Test }
        'build' { Step-Build }
        'cross' { Step-Cross }
        'all' { Step-Clean; Step-Test; Step-Build }
    }
}
catch {
    Write-Host "error: $_" -ForegroundColor Red
    exit 1
}
exit 0
