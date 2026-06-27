# ccq installer (Windows): place binary, install agent skill.
# Works from a source checkout (builds) OR a release archive (prebuilt ccq.exe).
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
if (-not (Test-Path ./ccq.exe)) {
  Write-Host "[ccq] building..."
  go build -o ccq.exe ./cmd/ccq
} else { Write-Host "[ccq] using prebuilt binary" }
$bin = "$env:LOCALAPPDATA\Programs\ccq"; New-Item -ItemType Directory -Force -Path $bin | Out-Null
Copy-Item ccq.exe "$bin\ccq.exe" -Force
Write-Host "[ccq] installed -> $bin\ccq.exe (add to PATH)"
if (Test-Path ./clangd.exe) {
  Copy-Item ./clangd.exe "$bin\clangd.exe" -Force
  Write-Host "[ccq] bundled clangd -> $bin\clangd.exe (ccq auto-finds it next to itself)"
}
$sk = "$env:USERPROFILE\.claude\skills\ccq"; New-Item -ItemType Directory -Force -Path $sk | Out-Null
Copy-Item SKILL.md "$sk\SKILL.md" -Force
Write-Host "[ccq] skill -> $sk\SKILL.md"
if (Get-Command clangd -ErrorAction SilentlyContinue) { Write-Host "[ccq] clangd found" }
else { Write-Host "[ccq] WARNING: clangd not found — install LLVM, or use --clangd <path>" }
