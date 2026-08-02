package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type installerCleanPaths struct {
	DataDirectory          string
	ConfigurationDirectory string
	CacheDirectory         string
	DefaultCacheParent     string
	InstallDirectory       string
	UserHome               string
	Configuration          globalConfiguration
}

type installerDirectoryRemovalPlan struct {
	Role       string
	Path       string
	Name       string
	Root       *os.Root
	EntryInfo  os.FileInfo
	RootExists bool
}

func (plan *installerDirectoryRemovalPlan) close() {
	if plan != nil && plan.Root != nil {
		_ = plan.Root.Close()
		plan.Root = nil
	}
}

type installerSSHRemovalPlan struct {
	Path     string
	Info     os.FileInfo
	Existing []byte
	Updated  []byte
	Mode     os.FileMode
	Changed  bool
}

// CleanInstallerData is the uninstall owner. It always removes app-owned
// machine-local state and removes user configuration only when explicitly
// selected. Project profiles are never traversed or deleted.
func CleanInstallerData(ctx context.Context, deleteConfiguration bool) error {
	release, err := acquireLifecycleLock(ctx)
	if err != nil {
		return err
	}
	paths, resolveErr := resolveInstallerCleanPaths()
	var cleanErr error
	if resolveErr == nil {
		cleanErr = cleanInstallerDataAt(ctx, paths, deleteConfiguration)
	}
	releaseErr := release()
	if resolveErr != nil {
		return resolveErr
	}
	if cleanErr != nil {
		return cleanErr
	}
	return releaseErr
}

func resolveInstallerCleanPaths() (installerCleanPaths, error) {
	dataDirectory, err := defaultDataDirectory()
	if err != nil {
		return installerCleanPaths{}, err
	}
	configurationRoot, err := os.UserConfigDir()
	if err != nil {
		return installerCleanPaths{}, fmt.Errorf("resolve user configuration directory: %w", err)
	}
	if !filepath.IsAbs(configurationRoot) {
		return installerCleanPaths{}, fmt.Errorf("user configuration directory is not absolute: %q", configurationRoot)
	}
	configurationDirectory := filepath.Join(filepath.Clean(configurationRoot), applicationName)
	configuration, err := loadGlobalConfiguration(filepath.Join(configurationDirectory, globalConfigurationName))
	if err != nil {
		return installerCleanPaths{}, err
	}
	configuredCache, err := validateConfiguredCacheDirectory(configuration.CacheDirectory)
	if err != nil {
		return installerCleanPaths{}, err
	}
	cacheDirectory, err := effectiveCacheDirectory(configuredCache)
	if err != nil {
		return installerCleanPaths{}, err
	}
	cacheConfigured := configuredCache != ""
	defaultCacheParent := ""
	if !cacheConfigured {
		if filepath.Base(cacheDirectory) != "cache" || filepath.Base(filepath.Dir(cacheDirectory)) != applicationName {
			return installerCleanPaths{}, fmt.Errorf("default cache directory has an unexpected shape: %s", cacheDirectory)
		}
		defaultCacheParent = filepath.Dir(cacheDirectory)
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return installerCleanPaths{}, fmt.Errorf("resolve user home for uninstall: %w", err)
	}
	if !filepath.IsAbs(userHome) {
		return installerCleanPaths{}, fmt.Errorf("user home for uninstall is not absolute: %q", userHome)
	}
	executable, err := os.Executable()
	if err != nil {
		return installerCleanPaths{}, fmt.Errorf("resolve installed executable for uninstall: %w", err)
	}
	if !filepath.IsAbs(executable) {
		return installerCleanPaths{}, fmt.Errorf("installed executable is not absolute: %q", executable)
	}
	paths := installerCleanPaths{
		DataDirectory:          filepath.Clean(dataDirectory),
		ConfigurationDirectory: configurationDirectory,
		CacheDirectory:         filepath.Clean(cacheDirectory),
		DefaultCacheParent:     defaultCacheParent,
		InstallDirectory:       filepath.Dir(filepath.Clean(executable)),
		UserHome:               filepath.Clean(userHome),
		Configuration:          configuration,
	}
	if err := validateInstallerCacheRemoval(paths); err != nil {
		return installerCleanPaths{}, err
	}
	return paths, nil
}

func validateInstallerCacheRemoval(paths installerCleanPaths) error {
	if !filepath.IsAbs(paths.CacheDirectory) {
		return fmt.Errorf("uninstall cache directory is not absolute: %q", paths.CacheDirectory)
	}
	for role, protected := range map[string]string{
		"install directory":        paths.InstallDirectory,
		"user SSH directory":       filepath.Join(paths.UserHome, ".ssh"),
		"machine-local state root": paths.DataDirectory,
		"user configuration root":  paths.ConfigurationDirectory,
	} {
		if hostPathsOverlap(paths.CacheDirectory, protected) {
			return fmt.Errorf("configured cache directory overlaps the %s and cannot be removed safely: %s", role, paths.CacheDirectory)
		}
	}
	for role, protected := range map[string]string{
		"Windows directory":     os.Getenv("WINDIR"),
		"Program Files":         os.Getenv("ProgramFiles"),
		"32-bit Program Files":  os.Getenv("ProgramFiles(x86)"),
		"ProgramData directory": os.Getenv("ProgramData"),
	} {
		protected = strings.TrimSpace(protected)
		if protected != "" && filepath.IsAbs(protected) && hostPathsOverlap(paths.CacheDirectory, filepath.Clean(protected)) {
			return fmt.Errorf("configured cache directory overlaps the %s and cannot be removed safely: %s", role, paths.CacheDirectory)
		}
	}
	for name, workspace := range paths.Configuration.Workspaces {
		workspace = strings.TrimSpace(workspace)
		if workspace == "" || !filepath.IsAbs(workspace) {
			return fmt.Errorf("configured workspace %q is not an absolute path; refusing clean uninstall", name)
		}
		if hostPathsOverlap(paths.CacheDirectory, filepath.Clean(workspace)) {
			return fmt.Errorf("configured cache directory overlaps workspace %q and cannot be removed safely: %s", name, paths.CacheDirectory)
		}
	}
	if discovery := paths.Configuration.WorkspaceDiscovery; discovery != nil {
		root := strings.TrimSpace(discovery.Root)
		if root != "" {
			if !filepath.IsAbs(root) {
				return errors.New("workspaceDiscovery.root is not absolute; refusing clean uninstall")
			}
			if hostPathsOverlap(paths.CacheDirectory, filepath.Clean(root)) {
				return fmt.Errorf("configured cache directory overlaps workspaceDiscovery.root and cannot be removed safely: %s", paths.CacheDirectory)
			}
		}
	}
	return nil
}

func cleanInstallerDataAt(ctx context.Context, paths installerCleanPaths, deleteConfiguration bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sshPlan, err := planInstallerSSHRemoval(paths.UserHome, paths.DataDirectory)
	if err != nil {
		return err
	}
	removalRoots := installerRemovalRoots(paths, deleteConfiguration)
	plans := make([]*installerDirectoryRemovalPlan, 0, len(removalRoots))
	defer func() {
		for _, plan := range plans {
			plan.close()
		}
	}()
	for _, root := range removalRoots {
		plan, err := planInstallerDirectoryRemoval(root.path, root.role)
		if err != nil {
			return err
		}
		plans = append(plans, plan)
	}
	if err := applyInstallerSSHRemoval(sshPlan); err != nil {
		return err
	}
	for _, plan := range plans {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := applyInstallerDirectoryRemoval(plan); err != nil {
			return err
		}
	}
	for _, plan := range plans {
		plan.close()
	}
	plans = nil
	if paths.DefaultCacheParent != "" {
		if err := removeEmptyInstallerCacheParent(paths.DefaultCacheParent); err != nil {
			return err
		}
	}
	return nil
}

func removeEmptyInstallerCacheParent(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("default cache parent is not absolute: %q", path)
	}
	path = filepath.Clean(path)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect default cache parent: %w", err)
	}
	if err := rejectMappedPathReparsePoints(path); err != nil {
		return fmt.Errorf("refusing unsafe default cache parent cleanup: %w", err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return fmt.Errorf("inspect default cache parent reparse state: %w", err)
	}
	if reparse || !info.IsDir() {
		return errors.New("default cache parent is not a regular non-reparse directory")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("read default cache parent: %w", err)
	}
	if len(entries) != 0 {
		return nil
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open parent of default cache namespace: %w", err)
	}
	defer root.Close()
	rootedInfo, err := root.Lstat(filepath.Base(path))
	if err != nil {
		return fmt.Errorf("revalidate default cache parent: %w", err)
	}
	if !os.SameFile(info, rootedInfo) {
		return errors.New("default cache parent identity changed before removal")
	}
	if err := root.Remove(filepath.Base(path)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if currentEntries, readErr := os.ReadDir(path); readErr == nil && len(currentEntries) != 0 {
			return nil
		}
		return fmt.Errorf("remove empty default cache parent: %w", err)
	}
	if _, err := root.Lstat(filepath.Base(path)); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return errors.New("remove empty default cache parent: path still exists")
		}
		return fmt.Errorf("verify default cache parent removal: %w", err)
	}
	return nil
}

type installerRemovalRoot struct {
	role string
	path string
}

func installerRemovalRoots(paths installerCleanPaths, deleteConfiguration bool) []installerRemovalRoot {
	candidates := []installerRemovalRoot{
		{role: "package and tool cache", path: paths.CacheDirectory},
		{role: "machine-local application state", path: paths.DataDirectory},
	}
	if deleteConfiguration {
		candidates = append(candidates, installerRemovalRoot{role: "user configuration", path: paths.ConfigurationDirectory})
	}
	result := make([]installerRemovalRoot, 0, len(candidates))
	for index, candidate := range candidates {
		covered := false
		for otherIndex, other := range candidates {
			if index == otherIndex || strings.EqualFold(filepath.Clean(candidate.path), filepath.Clean(other.path)) {
				continue
			}
			if hostPathContains(other.path, candidate.path) {
				covered = true
				break
			}
		}
		if !covered {
			duplicate := false
			for _, existing := range result {
				if strings.EqualFold(filepath.Clean(existing.path), filepath.Clean(candidate.path)) {
					duplicate = true
					break
				}
			}
			if !duplicate {
				result = append(result, candidate)
			}
		}
	}
	return result
}

func planInstallerDirectoryRemoval(path, role string) (*installerDirectoryRemovalPlan, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%s directory is not absolute: %q", role, path)
	}
	path = filepath.Clean(path)
	volumeRoot := filepath.Clean(filepath.VolumeName(path) + string(os.PathSeparator))
	if strings.EqualFold(path, volumeRoot) || filepath.Dir(path) == path || filepath.Base(path) == "." {
		return nil, fmt.Errorf("refusing to remove unsafe %s directory: %s", role, path)
	}
	plan := &installerDirectoryRemovalPlan{Role: role, Path: path, Name: filepath.Base(path)}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return plan, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect %s directory: %w", role, err)
	}
	if err := validateInstallerRemovalTree(path, role); err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s path is not a directory: %s", role, path)
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("open parent of %s directory: %w", role, err)
	}
	plan.Root = root
	rootedInfo, err := root.Lstat(plan.Name)
	if err != nil {
		plan.close()
		return nil, fmt.Errorf("inspect rooted %s directory: %w", role, err)
	}
	if !os.SameFile(info, rootedInfo) {
		plan.close()
		return nil, fmt.Errorf("%s directory identity changed during preflight", role)
	}
	plan.EntryInfo = rootedInfo
	plan.RootExists = true
	return plan, nil
}

func validateInstallerRemovalTree(path, role string) error {
	if err := rejectMappedPathReparsePoints(path); err != nil {
		return fmt.Errorf("refusing unsafe %s directory: %w", role, err)
	}
	return filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		reparse, err := fileInfoIsReparsePoint(info)
		if err != nil {
			return err
		}
		if reparse {
			relative, relativeErr := filepath.Rel(path, current)
			if relativeErr != nil {
				return relativeErr
			}
			return fmt.Errorf("refusing unsafe %s directory: contains reparse point %s", role, relative)
		}
		return nil
	})
}

func applyInstallerDirectoryRemoval(plan *installerDirectoryRemovalPlan) error {
	if !plan.RootExists {
		if _, err := os.Lstat(plan.Path); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return fmt.Errorf("verify absent %s directory: %w", plan.Role, err)
		}
		return fmt.Errorf("%s directory appeared after preflight: %s", plan.Role, plan.Path)
	}
	if plan.Root == nil || plan.EntryInfo == nil {
		return fmt.Errorf("%s removal plan is incomplete", plan.Role)
	}
	if err := validateInstallerRemovalTree(plan.Path, plan.Role); err != nil {
		return err
	}
	current, err := plan.Root.Lstat(plan.Name)
	if err != nil {
		return fmt.Errorf("revalidate rooted %s directory: %w", plan.Role, err)
	}
	if !os.SameFile(plan.EntryInfo, current) {
		return fmt.Errorf("%s directory identity changed before removal", plan.Role)
	}
	if err := plan.Root.RemoveAll(plan.Name); err != nil {
		return fmt.Errorf("remove %s directory: %w", plan.Role, err)
	}
	if _, err := plan.Root.Lstat(plan.Name); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("remove %s directory: path still exists", plan.Role)
		}
		return fmt.Errorf("verify %s directory removal: %w", plan.Role, err)
	}
	return nil
}

func planInstallerSSHRemoval(userHome, dataDirectory string) (installerSSHRemovalPlan, error) {
	if !filepath.IsAbs(userHome) || !filepath.IsAbs(dataDirectory) {
		return installerSSHRemovalPlan{}, errors.New("SSH uninstall paths must be absolute")
	}
	path := filepath.Join(filepath.Clean(userHome), ".ssh", "config")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return installerSSHRemovalPlan{Path: path}, nil
	}
	if err != nil {
		return installerSSHRemovalPlan{}, fmt.Errorf("inspect user SSH config for uninstall: %w", err)
	}
	if err := rejectMappedPathReparsePoints(path); err != nil {
		return installerSSHRemovalPlan{}, fmt.Errorf("refusing unsafe user SSH config cleanup: %w", err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return installerSSHRemovalPlan{}, fmt.Errorf("inspect user SSH config reparse state: %w", err)
	}
	if reparse || !info.Mode().IsRegular() {
		return installerSSHRemovalPlan{}, errors.New("user SSH config is not a regular non-reparse file")
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		return installerSSHRemovalPlan{}, fmt.Errorf("read user SSH config for uninstall: %w", err)
	}
	managedPath := filepath.Join(filepath.Clean(dataDirectory), "ssh", "config")
	updated, changed, err := removeManagedSSHInclude(string(existing), managedPath)
	if err != nil {
		return installerSSHRemovalPlan{}, fmt.Errorf("remove managed block from user SSH config: %w", err)
	}
	return installerSSHRemovalPlan{
		Path: path, Info: info, Existing: existing, Updated: []byte(updated), Mode: info.Mode().Perm(), Changed: changed,
	}, nil
}

func applyInstallerSSHRemoval(plan installerSSHRemovalPlan) error {
	if !plan.Changed {
		return nil
	}
	info, err := os.Lstat(plan.Path)
	if err != nil {
		return fmt.Errorf("revalidate user SSH config for uninstall: %w", err)
	}
	if !os.SameFile(plan.Info, info) {
		return errors.New("user SSH config identity changed before uninstall cleanup")
	}
	if err := rejectMappedPathReparsePoints(plan.Path); err != nil {
		return fmt.Errorf("user SSH config changed before uninstall cleanup: %w", err)
	}
	current, err := os.ReadFile(plan.Path)
	if err != nil {
		return fmt.Errorf("reread user SSH config for uninstall: %w", err)
	}
	if !bytes.Equal(current, plan.Existing) {
		return errors.New("user SSH config contents changed before uninstall cleanup")
	}
	if err := writeFileAtomically(plan.Path, plan.Updated, plan.Mode); err != nil {
		return fmt.Errorf("write user SSH config without managed block: %w", err)
	}
	written, err := os.ReadFile(plan.Path)
	if err != nil {
		return fmt.Errorf("verify user SSH config after uninstall cleanup: %w", err)
	}
	if !bytes.Equal(written, plan.Updated) {
		return errors.New("user SSH config verification failed after uninstall cleanup")
	}
	return nil
}

func removeManagedSSHInclude(existing, managedPath string) (string, bool, error) {
	startCount := strings.Count(existing, managedSSHIncludeStart)
	endCount := strings.Count(existing, managedSSHIncludeEnd)
	if startCount == 0 && endCount == 0 {
		return existing, false, nil
	}
	if startCount != 1 || endCount != 1 || !strings.HasPrefix(existing, managedSSHIncludeStart) {
		return "", false, errors.New("managed Sandbox SSH include markers are malformed")
	}
	afterStart := existing[len(managedSSHIncludeStart):]
	lineEnding := ""
	if strings.HasPrefix(afterStart, "\r\n") {
		lineEnding = "\r\n"
	} else if strings.HasPrefix(afterStart, "\n") {
		lineEnding = "\n"
	} else {
		return "", false, errors.New("managed Sandbox SSH include start line is malformed")
	}
	expected := strings.Join([]string{
		managedSSHIncludeStart,
		"Include " + quoteSSHPath(managedPath),
		managedSSHIncludeEnd,
	}, lineEnding)
	if !strings.HasPrefix(existing, expected) {
		return "", false, errors.New("managed Sandbox SSH include block does not match the app-owned path")
	}
	remaining := existing[len(expected):]
	if remaining == "" {
		return "", true, nil
	}
	if !strings.HasPrefix(remaining, lineEnding) {
		return "", false, errors.New("managed Sandbox SSH include end line is malformed")
	}
	remaining = remaining[len(lineEnding):]
	if strings.HasPrefix(remaining, lineEnding) {
		remaining = remaining[len(lineEnding):]
	}
	return remaining, true, nil
}
