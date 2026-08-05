//go:build !windows

package cli

func acquireInstallerLifecycleGate(_ []string) (func(), error) {
	return func() {}, nil
}
