function Get-GoFSDocumentationCommandTable {
	$dependencies = @{
		'go'   = 'https://go.dev/'
		'goda' = 'https://github.com/loov/goda'
		'dot'  = 'https://graphviz.org/'
		'd2'   = 'https://github.com/terrastruct/d2'
	}
	$commands = @{}
	foreach ($application in $dependencies.GetEnumerator()) {
		$name = $application.Name
		try {
			$command = Get-Command -Name $name -CommandType Application -ErrorAction Stop
		} catch {
			throw "Required command ``$name`` was not found. See: $($dependencies[$name])"
		}
		$commands[$name] = $command
	}
	return $commands
}

function New-GoFSDocumentation {
	[CmdletBinding(SupportsShouldProcess)]
	param()

	$commands = Get-GoFSDocumentationCommandTable
	$resultDirectory = Join-Path . assets
	[void](New-Item -ItemType Directory -ErrorAction Ignore -Path $resultDirectory)

	# Include all of our modules, but exclude the build tool.
	# Note: Module separator '/', not host separator.
	$godaExpression = '../... - ../cmd/build'
	$dependencyGraph = 'goda-short.svg'
	$dependencyGraphPath = $(Join-Path $resultDirectory $dependencyGraph)
	if ($PSCmdlet.ShouldProcess($dependencyGraphPath, 'Generate dependency graph')) {
		Write-Information "Generating graph: `"$($dependencyGraphPath)`""
		& $commands['goda'] graph -short -f='{{.ID}}' $godaExpression | & $commands['dot'] -Tsvg -o $dependencyGraphPath
	}

	$verbose = $InformationPreference -eq [System.Management.Automation.ActionPreference]::SilentlyContinue
	Get-ChildItem -Path $(Join-Path graphs *) -Include *.d2 | ForEach-Object {
		$vectorName = "$($_.BaseName).svg"
		$vectorPath = Join-Path $resultDirectory $vectorName
		if (!$PSCmdlet.ShouldProcess($vectorPath, 'Generate vector')) {
			return
		}
		Write-Information "Generating graph: `"$vectorPath`""
		# d2 uses stderr for all logging.
		# Redirect it and only print it conditionally.
		$startInfo = [System.Diagnostics.ProcessStartInfo]::new(
			$commands['d2'].Source,
			"`"$($_.FullName)`" `"$($vectorPath)`""
		)
		$startInfo.RedirectStandardError = $true
		$process = [System.Diagnostics.Process]::Start($startInfo)
		$stderr = $process.StandardError.ReadToEnd()
		$process.WaitForExit()
		if ($process.ExitCode) {
			$sourceRelative = Resolve-Path -Relative -Path $_.FullName
			Write-Error "`"$($sourceRelative)`"` -> `"$($vectorPath)`" $stderr"
		} elseif ($verbose) {
			Write-Information $stderr
		}
	}
}
