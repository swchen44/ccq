# ccq installer (Windows): build, place binary, install agent skill.
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
Write-Host "[ccq] building..."
go build -o ccq.exe ./cmd/ccq
$bin = "$env:LOCALAPPDATA\Programs\ccq"; New-Item -ItemType Directory -Force -Path $bin | Out-Null
Copy-Item ccq.exe "$bin\ccq.exe" -Force
Write-Host "[ccq] installed -> $bin\ccq.exe (add to PATH)"
$sk = "$env:USERPROFILE\.claude\skills\ccq"; New-Item -ItemType Directory -Force -Path $sk | Out-Null
Copy-Item SKILL.md "$sk\SKILL.md" -Force
Write-Host "[ccq] skill -> $sk\SKILL.md"
if (Get-Command clangd -ErrorAction SilentlyContinue) { Write-Host "[ccq] clangd found" }
else { Write-Host "[ccq] WARNING: clangd not found — install LLVM, or use --clangd <path>" }
