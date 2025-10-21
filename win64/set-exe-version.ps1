param(
    [Parameter(Mandatory=$true)]
    [string]$Version
)

$exePath = "./sse-broker.exe"
$iconPath = "../static/favicon.ico"
$rceditPath = "./rcedit-x64.exe"

if (!(Test-Path $exePath)) {
    Write-Error "Executable not found: $exePath"
    exit 1
}
if (!(Test-Path $iconPath)) {
    Write-Error "Icon not found: $iconPath"
    exit 1
}
if (!(Test-Path $rceditPath)) {
    Write-Error "rcedit not found: $rceditPath"
    exit 1
}

& $rceditPath $exePath `
    --set-icon $iconPath `
    --set-file-version $Version `
    --set-product-version $Version `
    --set-version-string "ProductName" "sse-broker" `
    --set-version-string "FileDescription" "https://github.com/sssxyd/go-sse-broker" `
    --set-version-string "LegalCopyright" "Apache 2.0"

if ($LASTEXITCODE -eq 0) {
    Write-Host "Successfully updated $exePath properties to version $Version."
} else {
    Write-Error "Failed to update $exePath properties."
}
