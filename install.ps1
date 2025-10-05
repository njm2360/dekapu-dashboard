$regPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
Remove-ItemProperty -Path $regPath -Name "PostInstallScript" -ErrorAction SilentlyContinue

$IsAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

# Check permission
if (-not $IsAdmin) {
    $tmp = "$env:TEMP\bootstrap_elevated.ps1"
    $url = "https://raw.githubusercontent.com/njm2360/dekapu-dashboard/main/install.ps1"

    Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing
    $content = Get-Content $tmp -Raw
    if ($PSVersionTable.PSVersion.Major -ge 7) {
        Set-Content -Path $tmp -Value $content -Encoding utf8BOM
    } else {
        Set-Content -Path $tmp -Value $content -Encoding UTF8
    }

    Start-Process powershell -Verb RunAs -ArgumentList "-NoExit", "-ExecutionPolicy Bypass -File `"$tmp`""

    exit 0
}

# Check Winget
if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
    Write-Host "Winget not found. Please update App Installer if you are using Windows 10."
    exit 1
}

# Install Git
if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    Write-Host "Installing Git..."
    winget install --id Git.Git -e --accept-source-agreements --accept-package-agreements
}

# Install Docker Desktop
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Host "Installing Docker Desktop..."
    Invoke-WebRequest "https://desktop.docker.com/win/main/amd64/Docker%20Desktop%20Installer.exe" -OutFile "$env:TEMP\DockerDesktopInstaller.exe"
    Start-Process -FilePath "$env:TEMP\DockerDesktopInstaller.exe" -ArgumentList "install", "--quiet", "--accept-license" -Wait
}

# Configure Autostart
$dockerSettings = "$env:APPDATA\Docker\settings-store.json"

$dockerDir = Split-Path $dockerSettings
if (-not (Test-Path $dockerDir)) {
    New-Item -ItemType Directory -Path $dockerDir | Out-Null
}

$jsonContent = @{
    AutoStart = $true
    OpenUIOnStartupDisabled = $true
} | ConvertTo-Json -Depth 2

$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($dockerSettings, $jsonContent, $utf8NoBom)

# Install WSL2 for Docker backend
$wslFeature = Get-WindowsOptionalFeature -Online -FeatureName Microsoft-Windows-Subsystem-Linux
$vmFeature  = Get-WindowsOptionalFeature -Online -FeatureName VirtualMachinePlatform

if ($wslFeature.State -ne 'Enabled' -or $vmFeature.State -ne 'Enabled') {
    Write-Host "Enabling WSL feature..."
    try {
        wsl --install --no-distribution
    } catch {
        Write-Host "Failed to install WSL. Aborting process."
        exit 1
    }

    $regPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
    Set-ItemProperty -Path $regPath -Name "PostInstallScript" -Value "powershell -ExecutionPolicy Bypass -File `"$env:TEMP\bootstrap_elevated.ps1`""
    Restart-Computer
    exit 0
}

# Clone repository
$repo = "https://github.com/njm2360/dekapu-dashboard.git"
$dir = "$env:USERPROFILE\dekapu-dashboard"
if (-not (Test-Path $dir)) {
    git clone $repo $dir
}
cd $dir

# Set environment
(Get-Content ".env.template") -replace '^USERNAME=.*', "USERNAME=$env:USERNAME" | Set-Content ".env"

# Docker Executable
$dockerExe = "$env:ProgramFiles\Docker\Docker\resources\bin\docker.exe"

if (-not (Test-Path $dockerExe)) {
    Write-Host "Docker not found. Aborting process."
    exit 1
}

# Wait for Docker Engine starting
Write-Host "Waiting for Docker Engine to start..."
$maxWait = 300
$elapsed = 0
while ($true) {
    & $dockerExe info *> $null
    if ($LASTEXITCODE -eq 0) {
        break
    }
    if ($elapsed -ge $maxWait) {
        Write-Host "Docker Engine failed to start. Aborting process."
        exit 1
    }
    Start-Sleep -Seconds 5
    $elapsed += 5
}

# Launch docker containers
Write-Host "Starting Docker containers..."
& $dockerExe compose up -d
