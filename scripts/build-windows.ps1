#Requires -PSEdition Core
<#
.SYNOPSIS
    Build script for UniviewCodeBuddyProxy Windows app.
.DESCRIPTION
    Builds WinUI 3 desktop app + Server CLI, packages as ZIP.
    Reads APP_VERSION from environment variable.
#>

$ErrorActionPreference = "Stop"

# ─── Configuration ───────────────────────────────────────────────
$AppName = "UniviewCodeBuddyProxy"
$Version = if ($env:APP_VERSION) { $env:APP_VERSION } else { "0.0.0-dev" }
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Resolve-Path (Join-Path $ScriptDir "..")
$WindowsDir = Join-Path $ProjectRoot "windows"
$DistDir = Join-Path $ProjectRoot "dist"

# ─── Banner ──────────────────────────────────────────────────────
Write-Host ""
Write-Host "╔══════════════════════════════════════════════════════╗"
Write-Host "║  Building $AppName v$Version for Windows"
Write-Host "╚══════════════════════════════════════════════════════╝"
Write-Host ""

# ─── 1. Restore workloads ───────────────────────────────────────
Write-Host "📦 Step 1/5: Restoring workloads..."
Push-Location $WindowsDir
try {
    dotnet workload restore $AppName\$AppName.csproj
    Write-Host "  ✅ Workloads restored"
}
finally {
    Pop-Location
}

# ─── 2. Publish WinUI app (self-contained x64) ──────────────────
Write-Host ""
Write-Host "📦 Step 2/5: Publishing WinUI app (x64, self-contained)..."

$WinUIProject = Join-Path $WindowsDir "$AppName\$AppName.csproj"
$WinUIPublishDir = Join-Path $WindowsDir "$AppName\bin\x64\Release\net8.0-windows10.0.19041.0\win-x64\publish"

dotnet restore $WinUIProject

msbuild $WinUIProject `
    /t:Publish /restore `
    /p:Configuration=Release `
    /p:Platform=x64 `
    /p:SelfContained=true `
    /p:RuntimeIdentifier=win-x64 `
    /p:PublishSingleFile=false `
    /p:WindowsAppSDKSelfContained=true

if (-not (Test-Path $WinUIPublishDir)) {
    Write-Error "❌ Publish directory not found: $WinUIPublishDir"
    exit 1
}

# Verify x:Bind bindings were generated (XamlCompiler must produce .g.cs files)
# .g.cs files are generated under the RID subdirectory (win-x64) when RuntimeIdentifier is set
$gcsPattern = Join-Path $WindowsDir "$AppName\obj\x64\Release\net8.0-windows10.0.19041.0\win-x64\*.g.cs"
$gcsFiles = Get-ChildItem -Path $gcsPattern -ErrorAction SilentlyContinue
if ($gcsFiles.Count -eq 0) {
    Write-Error "❌ No .g.cs files generated — x:Bind bindings missing! XamlCompiler may have been skipped."
    exit 1
}

Write-Host "  ✅ WinUI app published ($($gcsFiles.Count) .g.cs files generated, x:Bind OK)"

# ─── 3. Package WinUI app as ZIP ────────────────────────────────
Write-Host ""
Write-Host "📦 Step 3/5: Creating ZIP package..."

New-Item -ItemType Directory -Force -Path $DistDir | Out-Null

$WinZipName = "$AppName-Windows-x64.zip"
$WinZipPath = Join-Path $DistDir $WinZipName

# Remove existing ZIP if present
if (Test-Path $WinZipPath) { Remove-Item $WinZipPath -Force }

# Create ZIP from publish directory
Compress-Archive -Path "$WinUIPublishDir\*" -DestinationPath $WinZipPath -CompressionLevel Optimal

Write-Host "  ✅ ZIP created: $WinZipPath"
Write-Host "     Size: $([math]::Round((Get-Item $WinZipPath).Length / 1MB, 1)) MB"

# ─── 4. Publish Server CLI ──────────────────────────────────────
Write-Host ""
Write-Host "📦 Step 4/5: Publishing Server CLI..."

$ServerProject = Join-Path $WindowsDir "UniviewCodeBuddyProxy.Server\UniviewCodeBuddyProxy.Server.csproj"

@(
    @{ Rid = "win-x64";  ZipName = "codebuddy-proxy-server-win-x64.zip" },
    @{ Rid = "linux-x64"; ZipName = "codebuddy-proxy-server-linux-x64.zip" }
) | ForEach-Object {
    $rid = $_.Rid
    $zipName = $_.ZipName
    $zipPath = Join-Path $DistDir $zipName

    Write-Host "  📦 Publishing for $rid..."

    dotnet publish $ServerProject `
        -c Release `
        -r $rid `
        --self-contained true `
        -p:PublishSingleFile=true `
        -p:IncludeNativeLibrariesForSelfExtract=true

    $serverPublishDir = Join-Path $WindowsDir "UniviewCodeBuddyProxy.Server\bin\Release\net8.0\$rid\publish"

    if (Test-Path $serverPublishDir) {
        if (Test-Path $zipPath) { Remove-Item $zipPath -Force }
        Compress-Archive -Path "$serverPublishDir\*" -DestinationPath $zipPath -CompressionLevel Optimal
        Write-Host "  ✅ Server CLI ($rid): $zipPath"
    } else {
        Write-Host "  ⚠️  Server publish dir not found for $rid, skipping"
    }
}

# ─── 5. Summary ─────────────────────────────────────────────────
Write-Host ""
Write-Host "╔══════════════════════════════════════════════════════╗"
Write-Host "║  Build complete!"
Write-Host "║"
Write-Host "║  WinUI App:  $WinZipPath"
Write-Host "║  Version:    $Version"
Write-Host "║  Platform:   x64 (self-contained)"
Write-Host "╚══════════════════════════════════════════════════════╝"
