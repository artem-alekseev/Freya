[CmdletBinding()]
param(
    [string]$Root = 'C:\Cabal\Freya EP6',

    [int]$ServerId = 1,

    [int]$ChannelId = 1,

    [ValidateRange(5, 120)]
    [int]$TimeoutSeconds = 30
)

$ErrorActionPreference = 'Stop'

function Test-SamePath {
    param(
        [string]$Left,
        [string]$Right
    )

    if (
        [string]::IsNullOrWhiteSpace($Left) -or
        [string]::IsNullOrWhiteSpace($Right)
    ) {
        return $false
    }

    try {
        $leftFull = [IO.Path]::GetFullPath($Left).TrimEnd('\')
        $rightFull = [IO.Path]::GetFullPath($Right).TrimEnd('\')

        return [string]::Equals(
            $leftFull,
            $rightFull,
            [StringComparison]::OrdinalIgnoreCase
        )
    }
    catch {
        return $false
    }
}

function Get-ProcessPath {
    param(
        [System.Diagnostics.Process]$Process
    )

    try {
        return $Process.Path
    }
    catch {
        return $null
    }
}

function Get-ExpectedProcesses {
    param(
        [pscustomobject]$Service
    )

    return @(
        Get-Process `
            -Name $Service.ProcessName `
            -ErrorAction SilentlyContinue |
        Where-Object {
            Test-SamePath `
                -Left (Get-ProcessPath $_) `
                -Right $Service.Path
        }
    )
}

function Get-PortListeners {
    param(
        [int]$Port
    )

    $connections = @(
        Get-NetTCPConnection `
            -State Listen `
            -LocalPort $Port `
            -ErrorAction SilentlyContinue
    )

    if ($connections.Count -gt 0) {
        return $connections
    }

    $pattern = "^\s*TCP\s+\S+:$Port\s+\S+\s+LISTENING\s+(\d+)\s*$"

    return @(
        & netstat.exe -ano -p tcp |
        ForEach-Object {
            if ($_ -match $pattern) {
                [pscustomobject]@{
                    LocalPort     = $Port
                    OwningProcess = [int]$matches[1]
                }
            }
        }
    )
}

function Assert-PortOwnership {
    param(
        [object[]]$Services
    )

    foreach ($service in $Services) {
        foreach ($listener in @(Get-PortListeners $service.Port)) {
            $owner = Get-Process `
                -Id $listener.OwningProcess `
                -ErrorAction SilentlyContinue

            $ownerPath = if ($owner) {
                Get-ProcessPath $owner
            }
            else {
                $null
            }

            if (-not (Test-SamePath $ownerPath $service.Path)) {
                $shownPath = if ($ownerPath) {
                    $ownerPath
                }
                else {
                    '<unavailable>'
                }

                throw (
                    "Port {0} is owned by PID {1} ({2})." -f `
                        $service.Port,
                        $listener.OwningProcess,
                        $shownPath
                )
            }
        }
    }
}

function Stop-ExpectedService {
    param(
        [pscustomobject]$Service
    )

    foreach ($process in @(Get-ExpectedProcesses $Service)) {
        $processId = $process.Id

        Write-Host (
            "[STOPPING] {0}: PID {1}" -f `
                $Service.Name,
                $processId
        ) -ForegroundColor Yellow

        Stop-Process `
            -Id $processId `
            -Force `
            -ErrorAction SilentlyContinue

        $deadline = (Get-Date).AddSeconds(10)

        do {
            Start-Sleep -Milliseconds 100

            $running = Get-Process `
                -Id $processId `
                -ErrorAction SilentlyContinue
        }
        while (
            $running -and
            (Get-Date) -lt $deadline
        )

        if ($running) {
            throw "Unable to stop $($Service.Name) PID $processId."
        }
    }
}

function Wait-Port {
    param(
        [pscustomobject]$Service,
        [System.Diagnostics.Process]$Process,
        [int]$Timeout
    )

    $deadline = (Get-Date).AddSeconds($Timeout)

    do {
        $running = Get-Process `
            -Id $Process.Id `
            -ErrorAction SilentlyContinue

        if (-not $running) {
            throw (
                "{0} exited before port {1} became ready." -f `
                    $Service.Name,
                    $Service.Port
            )
        }

        foreach ($listener in @(Get-PortListeners $Service.Port)) {
            $owner = Get-Process `
                -Id $listener.OwningProcess `
                -ErrorAction SilentlyContinue

            if (
                $owner -and
                (Test-SamePath (Get-ProcessPath $owner) $Service.Path)
            ) {
                return
            }
        }

        Start-Sleep -Milliseconds 250
    }
    while ((Get-Date) -lt $deadline)

    throw (
        "{0} did not listen on port {1} within {2} seconds." -f `
            $Service.Name,
            $Service.Port,
            $Timeout
    )
}

function Read-AppendedText {
    param(
        [string]$Path,
        [long]$Offset
    )

    if (-not (Test-Path -LiteralPath $Path)) {
        return ''
    }

    $stream = [IO.File]::Open(
        $Path,
        [IO.FileMode]::Open,
        [IO.FileAccess]::Read,
        [IO.FileShare]::ReadWrite
    )

    try {
        if ($stream.Length -lt $Offset) {
            $Offset = 0
        }

        [void]$stream.Seek(
            $Offset,
            [IO.SeekOrigin]::Begin
        )

        $reader = [IO.StreamReader]::new($stream)

        try {
            return $reader.ReadToEnd()
        }
        finally {
            $reader.Dispose()
        }
    }
    finally {
        $stream.Dispose()
    }
}

function Wait-MasterRegistration {
    param(
        [string]$LogPath,
        [long]$Offset,
        [string]$Pattern,
        [string]$ServiceName,
        [int]$Timeout
    )

    $deadline = (Get-Date).AddSeconds($Timeout)

    do {
        if ((Read-AppendedText $LogPath $Offset) -match $Pattern) {
            return
        }

        Start-Sleep -Milliseconds 250
    }
    while ((Get-Date) -lt $deadline)

    throw (
        "{0} did not register in Master within {1} seconds." -f `
            $ServiceName,
            $Timeout
    )
}

function Read-AppendedLogChunk {
    param(
        [string]$Path,
        [long]$Offset
    )

    if (-not (Test-Path -LiteralPath $Path)) {
        return $null
    }

    $stream = [IO.File]::Open(
        $Path,
        [IO.FileMode]::Open,
        [IO.FileAccess]::Read,
        [IO.FileShare]::ReadWrite
    )

    try {
        $wasReset = $stream.Length -lt $Offset

        if ($wasReset) {
            $Offset = 0
        }

        [void]$stream.Seek(
            $Offset,
            [IO.SeekOrigin]::Begin
        )

        $reader = [IO.StreamReader]::new(
            $stream,
            [Text.Encoding]::Default,
            $true,
            4096,
            $true
        )

        try {
            $text = $reader.ReadToEnd()

            return [pscustomobject]@{
                Text       = $text
                NextOffset = [long]$stream.Position
                WasReset   = $wasReset
            }
        }
        finally {
            $reader.Dispose()
        }
    }
    finally {
        $stream.Dispose()
    }
}

function New-LogTailState {
    param(
        [string]$Name,
        [string]$Path,
        [ConsoleColor]$Color,
        [int]$Tail = 10
    )

    return [pscustomobject]@{
        Name        = $Name
        Path        = $Path
        Color       = $Color
        Tail        = $Tail
        Offset      = [long]0
        PendingText = ''
        Initialized = $false
    }
}

function Write-LogLine {
    param(
        [pscustomobject]$State,

        [AllowEmptyString()]
        [string]$Line
    )

    Write-Host `
        -ForegroundColor $State.Color `
        -NoNewline `
        ("[{0}] " -f $State.Name)

    Write-Host $Line
}

function Write-NewLogContent {
    param(
        [pscustomobject]$State
    )

    if (-not (Test-Path -LiteralPath $State.Path)) {
        return
    }

    if (-not $State.Initialized) {
        $initialLines = @(
            Get-Content `
                -LiteralPath $State.Path `
                -Tail $State.Tail `
                -ErrorAction SilentlyContinue
        )

        foreach ($line in $initialLines) {
            Write-LogLine `
                -State $State `
                -Line $line
        }

        $State.Offset = [long](
            Get-Item -LiteralPath $State.Path
        ).Length

        $State.Initialized = $true
        return
    }

    $chunk = Read-AppendedLogChunk `
        -Path $State.Path `
        -Offset $State.Offset

    if ($null -eq $chunk) {
        return
    }

    $State.Offset = $chunk.NextOffset

    if ($chunk.WasReset) {
        $State.PendingText = ''
    }

    if ([string]::IsNullOrEmpty($chunk.Text)) {
        return
    }

    $combined = $State.PendingText + $chunk.Text

    $combined = $combined.
        Replace("`r`n", "`n").
        Replace("`r", "`n")

    $parts = [regex]::Split($combined, "`n")

    for ($index = 0; $index -lt ($parts.Count - 1); $index++) {
        Write-LogLine `
            -State $State `
            -Line $parts[$index]
    }

    if ($combined.EndsWith("`n")) {
        $State.PendingText = ''
    }
    else {
        $State.PendingText = $parts[$parts.Count - 1]
    }
}

function Test-StopKey {
    try {
        while ([Console]::KeyAvailable) {
            $key = [Console]::ReadKey($true)

            if ($key.Key -eq [ConsoleKey]::Q) {
                return $true
            }

            $controlPressed = (
                $key.Modifiers -band
                [ConsoleModifiers]::Control
            ) -ne 0

            if (
                $controlPressed -and
                $key.Key -eq [ConsoleKey]::C
            ) {
                return $true
            }
        }
    }
    catch {
        # Некоторые PowerShell-хосты не поддерживают KeyAvailable.
    }

    return $false
}

function Show-MergedLogs {
    param(
        [object[]]$Logs,
        [object[]]$StartedProcesses
    )

    $states = @(
        foreach ($log in $Logs) {
            New-LogTailState `
                -Name $log.Name `
                -Path $log.Path `
                -Color $log.Color `
                -Tail 10
        }
    )

    $originalTreatControlCAsInput = [Console]::TreatControlCAsInput

    try {
        [Console]::TreatControlCAsInput = $true

        Write-Host ''
        Write-Host 'Серверы запущены.' -ForegroundColor Green
        Write-Host 'Нажмите Q или Ctrl+C для остановки всех серверов.' -ForegroundColor Yellow
        Write-Host ''

        while ($true) {
            if (Test-StopKey) {
                Write-Host ''
                Write-Host 'Получена команда остановки.' -ForegroundColor Yellow
                return
            }

            foreach ($state in $states) {
                Write-NewLogContent $state
            }

            foreach ($entry in $StartedProcesses) {
                $running = Get-Process `
                    -Id $entry.Process.Id `
                    -ErrorAction SilentlyContinue

                if (-not $running) {
                    throw (
                        "{0} exited unexpectedly, PID {1}." -f `
                            $entry.Service.Name,
                            $entry.Process.Id
                    )
                }
            }

            Start-Sleep -Milliseconds 200
        }
    }
    finally {
        [Console]::TreatControlCAsInput = $originalTreatControlCAsInput
    }
}

$rootPath = (Resolve-Path -LiteralPath $Root).Path
$binPath = Join-Path $rootPath 'bin'

$masterAppLog = Join-Path $rootPath 'log\masterserver.log'
$loginAppLog = Join-Path $rootPath 'log\loginserver.log'

$gameAppLog = Join-Path $rootPath (
    'log\gameserver_{0}_{1}.log' -f `
        $ServerId,
        $ChannelId
)

$services = @(
    [pscustomobject]@{
        Name             = 'Master'
        ProcessName      = 'masterserver'
        Path             = Join-Path $binPath 'masterserver.exe'
        Port             = 9001
        Arguments        = @()
        WorkingDirectory = Join-Path $rootPath 'cmd\masterserver'
    },

    [pscustomobject]@{
        Name             = 'Login'
        ProcessName      = 'loginserver'
        Path             = Join-Path $binPath 'loginserver.exe'
        Port             = 38101
        Arguments        = @()
        WorkingDirectory = Join-Path $rootPath 'cmd\loginserver'
    },

    [pscustomobject]@{
        Name             = 'Game'
        ProcessName      = 'gameserver'
        Path             = Join-Path $binPath 'gameserver.exe'
        Port             = 38111
        Arguments        = @(
            [string]$ServerId,
            [string]$ChannelId
        )
        WorkingDirectory = Join-Path $rootPath 'cmd\gameserver'
    }
)

$logs = @(
    [pscustomobject]@{
        Name  = 'MASTER'
        Path  = $masterAppLog
        Color = [ConsoleColor]::Cyan
    },

    [pscustomobject]@{
        Name  = 'LOGIN'
        Path  = $loginAppLog
        Color = [ConsoleColor]::Green
    },

    [pscustomobject]@{
        Name  = 'GAME'
        Path  = $gameAppLog
        Color = [ConsoleColor]::Magenta
    }
)

New-Item `
    -ItemType Directory `
    -Path $binPath `
    -Force |
Out-Null

foreach ($service in $services) {
    if (-not (Test-Path -LiteralPath $service.WorkingDirectory)) {
        throw (
            "Working directory does not exist: {0}" -f `
                $service.WorkingDirectory
        )
    }
}

Assert-PortOwnership $services

# Останавливаем старые серверы:
# Game → Login → Master
for ($index = $services.Count - 1; $index -ge 0; $index--) {
    Stop-ExpectedService $services[$index]
}

foreach ($service in $services) {
    $listeners = @(Get-PortListeners $service.Port)

    if ($listeners.Count -gt 0) {
        throw (
            "Port {0} remained occupied after stopping {1}." -f `
                $service.Port,
                $service.Name
        )
    }
}

$env:GOCACHE = Join-Path $env:TEMP 'freya-go-build'

$buildTargets = @(
    @{
        Output  = $services[0].Path
        Package = './cmd/masterserver'
    },

    @{
        Output  = $services[1].Path
        Package = './cmd/loginserver'
    },

    @{
        Output  = $services[2].Path
        Package = './cmd/gameserver'
    }
)

Write-Host ''
Write-Host 'Сборка серверов...' -ForegroundColor Yellow

Push-Location $rootPath

try {
    foreach ($target in $buildTargets) {
        Write-Host (
            "[BUILD] {0}" -f $target.Package
        )

        & go build `
            -buildvcs=false `
            -o $target.Output `
            $target.Package

        if ($LASTEXITCODE -ne 0) {
            throw "Build failed for $($target.Package)."
        }
    }
}
finally {
    Pop-Location
}

Write-Host '[OK] Сборка завершена.' -ForegroundColor Green

$masterLogOffset = if (Test-Path -LiteralPath $masterAppLog) {
    [long](Get-Item -LiteralPath $masterAppLog).Length
}
else {
    [long]0
}

$started = [System.Collections.Generic.List[object]]::new()

try {
    foreach ($service in $services) {
        Write-Host (
            "[STARTING] {0}..." -f $service.Name
        ) -ForegroundColor Yellow

        $startArgs = @{
            FilePath         = $service.Path
            WorkingDirectory = $service.WorkingDirectory
            WindowStyle      = 'Hidden'
            PassThru         = $true
        }

        if ($service.Arguments.Count -gt 0) {
            $startArgs.ArgumentList = $service.Arguments
        }

        $process = Start-Process @startArgs

        $started.Add(
            [pscustomobject]@{
                Service = $service
                Process = $process
            }
        ) | Out-Null

        Wait-Port `
            -Service $service `
            -Process $process `
            -Timeout $TimeoutSeconds

        if ($service.Name -eq 'Login') {
            Wait-MasterRegistration `
                -LogPath $masterAppLog `
                -Offset $masterLogOffset `
                -Pattern 'Server type: LoginServer' `
                -ServiceName 'Login' `
                -Timeout $TimeoutSeconds
        }
        elseif ($service.Name -eq 'Game') {
            $gamePattern = (
                "Server type: GameServer \(type: \d+, server: {0}, channel: {1}," -f `
                    $ServerId,
                    $ChannelId
            )

            Wait-MasterRegistration `
                -LogPath $masterAppLog `
                -Offset $masterLogOffset `
                -Pattern $gamePattern `
                -ServiceName 'Game' `
                -Timeout $TimeoutSeconds
        }

        Write-Host (
            "[OK] {0}: PID {1}, port {2}" -f `
                $service.Name,
                $process.Id,
                $service.Port
        ) -ForegroundColor Green
    }

    Show-MergedLogs `
        -Logs $logs `
        -StartedProcesses $started
}
catch {
    Write-Host ''
    Write-Host (
        "[ERROR] {0}" -f $_.Exception.Message
    ) -ForegroundColor Red

    throw
}
finally {
    Write-Host ''
    Write-Host 'Остановка серверов...' -ForegroundColor Yellow

    for ($index = $started.Count - 1; $index -ge 0; $index--) {
        $entry = $started[$index]

        try {
            Stop-ExpectedService $entry.Service

            Write-Host (
                "[STOPPED] {0}" -f $entry.Service.Name
            ) -ForegroundColor Green
        }
        catch {
            Write-Warning (
                "Не удалось остановить {0}: {1}" -f `
                    $entry.Service.Name,
                    $_.Exception.Message
            )
        }
    }

    Write-Host 'Все серверы остановлены.' -ForegroundColor Green
}