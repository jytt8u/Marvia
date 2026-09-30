<#
    Проверка установки, повторной установки и удаления в случайной папке.
    Тестовая сборка не меняет реестр, ярлыки или работающий VPN пользователя.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)] [string]$Package,
    [Parameter(Mandatory = $true)] [string]$MakeNSIS,
    [Parameter(Mandatory = $true)] [string]$WorkDirectory
)
$ErrorActionPreference = 'Stop'
if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { throw 'эта проверка запускается на Windows' }
$packagePath = (Resolve-Path -LiteralPath $Package).Path
[IO.Directory]::CreateDirectory([IO.Path]::GetFullPath($WorkDirectory)) | Out-Null
$workRoot = (Resolve-Path -LiteralPath $WorkDirectory).Path
$testRoot = Join-Path $workRoot ('installer-test-' + [guid]::NewGuid().ToString('N'))
$payload = Join-Path $testRoot 'payload'
$installation = Join-Path $testRoot 'Папка с пробелами'
$testSetup = Join-Path $testRoot 'test-setup.exe'
try {
    Expand-Archive -LiteralPath $packagePath -DestinationPath $payload
    & $MakeNSIS '/V2' '/WX' '/INPUTCHARSET' 'UTF8' '/DTEST_INSTALL' `
        "/DINPUT_DIR=$payload" "/DOUTPUT_FILE=$testSetup" '/DVERSION=1.0.2-test' '/DNUMERIC_VERSION=1.0.2.0' `
        (Join-Path $PSScriptRoot 'windows-installer.nsi')
    if ($LASTEXITCODE -ne 0) { throw 'не собрался тестовый установщик' }
    [IO.Directory]::CreateDirectory($installation) | Out-Null
    $sentinel = Join-Path $installation 'чужой документ.txt'
    [IO.File]::WriteAllText($sentinel, 'сохранить при обновлении и удалении')
    foreach ($attempt in 1..2) {
        $process = Start-Process -FilePath $testSetup -ArgumentList "/S /D=$installation" -WindowStyle Hidden -Wait -PassThru
        if ($process.ExitCode -ne 0) { throw "установка $attempt завершилась с кодом $($process.ExitCode)" }
        foreach ($name in 'marvia-windows.exe', 'wintun.dll', 'WINTUN-LICENSE.txt', 'НАЧНИТЕ-ЗДЕСЬ.txt') {
            $expected = (Get-FileHash -LiteralPath (Join-Path $payload $name)).Hash
            $actual = (Get-FileHash -LiteralPath (Join-Path $installation $name)).Hash
            if ($expected -ne $actual) { throw "при установке изменился $name" }
        }
        if ([IO.File]::ReadAllText($sentinel) -ne 'сохранить при обновлении и удалении') { throw 'изменён посторонний файл' }
    }
    $uninstaller = Join-Path $installation 'Удалить Marvia.exe'
    $process = Start-Process -FilePath $uninstaller -ArgumentList "/S _?=$installation" -WindowStyle Hidden -Wait -PassThru
    if ($process.ExitCode -ne 0) { throw "удаление завершилось с кодом $($process.ExitCode)" }
    foreach ($name in 'marvia-windows.exe', 'wintun.dll', 'WINTUN-LICENSE.txt', 'НАЧНИТЕ-ЗДЕСЬ.txt') {
        if (Test-Path -LiteralPath (Join-Path $installation $name)) { throw "после удаления остался $name" }
    }
    if ([IO.File]::ReadAllText($sentinel) -ne 'сохранить при обновлении и удалении') { throw 'удалён посторонний файл' }
    Write-Host 'Установка, обновление, удаление и сохранение чужих файлов проверены.'
} finally {
    $resolvedTest = [IO.Path]::GetFullPath($testRoot)
    $prefix = $workRoot.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if (-not $resolvedTest.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) { throw 'тестовая папка вне каталога проверки' }
    if (Test-Path -LiteralPath $resolvedTest) { Remove-Item -LiteralPath $resolvedTest -Recurse -Force }
}
