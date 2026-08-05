//go:build windows

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-sandbox/internal/productidentity"
)

func TestPendingInstallerTransactionBlocksOnlyInstalledCommand(t *testing.T) {
	localAppData := t.TempDir()
	installDirectory := filepath.Join(localAppData, "Programs", productidentity.InstallDirectoryName)
	installedExecutable := filepath.Join(installDirectory, productidentity.ExecutableName)
	if err := rejectPendingInstallerTransactionAt(installedExecutable, localAppData); err != nil {
		t.Fatalf("transaction-free installed command: %v", err)
	}

	transactionDirectory := filepath.Join(localAppData, "Programs", "."+productidentity.ApplicationName+"-installer-transaction")
	if err := os.MkdirAll(transactionDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := rejectPendingInstallerTransactionAt(installedExecutable, localAppData); err == nil || !strings.Contains(err.Error(), "must be recovered") {
		t.Fatalf("pending transaction error = %v", err)
	}

	portableExecutable := filepath.Join(localAppData, "portable", productidentity.ExecutableName)
	if err := rejectPendingInstallerTransactionAt(portableExecutable, localAppData); err != nil {
		t.Fatalf("portable command was blocked by installed transaction: %v", err)
	}
}
