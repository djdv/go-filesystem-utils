[CmdletBinding(PositionalBinding = $false)]
param (
	[Parameter(Mandatory = $false)]
	[string]$pkgPath = $(Join-Path "." "cmd" "fs"),

    [parameter(ValueFromRemainingArguments = $true)]$userArgs
)

$port = 6066
$location = "127.0.0.1`:$port" # :$port"
try { $dlv = Get-Command -Name "dlv" -CommandType Application -ErrorAction Stop }
catch { Write-Host "``dlv`` not found, but is required"; return}
$dlvArgs = "debug","$pkgPath","--listen=$location","--headless","--disable-aslr"

if ($userArgs.Length -ne 0) {
	$userArgs = "--", [string]$userArgs
}

try { $gdlv = Get-Command -Name "gdlv" -CommandType Application -ErrorAction Stop }
catch { $gdlv = $dlv }
$gdlvArgs = "connect","$location"

$fullArgs = $dlvArgs + $userArgs
Write-Host "Spawning dlv single instance server at: $location`nWith args: ``$fullArgs``"
$dlvProc = Start-Process -FilePath $dlv -ArgumentList $($dlvArgs+$userArgs) -NoNewWindow

for ($attempts=0; $attempts -ne 10; $attempts++) {
  try {
	Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction Stop | Out-Null
	Write-Host "connecting to $location"
	Start-Process -FilePath $gdlv -ArgumentList $gdlvArgs -NoNewWindow
	return
  }
  catch {
	Write-Host "Debug server not responding, still compiling? Waiting to retry..."
	Start-Sleep 1
  }
}

Write-Host "Server never responded, giving up"
if ($dlvProc -ne $null) {
	$dlvProc.Kill()
}
return
