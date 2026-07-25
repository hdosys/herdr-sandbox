//go:build !windows

package sandbox

import "strings"

func expectedWindowsSandboxCommandLine(executable, configPath string) string {
	return escapeWindowsCommandLineArgument(executable) + " " + escapeWindowsCommandLineArgument(configPath)
}

func escapeWindowsCommandLineArgument(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n\v\"") {
		return value
	}
	var result strings.Builder
	result.WriteByte('"')
	backslashes := 0
	for _, character := range value {
		switch character {
		case '\\':
			backslashes++
		case '"':
			result.WriteString(strings.Repeat("\\", backslashes*2+1))
			result.WriteRune(character)
			backslashes = 0
		default:
			result.WriteString(strings.Repeat("\\", backslashes))
			backslashes = 0
			result.WriteRune(character)
		}
	}
	result.WriteString(strings.Repeat("\\", backslashes*2))
	result.WriteByte('"')
	return result.String()
}
