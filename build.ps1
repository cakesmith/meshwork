param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$GradleArgs
)

$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$GradleWrapper = Join-Path $RepoRoot "gradlew.bat"

$env:GRADLE_USER_HOME = Join-Path $RepoRoot ".gradle-user-home"

if (-not $GradleArgs -or $GradleArgs.Count -eq 0) {
    $GradleArgs = @("build", "--no-daemon")
}

if (-not (Test-Path $GradleWrapper)) {
    throw "Missing gradlew.bat in $RepoRoot"
}

& $GradleWrapper @GradleArgs
exit $LASTEXITCODE
