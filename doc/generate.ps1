$moduleName = "go-fs-documentation"
$modulePath = Join-Path . "$($moduleName).psm1"

Import-Module -Name $modulePath
New-GoFSDocumentation
Remove-Module -Name $moduleName
