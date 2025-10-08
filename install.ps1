# Remove AutoRun entry
$regPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
Remove-ItemProperty -Path $regPath -Name "dekapu-dashboard-setup" -ErrorAction SilentlyContinue

$envOkFlag = "$env:TEMP\dekapu-dashboard-env-ok.flag"

$IsAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

function Configure_DockerDesktop_Autostart {
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
}

function Is_WSL_Installed {
    Write-Host "Checking WSL installation status..."

    if (-not (Get-Command wsl.exe -ErrorAction SilentlyContinue)) {
        Write-Host "wsl.exe not found. This system may not support WSL."
        exit 1
    }

    $proc = New-Object System.Diagnostics.Process -Property @{
        StartInfo = New-Object System.Diagnostics.ProcessStartInfo -Property @{
            FileName = "wsl.exe"
            Arguments = "-l -v"
            RedirectStandardOutput = $true
            RedirectStandardError  = $true
            UseShellExecute = $false
            CreateNoWindow = $true
        }
    }

    $proc.Start() | Out-Null

    if (-not $proc.WaitForExit(5000)) {
        Write-Host "WSL check timed out (possible uninstalled state)."
        try { $proc.Kill() } catch {}
        return $false
    }

    if ($proc.ExitCode -eq 0 -or $proc.ExitCode -eq -1) {
        Write-Host "WSL is already installed."
        return $true
    } else {
        Write-Host "WSL not installed."
        return $false
    }
}

function Execution_Env_Setup {
    # Privilege escalation for application install
    if (-not $IsAdmin) {
        $tmp = "$env:TEMP\dekapu-dashboard-setup.ps1"
        $url = "https://raw.githubusercontent.com/njm2360/dekapu-dashboard/main/install.ps1"

        if (-not (Test-Path $tmp)) {
            Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing
        }

        $wtPath = (Get-Command wt.exe -ErrorAction SilentlyContinue).Path

        if ($wtPath) {
            # Windows Terminal（UWP）
            Start-Process -FilePath $wtPath -Verb RunAs -ArgumentList "powershell -ExecutionPolicy Bypass -File `"$tmp`""
        } else {
            # Fallback classic terminal
            Start-Process powershell -Verb RunAs -ArgumentList "-NoProfile","-ExecutionPolicy","Bypass","-File","`"$tmp`""
        }

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
        if ($LASTEXITCODE -ne 0) {
            Write-Host "Git installation failed. Aborting process."
            exit 1
        }
    }

    # Install Docker Desktop
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        Write-Host "Installing Docker Desktop..."
        winget install --id Docker.DockerDesktop -e --accept-source-agreements --accept-package-agreements --override "install --quiet --accept-license"
        if ($LASTEXITCODE -ne 0) {
            Write-Host "Docker Desktop installation failed. Aborting process."
            exit 1
        }
        Configure_DockerDesktop_Autostart
    }

    # Install WSL2 for Docker backend
    if (-not (Is_WSL_Installed)) {
        Write-Host "Enabling WSL feature..."

        try {
            wsl --install --no-distribution
        } catch {
            Write-Host "Failed to install WSL. Aborting process."
            exit 1
        }

        New-Item -ItemType File -Path $envOkFlag -Force | Out-Null

        # Register script in AutoRun for setup after reboot
        $regPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
        Set-ItemProperty -Path $regPath -Name "dekapu-dashboard-setup" -Value "powershell -ExecutionPolicy Bypass -File `"$env:TEMP\dekapu-dashboard-setup.ps1`""

        Restart-Computer
        exit 0
    }

    New-Item -ItemType File -Path $envOkFlag -Force | Out-Null
}

function Application_Setup {
    # Git path
    $gitBin = "$env:ProgramFiles\Git\cmd"

    # Ensure PATH includes Git
    if (-not ($env:PATH -like "*$gitBin*")) {
        if (Test-Path $gitBin) {
            Write-Host "Temporarily adding Git to PATH..."
            $env:PATH = "$env:PATH;$gitBin"
        } else {
            Write-Host "Git not found at expected path: $gitBin"
        }
    }

    # Clone repository
    $repo = "https://github.com/njm2360/dekapu-dashboard.git"
    $dir = "$env:USERPROFILE\dekapu-dashboard"
    if (-not (Test-Path $dir)) {
        git clone $repo $dir
    }
    cd $dir

    # Set environment
    (Get-Content ".env.template") |
    ForEach-Object {
        if ($_ -match '^VRCHAT_LOG_DIR=') {
            $winPath = [Environment]::GetEnvironmentVariable('USERPROFILE')

            $drive = $winPath.Substring(0,1).ToLower()

            $converted = $winPath -replace '^[A-Za-z]:', "/host_mnt/$drive"
            $converted = $converted -replace '\\', '/'

            "VRCHAT_LOG_DIR=${converted}/AppData/LocalLow/VRChat/VRChat"
        }
        else {
            $_
        }
    } | Set-Content ".env"

    # Docker paths
    $dockerBin = "$env:ProgramFiles\Docker\Docker\resources\bin"
    $dockerDesktopExe = "$env:ProgramFiles\Docker\Docker\Docker Desktop.exe"

    # Ensure PATH includes Docker resources
    if (-not ($env:PATH -like "*$dockerBin*")) {
        Write-Host "Temporarily adding Docker resources to PATH..."
        $env:PATH = "$env:PATH;$dockerBin"
    }

    # Start Docker Desktop
    Write-Host "Starting Docker Desktop..."
    Start-Process -FilePath $dockerDesktopExe

    # Wait for Docker Engine starting
    Write-Host "Waiting for Docker Engine to start..."
    $maxWait = 3600
    $elapsed = 0
    while ($true) {
        docker info *> $null
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
    docker compose up -d

    Pause
}

# Main
if (-not (Test-Path $envOkFlag)) {
    Execution_Env_Setup
}

Application_Setup
