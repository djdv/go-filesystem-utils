Param (
  [Switch] $openHTMLReport,
  [String] $tags,
  [String] $coverPkg = "./...", # NOTE: coverpkg uses Go import path notation, not file system path notation.
  [String] $coverTarget = $(Join-Path . ...),
  $goBin = $(Get-Command -Name go -CommandType Application -TotalCount 1 -ErrorAction SilentlyContinue -ErrorVariable goBinError)
)

# Handle environment.
if ($goBinError) {
  "Go binary not found:"
  $goBinError | ForEach-Object {
    $_ | Write-Host
  }
  exit 1
}


# Setup temporary cover file
$coverFile = New-TemporaryFile

# Setup test command line.
$goTestArgs = "test", "-v", "-covermode=count", "-coverpkg=`"$coverPkg`"", "-coverprofile=`"$($coverFile.FullName)`"", "`"$coverTarget`""
if (-not [string]::IsNullOrWhiteSpace($tags)) {
  $goTestArgs += "-tags=`"$tags`""
}

# Call the tests.
& $goBin $goTestArgs
if ($LASTEXITCODE) {
  Remove-Item $coverFile
  exit $LASTEXITCODE
}

# Print coverage.
$goCoverArgs = "tool", "cover", "-func=`"$($coverFile.FullName)`""
& $goBin $goCoverArgs

# Generate HTML report.
$coverHTMLFile = Join-Path $([System.IO.Path]::GetTempPath()) "cover.html"
if ((Test-Path $coverHTMLFile)) {
  $coverHTMLFile = Get-ItemProperty $coverHTMLFile
} else {
  $coverHTMLFile = New-TemporaryFile | Rename-Item -NewName "cover.html" -PassThru
}
$goHTMLArgs = "tool", "cover", "-html=`"$($coverFile.FullName)`"", "-o=`"$($coverHTMLFile.FullName)`""
& $goBin $goHTMLArgs

# Cleanup.
Remove-Item $coverFile

# Maybe open to report.
if ($openHTMLReport) {
  Invoke-Item $coverHTMLFile
}
