// Package productidentity owns the shared application and build identity.
package productidentity

// Machine-facing paths use ApplicationName. Windows-facing labels and the
// install directory use DisplayName.
const (
	ApplicationName          = "herdr-sandbox"
	CommandName              = "sandbox"
	DisplayName              = "Herdr Sandbox"
	ExecutableName           = CommandName + ".exe"
	BaseScriptName           = "base.ps1"
	StackScriptName          = "stacks.ps1"
	LicenseName              = "LICENSE.txt"
	LicenseSourceName        = "LICENSE"
	ConfigurationName        = "config.json"
	SampleConfigurationName  = "config.sample.json"
	ConfigurationSchemaName  = "config.schema.json"
	UserScriptName           = "user.ps1"
	ProjectDirectoryName     = ".herdr-sandbox"
	ProjectScriptName        = "provision.ps1"
	InstallDirectoryName     = DisplayName
	Publisher                = "hdosys"
	ProductURL               = "https://github.com/hdosys/herdr-sandbox"
	ProductGUID              = "bf6ef455-61af-43bd-8a25-521c6c7e13f9"
	UninstallKeyName         = "{BF6EF455-61AF-43BD-8A25-521C6C7E13F9}"
	QuietUninstallHelperName = "uninstall.ps1"
	Copyright                = "Copyright (c) 2026 hdosys"
	LifecycleMutexName       = `Global\` + ApplicationName + `-lifecycle-v1`
)
