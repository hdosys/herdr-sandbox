package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	agentGitInspectionTimeout = 30 * time.Second
	maximumAgentGitOutput     = 4 * 1024 * 1024
)

var excludedAgentGitMetadataDirectories = map[string]bool{
	"hooks":    true,
	"lfs":      true,
	"logs":     true,
	"rr-cache": true,
	"svn":      true,
}

var rejectedAgentGitMetadataDirectories = map[string]bool{
	"modules":      true,
	"rebase-apply": true,
	"rebase-merge": true,
	"sequencer":    true,
	"worktrees":    true,
}

var excludedAgentGitMetadataFiles = map[string]bool{
	"commit_editmsg": true,
	"fetch_head":     true,
	"orig_head":      true,
	"squash_msg":     true,
}

var rejectedAgentGitMetadataFiles = map[string]bool{
	"auto_merge":       true,
	"bisect_log":       true,
	"bisect_start":     true,
	"cherry_pick_head": true,
	"commondir":        true,
	"config.worktree":  true,
	"gitdir":           true,
	"merge_head":       true,
	"revert_head":      true,
}

func archiveAgentGitRepository(ctx context.Context, directory, archiveRoot string, add func(string, string) error) ([]string, error) {
	if directory == "" {
		return nil, nil
	}
	gitDirectory := filepath.Join(directory, ".git")
	info, err := os.Lstat(gitDirectory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect agent Git directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("agent configuration Git metadata must be one physical .git directory: %s", gitDirectory)
	}
	gitExecutable, err := exec.LookPath("git.exe")
	if err != nil {
		gitExecutable, err = exec.LookPath("git")
	}
	if err != nil {
		return nil, errors.New("Git is required to inspect an agent configuration repository")
	}

	inspectionContext, cancel := context.WithTimeout(ctx, agentGitInspectionTimeout)
	defer cancel()
	if err := validateAgentGitRepository(inspectionContext, gitExecutable, directory, gitDirectory); err != nil {
		return nil, err
	}
	tracked, err := listAgentGitTrackedFiles(inspectionContext, gitExecutable, directory)
	if err != nil {
		return nil, err
	}
	deleted := []string{}
	for _, relative := range tracked {
		if reason := blockedAgentGitTrackedPath(archiveRoot, relative); reason != "" {
			return nil, fmt.Errorf("agent configuration repository tracks excluded %s: %s", reason, relative)
		}
		source := filepath.Join(directory, relative)
		info, err := os.Lstat(source)
		if errors.Is(err, os.ErrNotExist) {
			deleted = append(deleted, filepath.ToSlash(relative))
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect tracked agent configuration %s: %w", relative, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("tracked agent configuration is not a regular file: %s", relative)
		}
		if err := add(source, filepath.Join(archiveRoot, relative)); err != nil {
			return nil, fmt.Errorf("archive tracked agent configuration %s: %w", relative, err)
		}
	}
	if err := archiveAgentGitMetadata(gitDirectory, filepath.Join(archiveRoot, ".git"), add); err != nil {
		return nil, err
	}
	return deleted, nil
}

func validateAgentGitRepository(ctx context.Context, gitExecutable, directory, gitDirectory string) error {
	output, err := runAgentGit(ctx, gitExecutable, directory, 32*1024, "validate agent configuration repository",
		"rev-parse", "--path-format=absolute", "--show-toplevel", "--absolute-git-dir", "--git-common-dir", "--is-inside-work-tree", "--is-bare-repository", "--show-ref-format")
	if err != nil {
		return err
	}
	lines := splitAgentGitLines(output)
	if len(lines) != 6 || !sameAgentGitPath(lines[0], directory) || !sameAgentGitPath(lines[1], gitDirectory) ||
		!sameAgentGitPath(lines[2], gitDirectory) || lines[3] != "true" || lines[4] != "false" || lines[5] != "files" {
		return fmt.Errorf("agent configuration repository does not use its physical root .git directory: %s", directory)
	}
	return nil
}

func listAgentGitTrackedFiles(ctx context.Context, gitExecutable, directory string) ([]string, error) {
	output, err := runAgentGit(ctx, gitExecutable, directory, maximumAgentGitOutput, "enumerate tracked agent configuration",
		"ls-files", "--cached", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	records := bytes.Split(output, []byte{0})
	tracked := make([]string, 0, len(records))
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		if len(record) == 0 {
			continue
		}
		separator := bytes.IndexByte(record, '\t')
		if separator < 0 || !utf8.Valid(record[separator+1:]) {
			return nil, errors.New("Git returned an invalid tracked agent configuration entry")
		}
		fields := bytes.Fields(record[:separator])
		if len(fields) != 3 || string(fields[2]) != "0" {
			return nil, errors.New("agent configuration repository has an unresolved index entry")
		}
		mode := string(fields[0])
		if mode != "100644" && mode != "100755" {
			return nil, fmt.Errorf("agent configuration repository tracks unsupported mode %s", mode)
		}
		relative, err := cleanAgentGitRelativePath(string(record[separator+1:]))
		if err != nil {
			return nil, err
		}
		identity := strings.ToLower(filepath.ToSlash(relative))
		if seen[identity] {
			return nil, fmt.Errorf("agent configuration repository contains a case-colliding tracked path: %s", relative)
		}
		seen[identity] = true
		tracked = append(tracked, relative)
		if len(tracked) > maximumConfigurationFiles {
			return nil, fmt.Errorf("agent configuration repository exceeds tracked-file limit %d", maximumConfigurationFiles)
		}
	}
	return tracked, nil
}

func cleanAgentGitRelativePath(value string) (string, error) {
	if value == "" || strings.Contains(value, `\`) || strings.Contains(value, ":") {
		return "", fmt.Errorf("agent configuration repository returned an unsafe tracked path: %q", value)
	}
	relative := filepath.FromSlash(value)
	cleaned := filepath.Clean(relative)
	if filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" || cleaned == "." || cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) || filepath.ToSlash(cleaned) != value {
		return "", fmt.Errorf("agent configuration repository returned an unsafe tracked path: %q", value)
	}
	return cleaned, nil
}

func blockedAgentGitTrackedPath(archiveRoot, relative string) string {
	normalized := strings.ToLower(filepath.ToSlash(relative))
	for _, exact := range []string{
		".claude.json", ".credentials.json", ".env", ".env.local", "auth.json",
		"credentials.json", "history.json", "history.jsonl", "secrets.json",
	} {
		if normalized == exact {
			return "credential or runtime file"
		}
	}
	for _, prefix := range []string{"cache", "caches", "log", "logs", "projects", "session-state", "sessions", "temp", "tmp"} {
		if normalized == prefix || strings.HasPrefix(normalized, prefix+"/") {
			return "runtime state"
		}
	}
	if archiveRoot == "github-copilot" && normalized == "config.json" {
		return "credential or runtime file"
	}
	if archiveRoot == "codex" && (normalized == "skills/.system" || strings.HasPrefix(normalized, "skills/.system/")) {
		return "generated system skill"
	}
	for _, component := range strings.Split(normalized, "/") {
		if component == "node_modules" {
			return "generated package state"
		}
	}
	name := filepath.Base(normalized)
	for _, suffix := range []string{".key", ".log", ".p12", ".pem", ".pfx"} {
		if strings.HasSuffix(name, suffix) {
			return "credential or log file"
		}
	}
	return ""
}

func archiveAgentGitMetadata(directory, archiveRoot string, add func(string, string) error) error {
	return filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("agent Git metadata contains a symbolic link: %s", path)
		}
		if path == directory {
			return nil
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		normalized := strings.ToLower(filepath.ToSlash(relative))
		if entry.IsDir() {
			if !strings.Contains(normalized, "/") && excludedAgentGitMetadataDirectories[normalized] {
				return filepath.SkipDir
			}
			if !strings.Contains(normalized, "/") && rejectedAgentGitMetadataDirectories[normalized] {
				return fmt.Errorf("agent Git metadata contains unsupported or active directory: %s", relative)
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("agent Git metadata is not a regular file: %s", relative)
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".lock") {
			return fmt.Errorf("agent Git metadata is locked; retry after the Git operation finishes: %s", relative)
		}
		if !strings.Contains(normalized, "/") && strings.HasSuffix(name, ".log") {
			return nil
		}
		if !strings.Contains(normalized, "/") && strings.HasSuffix(name, ".pid") {
			return fmt.Errorf("agent Git metadata contains an active process marker: %s", relative)
		}
		if !strings.Contains(normalized, "/") && name == "packed-refs.new" {
			return fmt.Errorf("agent Git metadata contains a temporary refs file: %s", relative)
		}
		if !strings.Contains(normalized, "/") && rejectedAgentGitMetadataFiles[name] {
			return fmt.Errorf("agent Git metadata contains unsupported or active state: %s", relative)
		}
		if normalized == "objects/info/alternates" || normalized == "objects/info/http-alternates" {
			return fmt.Errorf("agent Git metadata uses an external object store: %s", relative)
		}
		if !strings.Contains(normalized, "/") && excludedAgentGitMetadataFiles[name] {
			return nil
		}
		if strings.HasPrefix(normalized, "objects/pack/tmp_") {
			return fmt.Errorf("agent Git metadata contains a temporary object pack: %s", relative)
		}
		if err := add(path, filepath.Join(archiveRoot, relative)); err != nil {
			return fmt.Errorf("archive agent Git metadata %s: %w", relative, err)
		}
		return nil
	})
}

func runAgentGit(ctx context.Context, executable, directory string, maximumOutput int, role string, arguments ...string) ([]byte, error) {
	commandArguments := []string{"-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-C", directory}
	commandArguments = append(commandArguments, arguments...)
	command := hiddenCommandContext(ctx, executable, commandArguments...)
	command.Env = agentGitEnvironment(command.Env)
	stdout := boundedCommandOutput{maximum: maximumOutput}
	stderr := boundedCommandOutput{maximum: 4096}
	defer stdout.clear()
	defer stderr.clear()
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s: %w", role, ctx.Err())
		}
		return nil, fmt.Errorf("%s: %w: %s", role, err, stderr.text())
	}
	if stdout.overflow {
		return nil, fmt.Errorf("%s exceeded the %d-byte output limit", role, maximumOutput)
	}
	return append([]byte(nil), stdout.buffer.Bytes()...), nil
}

func agentGitEnvironment(parent []string) []string {
	environment := make([]string, 0, len(parent)+4)
	for _, entry := range parent {
		name, _, found := strings.Cut(entry, "=")
		if found && strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment,
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
	)
}

func splitAgentGitLines(output []byte) []string {
	text := strings.ReplaceAll(string(output), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func sameAgentGitPath(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}
