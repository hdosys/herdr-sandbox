$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
# The private installer consumes the same verified cache records as every other
# package. No release lookup or dependency download occurs on the target.
Install-ProvisioningMaterializedWinGet
