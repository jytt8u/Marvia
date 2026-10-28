<#
    Установщик и переносимый комплект Windows: EXE, официальный Wintun и лицензия.
    Один EXE умеет открыть окно, но без DLL не создаст VPN-адаптер.
    Пример: ./scripts/package-windows.ps1 -Exe ./dist/marvia-windows.exe -OutputDirectory ./dist -Version 1.0.2
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)] [string]$Exe,
    [Parameter(Mandatory = $true)] [string]$OutputDirectory,
    [string]$WintunArchive,
    [string]$Version = 'dev',
    [string]$MakeNSIS
)

$ErrorActionPreference = 'Stop'
$exePath = (Resolve-Path -LiteralPath $Exe).Path
[IO.Directory]::CreateDirectory([IO.Path]::GetFullPath($OutputDirectory)) | Out-Null
$outputPath = (Resolve-Path -LiteralPath $OutputDirectory).Path
$temporaryPath = Join-Path $outputPath ('windows-package-' + [guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($temporaryPath) | Out-Null

try {
    if (-not $WintunArchive) {
        $WintunArchive = Join-Path $temporaryPath 'wintun.zip'
        Invoke-WebRequest 'https://www.wintun.net/builds/wintun-0.14.1.zip' -OutFile $WintunArchive
    }
    # Версия закреплена вместе с опубликованной суммой: замена архива на
    # сервере не должна незаметно менять драйвер внутри нашего выпуска.
    $expected = '07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51'
    if ((Get-FileHash -LiteralPath $WintunArchive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected) {
        throw 'контрольная сумма официального архива Wintun не совпадает'
    }
    $bundlePath = Join-Path $temporaryPath 'bundle'
    [IO.Directory]::CreateDirectory($bundlePath) | Out-Null
    Copy-Item -LiteralPath $exePath -Destination (Join-Path $bundlePath 'marvia-windows.exe')
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $archive = [IO.Compression.ZipFile]::OpenRead((Resolve-Path -LiteralPath $WintunArchive).Path)
    try {
        foreach ($file in @(
            @{ Entry = 'wintun/bin/amd64/wintun.dll'; Name = 'wintun.dll' },
            @{ Entry = 'wintun/LICENSE.txt'; Name = 'WINTUN-LICENSE.txt' }
        )) {
            $entry = $archive.GetEntry($file.Entry)
            if (-not $entry) { throw "в архиве отсутствует $($file.Entry)" }
            [IO.Compression.ZipFileExtensions]::ExtractToFile($entry, (Join-Path $bundlePath $file.Name))
        }
    } finally { $archive.Dispose() }
    if ([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT) {
        $signature = Get-AuthenticodeSignature -LiteralPath (Join-Path $bundlePath 'wintun.dll')
        if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch 'CN=WireGuard LLC(?:,|$)') {
            throw 'не подтверждена подпись WireGuard у wintun.dll'
        }
    }
    $instructions = @'
Marvia для Windows x64

1. Распакуйте весь архив в одну папку. Не запускайте EXE прямо из ZIP.
2. Завершите старую Marvia через «Выйти» в трее.
3. Запустите marvia-windows.exe. Подтвердите штатный запрос прав администратора.
4. Добавьте свою ссылку доступа и нажмите кнопку подключения.

Файл wintun.dll должен оставаться рядом с EXE. Он подписан WireGuard LLC.
Для окна нужен Microsoft Edge WebView2 Runtime. Если его нет, Marvia предложит
официальную страницу Microsoft; установите Evergreen Runtime и запустите Marvia снова.
https://developer.microsoft.com/microsoft-edge/webview2/#download-section
Сам EXE Marvia пока без подписи издателя; SmartScreen может предупреждать.
Наличие DLL устраняет ошибку создания адаптера, но не гарантирует доступность ноды.
'@
    [IO.File]::WriteAllText((Join-Path $bundlePath 'НАЧНИТЕ-ЗДЕСЬ.txt'), $instructions, [Text.UTF8Encoding]::new($false))
    $sums = Get-ChildItem -LiteralPath $bundlePath -File | Sort-Object Name | ForEach-Object {
        '{0}  {1}' -f (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant(), $_.Name
    }
    [IO.File]::WriteAllLines((Join-Path $bundlePath 'SHA256SUMS'), [string[]]$sums, [Text.UTF8Encoding]::new($false))
    if (-not $MakeNSIS) { $MakeNSIS = (Get-Command makensis -ErrorAction Stop).Source }
    $numericVersion = '0.0.0.0'
    if ($Version -match '^v?(\d+)\.(\d+)\.(\d+)') {
        $numericVersion = '{0}.{1}.{2}.0' -f $Matches[1], $Matches[2], $Matches[3]
    }
    $versionLabel = $Version -replace '^v', ''
    if ($versionLabel -notmatch '^[A-Za-z0-9.+_-]+$') { throw 'недопустимая версия приложения' }
    $flag = if ([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT) { '/' } else { '-' }
    $installerPath = Join-Path $temporaryPath 'marvia-windows-setup.exe'
    & $MakeNSIS "${flag}V2" "${flag}WX" "${flag}INPUTCHARSET" 'UTF8' `
        "${flag}DINPUT_DIR=$bundlePath" "${flag}DOUTPUT_FILE=$installerPath" `
        "${flag}DVERSION=$versionLabel" "${flag}DNUMERIC_VERSION=$numericVersion" `
        (Join-Path $PSScriptRoot 'windows-installer.nsi')
    if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $installerPath)) { throw 'не собрался установщик Windows' }
    $zipPath = Join-Path $temporaryPath 'marvia-windows.zip'
    [IO.Compression.ZipFile]::CreateFromDirectory($bundlePath, $zipPath)
    Move-Item -LiteralPath $installerPath -Destination (Join-Path $outputPath 'marvia-windows-setup.exe') -Force
    Move-Item -LiteralPath $zipPath -Destination (Join-Path $outputPath 'marvia-windows.zip') -Force
    Write-Host "Комплект готов: $(Join-Path $outputPath 'marvia-windows.zip')"
    Write-Host "Установщик готов: $(Join-Path $outputPath 'marvia-windows-setup.exe')"
} finally {
    # Удаляем только собственную случайную папку внутри явно заданного вывода.
    $resolvedTemporary = [IO.Path]::GetFullPath($temporaryPath)
    $prefix = $outputPath.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if (-not $resolvedTemporary.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'временная папка вышла за пределы каталога сборки'
    }
    Remove-Item -LiteralPath $resolvedTemporary -Recurse -Force
}
