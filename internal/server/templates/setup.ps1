# Kronos Worker Node Agent Windows Setup
$ErrorActionPreference = "Stop"

Write-Host "=== Kronos Node Agent Setup ===" -ForegroundColor Cyan

# 1. Resolve Master URL
$MasterURL = $env:KRONOS_MASTER_URL
if (-not $MasterURL) {
    $DefaultMaster = "{{ .MasterURL }}"
    $InputURL = Read-Host "Enter Master Server URL [default: $DefaultMaster]"
    if ($InputURL) { $MasterURL = $InputURL } else { $MasterURL = $DefaultMaster }
}

# 2. Resolve Agent Secret
$AgentSecret = $env:KRONOS_AGENT_SECRET
if (-not $AgentSecret) {
    $AgentSecret = Read-Host "Enter Node AGENT_SECRET" -AsSecureString
    $BSTR = [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($AgentSecret)
    $AgentSecret = [System.Runtime.InteropServices.Marshal]::PtrToStringAuto($BSTR)
}

# 3. Resolve Allowed Slugs & Task Unit
$AllowedSlugs = $env:KRONOS_ALLOWED_SLUGS
$TaskUnit = $env:KRONOS_TASK_UNIT
if (-not $TaskUnit) { $TaskUnit = "cpu" }

# 4. Create Config & Binary Directories
$ConfigDir = Join-Path $env:APPDATA "Kronos"
$BinDir = Join-Path $ConfigDir "bin"
New-Item -ItemType Directory -Force -Path $ConfigDir | Out-Null
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null

# 5. Save agent.conf
$ConfFile = Join-Path $ConfigDir "agent.conf"
$ConfContent = @"
# Kronos Node Agent Configuration
MASTER_URL=$MasterURL
AGENT_SECRET=$AgentSecret
ALLOWED_SLUGS=$AllowedSlugs
TASK_UNIT=$TaskUnit
"@

Set-Content -Path $ConfFile -Value $ConfContent -Encoding UTF8
Write-Host "`nConfiguration saved to $ConfFile" -ForegroundColor Green

Write-Host "=== Setup Completed Successfully! ===" -ForegroundColor Green
