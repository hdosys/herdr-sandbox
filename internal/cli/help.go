package cli

func scopedUsage(command string) string {
	switch command {
	case "up":
		return "Usage: sandbox up [--project PATH] [--stack NAME]... [--remote-access] [--yes] [--memory-mb MB] [--timeout DURATION] [--no-attach]\nFirst use guides project/tool selection. Existing profiles are never overwritten.\nInitial setup resumes; an already usable environment opens without updates, even after a failed refresh.\nFor intentional installer testing: sandbox up --provisioning installer --installer FILE --no-attach\n"
	case "refresh":
		return "Usage: sandbox refresh [--project PATH] [--timeout DURATION]\nExplicitly update the running environment without attaching. Reuse successful unchanged steps.\nA changed fixed mapping requires an explicit new startup; refresh never silently discards the guest.\n"
	case "remote":
		return remoteUsage
	case "status":
		return "Usage: sandbox status [--verbose]\nRead-only health and setup result. A usable guest may have a failed update. Exit 0 only when ready.\n"
	case "down":
		return "Usage: sandbox down [--yes]\nDiscard the exact managed guest after explicit confirmation. Guest-only data is lost.\nMapped host-file edits and permanently saved device keys remain; host edits are not rolled back.\n"
	case "attach":
		return "Usage: sandbox attach\nVerify and open the existing ready environment without any setup or update.\n"
	case "init":
		return "Usage: sandbox init [--stack NAME]...\nCreate a project profile without replacing an existing one. Normal first use can use sandbox up instead.\n"
	case "plan":
		return "Usage: sandbox plan\nOptional read-only preview of configured folders, packages and fixed-launch differences.\n"
	case "config":
		return "Usage: sandbox config\nOpen the host configuration for advanced settings. Existing files are preserved.\n"
	case "clean":
		return "Usage: sandbox clean\nRemove only proven inactive app-owned run state, never the running guest or uncertain state.\n"
	case "pull-host-config":
		return "Usage: sandbox pull-host-config\nFast-forward configured Git-backed host configuration roots; preserve dirty or divergent work.\n"
	case "workstation":
		return workstationUsage
	case "version", "--version":
		return "Usage: sandbox version\nShow this application's version, build freshness and source revision.\n"
	default:
		return usage
	}
}
