package sandbox

import (
	"encoding/xml"
	"path/filepath"
	"testing"
)

func TestRenderConfigMapsSharedModelsReadWrite(t *testing.T) {
	encoded, err := renderConfigWithMappedDirectories(
		`C:\Runs\one\input`,
		`C:\Runs\one\status`,
		`E:\cache`,
		"",
		`F:\Models`,
		nil,
		nil,
		4096,
		false,
		false,
	)
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	var config wsbConfiguration
	if err := xml.Unmarshal(encoded, &config); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	found := false
	for _, mapping := range config.MappedFolders.Folders {
		if mapping.SandboxFolder == guestModelsDirectory {
			found = true
			if mapping.HostFolder != `F:\Models` || mapping.ReadOnly {
				t.Fatalf("models mapping = %#v", mapping)
			}
		}
	}
	if !found {
		t.Fatal("models mapping is missing")
	}
}

func TestConfiguredModelsDirectoryRequiresExistingAbsoluteDirectory(t *testing.T) {
	directory := t.TempDir()
	got, err := validateConfiguredModelsDirectory(directory)
	if err != nil {
		t.Fatalf("validate model directory: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("validated model directory is not absolute: %q", got)
	}
	if _, err := validateConfiguredModelsDirectory("models"); err == nil {
		t.Fatal("relative model directory unexpectedly succeeded")
	}
}
