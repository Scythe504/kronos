# Kronos Worker Node Agent Windows Setup
$ErrorActionPreference = "Stop"

# ==========================================
# 1. Top-Level Variable Initialization
# ==========================================
$Version = if ($env:KRONOS_VERSION) { $env:KRONOS_VERSION } else { "{{ .Version }}" }
if ($Version -eq "{{ .Version }}" -or -not $Version) { $Version = "v0.1.0" }

$Repo = if ($env:KRONOS_REPO) { $env:KRONOS_REPO } else { "scythe504/kronos" }

$DefaultMaster = "{{ .MasterURL }}"
if ($DefaultMaster -eq "{{ .MasterURL }}" -or -not $DefaultMaster) { $DefaultMaster = "http://localhost:8080" }
$MasterURL = if ($env:KRONOS_MASTER_URL) { $env:KRONOS_MASTER_URL } else { $DefaultMaster }

$AllowedSlugs = if ($env:KRONOS_ALLOWED_SLUGS) { $env:KRONOS_ALLOWED_SLUGS } else { "" }
$TaskUnit = if ($env:KRONOS_TASK_UNIT) { $env:KRONOS_TASK_UNIT } else { "cpu" }

# Directories & File Paths
$ConfigDir = Join-Path $env:APPDATA "Kronos"
$BinDir = Join-Path $ConfigDir "bin"
$ConfFile = Join-Path $ConfigDir "agent.conf"
$BinaryPath = Join-Path $BinDir "kronos.exe"

Write-Host "=== Kronos Node Agent Setup ($Version) ===" -ForegroundColor Cyan

# Interactive prompt if interactive and KRONOS_MASTER_URL not set
if (-not $env:KRONOS_MASTER_URL -and [Environment]::UserInteractive) {
    $InputURL = Read-Host "Enter Master Server URL [default: $MasterURL]"
    if ($InputURL) { $MasterURL = $InputURL }
}

# ==========================================
# 2. Detect Architecture & Setup Download URLs
# ==========================================
$ArchRaw = $env:PROCESSOR_ARCHITECTURE
switch -Regex ($ArchRaw) {
    "AMD64|amd64" { $Arch = "x86_64" }
    "ARM64|arm64" { $Arch = "arm64" }
    "x86|386"     { $Arch = "i386" }
    Default       { $Arch = "x86_64" }
}

$ArchiveName = "kronos_Windows_${Arch}.zip"
$DownloadURL = "https://github.com/${Repo}/releases/download/${Version}/${ArchiveName}"

$VersionNoV = $Version.TrimStart('v')
$ChecksumsName = "kronos_${VersionNoV}_checksums.txt"
$ChecksumsURL = "https://github.com/${Repo}/releases/download/${Version}/${ChecksumsName}"

# ==========================================
# 3. Create Directories & Download Release
# ==========================================
New-Item -ItemType Directory -Force -Path $ConfigDir | Out-Null
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null

$TmpDir = Join-Path $env:TEMP ("kronos-install-" + [Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Force -Path $TmpDir | Out-Null

try {
    $ZipPath = Join-Path $TmpDir $ArchiveName
    $ChecksumsPath = Join-Path $TmpDir $ChecksumsName

    Write-Host "Downloading $ArchiveName from $DownloadURL..." -ForegroundColor Cyan
    Invoke-WebRequest -Uri $DownloadURL -OutFile $ZipPath -UseBasicParsing

    try {
        Invoke-WebRequest -Uri $ChecksumsURL -OutFile $ChecksumsPath -UseBasicParsing
        if (Test-Path $ChecksumsPath) {
            Write-Host "Verifying checksum..." -ForegroundColor Cyan
            $FileHash = (Get-FileHash -Path $ZipPath -Algorithm SHA256).Hash.ToLower()
            $ChecksumContent = Get-Content -Path $ChecksumsPath
            $MatchingLine = $ChecksumContent | Where-Object { $_ -match $ArchiveName }
            if ($MatchingLine) {
                $ExpectedHash = ($MatchingLine -split '\s+')[0].ToLower()
                if ($FileHash -eq $ExpectedHash) {
                    Write-Host "Checksum verified successfully!" -ForegroundColor Green
                } else {
                    Write-Host "Warning: Checksum mismatch! Expected: $ExpectedHash, Got: $FileHash" -ForegroundColor Yellow
                }
            }
        }
    } catch {
        Write-Host "Warning: Could not fetch checksum file for verification." -ForegroundColor Yellow
    }

    Write-Host "Extracting archive..." -ForegroundColor Cyan
    Expand-Archive -Path $ZipPath -DestinationPath $TmpDir -Force

    $ExtractedExe = Join-Path $TmpDir "kronos.exe"
    if (Test-Path $ExtractedExe) {
        Copy-Item -Path $ExtractedExe -Destination $BinaryPath -Force
        Write-Host "Binary installed to $BinaryPath" -ForegroundColor Green
    } else {
        throw "Binary kronos.exe was not found in downloaded zip archive."
    }
} finally {
    Remove-Item -Path $TmpDir -Recurse -Force -ErrorAction SilentlyContinue
}

# ==========================================
# 4. Update PATH Environment Variable
# ==========================================
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$BinDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$BinDir", "User")
    Write-Host "Added $BinDir to User PATH" -ForegroundColor Green
}

# ==========================================
# 5. Save Configuration to agent.conf
# ==========================================
$ConfContent = @"
# Kronos Node Agent Configuration
MASTER_URL=$MasterURL
ALLOWED_SLUGS=$AllowedSlugs
TASK_UNIT=$TaskUnit
"@

Set-Content -Path $ConfFile -Value $ConfContent -Encoding UTF8
Write-Host "`nConfiguration saved to $ConfFile" -ForegroundColor Green

Write-Host "=== Setup Completed Successfully! ===" -ForegroundColor Green

