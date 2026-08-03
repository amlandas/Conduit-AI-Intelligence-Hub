#Requires -Version 5.1
<#
.SYNOPSIS
    Conduit 2.0 does not support Windows yet.

.DESCRIPTION
    This script used to install Conduit 1.x on Windows. Conduit 2.0 is a
    rebuild, and Windows is not part of it yet.

    This is a deliberate scope decision, not an oversight. Supporting Windows
    properly means more than compiling the binary:

      * llama-server process supervision. The embedding sidecar is managed with
        POSIX process groups and flock (see internal/embed/sysutil_unix.go).
        The Windows half of that file is a stub and has never been exercised.
      * Path and permission conventions. Data directories, executable
        discovery and config locations all differ.
      * Verified installs. There is no signed Windows artifact, and shipping an
        unsigned executable that downloads a few hundred megabytes of model
        weights is not something to do casually.

    None of this is hard. It is simply not done, and claiming otherwise by
    leaving a plausible-looking installer here would waste your time before
    telling you the same thing.

.NOTES
    WSL2 works today. Conduit runs under Linux in WSL2 with no changes:

        wsl --install                     # if you do not already have it
        # then, inside the WSL2 shell:
        git clone https://github.com/amlandas/Conduit-AI-Intelligence-Hub
        cd Conduit-AI-Intelligence-Hub
        ./scripts/install.sh --from-source

    An AI client running on Windows can reach an MCP server running in WSL2,
    so this is a working setup rather than a consolation prize.

    Native Windows support is planned. Track it on the repository issues.

.LINK
    https://github.com/amlandas/Conduit-AI-Intelligence-Hub
#>

[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

Write-Host ''
Write-Host 'Conduit 2.0 does not support Windows yet.' -ForegroundColor Yellow
Write-Host ''
Write-Host 'Conduit 2.0 is a rebuild of Conduit 1.x. Windows support has not been'
Write-Host 'ported. Rather than install something that would fail later, this script'
Write-Host 'stops here.'
Write-Host ''
Write-Host 'What works today: WSL2.' -ForegroundColor Green
Write-Host ''
Write-Host '  1. Install WSL2, if you do not already have it:'
Write-Host '       wsl --install'
Write-Host ''
Write-Host '  2. Inside the WSL2 shell:'
Write-Host '       git clone https://github.com/amlandas/Conduit-AI-Intelligence-Hub'
Write-Host '       cd Conduit-AI-Intelligence-Hub'
Write-Host '       ./scripts/install.sh --from-source'
Write-Host ''
Write-Host 'An AI client running on Windows can talk to an MCP server running in'
Write-Host 'WSL2, so this is a working setup, not a workaround.'
Write-Host ''
Write-Host 'Native Windows support is planned but not scheduled.'
Write-Host 'See https://github.com/amlandas/Conduit-AI-Intelligence-Hub for status.'
Write-Host ''

# Non-zero, so an automated caller treats this as "did not install" rather than
# as a successful no-op.
exit 1
