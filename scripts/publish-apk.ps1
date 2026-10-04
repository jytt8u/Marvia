<#
    Сборка и выкладка приложения для Android.

    Запасной путь. Обычно APK собирает и подписывает сама сборка выпуска на
    GitHub (.github/workflows/release.yml), когда ключ лежит в секретах
    окружения release — см. docs/guide.md. Этот скрипт нужен, если секретов нет
    или сборка APK на GitHub не удалась: он собирает тем же ключом с диска.
    Ключ у приложения ровно один навсегда — потеряешь или подменишь, и
    обновление не встанет ни у одного покупателя, придётся ставить заново.

    Код обновления по умолчанию берётся из android/version-code — того же
    файла, что читает сборка на GitHub.

    Пример:
        .\scripts\publish-apk.ps1 -Tag v0.13.0-alpha.2
#>
[CmdletBinding()]
param(
    # Метка релиза, к которому прикладываем файл.
    [Parameter(Mandatory = $true)]
    [string]$Tag,

    # Репозиторий сборок. Открытый: покупатель качает без токенов и логинов.
    [string]$Repo = 'jytt8u/marvia',

    # Код Android растёт даже при возврате номера версии к alpha после 1.x.
    [ValidateRange(0, 2100000000)]
    [int]$VersionCode = 0,

    # Пропустить пересборку ядра на Go — она долгая и нужна, только когда
    # менялся Go, а не Kotlin.
    [switch]$SkipCore
)

$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..')

if ($Tag -notmatch '^v0\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$') {
    throw 'выпуск 1.x отложен; разрешены только тестовые версии v0.x'
}
if ($VersionCode -eq 0) {
    $VersionCode = [int]((Get-Content 'android\version-code' -Raw).Trim())
}
if ($Tag.Contains('-') -and $VersionCode -lt 10002) {
    throw 'для alpha после 1.0.1 задайте -VersionCode больше 10001; каждый следующий APK требует ещё больший код'
}

# Пока выпуск отложен, этот скрипт не изменяет публичные файлы. Сначала
# владелец проверяет конкретный кандидат и публикует готовый черновик отдельно.
$releaseState = gh release view $Tag --repo $Repo --json isDraft
if ($LASTEXITCODE -ne 0) { throw 'черновик релиза не найден' }
if (-not ($releaseState | ConvertFrom-Json).isDraft) {
    throw 'APK можно прикладывать только к черновику тестового релиза'
}

if (-not $env:ANDROID_HOME) { $env:ANDROID_HOME = 'D:\android-sdk' }
if (-not $env:MARVIA_RELEASE_KEYS) { $env:MARVIA_RELEASE_KEYS = 'D:\veil-keys\veil-release.properties' }

# Версия приложения — из метки: одна строка на apk, aab и файл .version на
# панели, по которому покупатель узнаёт про обновление.
$env:MARVIA_VERSION = $Tag -replace '^v', ''
if ($VersionCode -gt 0) {
    $env:MARVIA_VERSION_CODE = [string]$VersionCode
} else {
    Remove-Item Env:MARVIA_VERSION_CODE -ErrorAction SilentlyContinue
}

if (-not (Test-Path $env:MARVIA_RELEASE_KEYS)) {
    throw "нет ключа подписи: $env:MARVIA_RELEASE_KEYS. Неподписанный APK не поставится."
}

if (-not $SkipCore) {
    $env:PATH = "$env:PATH;$(go env GOPATH)\bin"
    if (-not $env:ANDROID_NDK_HOME) {
        $ndk = Get-ChildItem (Join-Path $env:ANDROID_HOME 'ndk') -Directory |
            Sort-Object Name | Select-Object -Last 1
        if (-not $ndk) { throw "не нашёл NDK в $env:ANDROID_HOME\ndk" }
        $env:ANDROID_NDK_HOME = $ndk.FullName
    }

    Write-Host '== ядро -> android/app/libs/marvia.aar'
    gomobile bind '-target=android/arm64,android/arm' '-androidapi' '24' '-trimpath' '-ldflags=-s -w' `
        '-javapkg=io.marvia' '-o' 'android/app/libs/marvia.aar' './mobile'
    if ($LASTEXITCODE -ne 0) { throw 'не собралась библиотека ядра' }
}

Write-Host '== приложение'
Push-Location android
try {
    # APK — для панели продавца, AAB — для Google Play: тот принимает только
    # bundle. Подпись одна и та же: Play App Signing получает наш ключ, а не
    # заводит свой, иначе apk с панели и приложение из Play телефон счёл бы
    # разными программами.
    .\gradlew.bat assembleRelease bundleRelease --console=plain
    if ($LASTEXITCODE -ne 0) { throw 'не собралось приложение' }
}
finally {
    Pop-Location
}

$apk = 'android\app\build\outputs\apk\release\app-release.apk'
$out = Join-Path ([System.IO.Path]::GetTempPath()) 'marvia-android.apk'
Copy-Item $apk $out -Force

# Подпись проверяем до выкладки: неподписанный или подписанный отладочным
# ключом APK встанет только поверх такого же, а у покупателей стоит боевой.
$signature = & "$env:ANDROID_HOME\build-tools\36.0.0\apksigner.bat" verify --print-certs $out
if ($LASTEXITCODE -ne 0) { throw 'APK не подписан' }
Write-Host $signature[1]

$sum = (Get-FileHash $out -Algorithm SHA256).Hash.ToLower()
"$sum  marvia-android.apk" | Out-File -FilePath "$out.sha256" -Encoding ascii -NoNewline

$aab = 'android\app\build\outputs\bundle\release\app-release.aab'
$outAab = Join-Path ([System.IO.Path]::GetTempPath()) 'marvia-android.aab'
Copy-Item $aab $outAab -Force

Write-Host "== выкладываю в $Repo, метка $Tag"
gh release upload $Tag $out "$out.sha256" $outAab --repo $Repo --clobber
if ($LASTEXITCODE -ne 0) { throw 'не выложилось' }

Write-Host ''
Write-Host "готово: marvia-android.apk $env:MARVIA_VERSION, sha256 $sum; marvia-android.aab — для Play"
