#Requires -Version 7

if ($IsWindows) {
  Set-Content -Path ENV:CPATH -Value $(Join-Path (${ENV:ProgramFiles(x86)} ?? $ENV:ProgramFiles) "WinFsp" "inc" "fuse")
}
