package sandbox

import (
	"encoding/xml"
	"path/filepath"
	"testing"
)

func TestRenderConfigMapsVoxCPM2ModelsReadOnly(t *testing.T) {
	encoded, err := renderConfigWithMappedDirectories(
		`C:\Runs\one\input`,
		`C:\Runs\one\status`,
		`E:\cache`,
		"",
		`F:\Models\VoxCPM2`,
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
		if mapping.SandboxFolder == guestVoxCPM2Models {
			found = true
			if mapping.HostFolder != `F:\Models\VoxCPM2` || !mapping.ReadOnly {
				t.Fatalf("VoxCPM2 mapping = %#v", mapping)
			}
		}
	}
	if !found {
		t.Fatal("VoxCPM2 model mapping is missing")
	}
}

func TestConfiguredVoxCPM2ModelDirectoryRequiresExistingAbsoluteDirectory(t *testing.T) {
	directory := t.TempDir()
	got, err := validateConfiguredVoxCPM2ModelDirectory(directory)
	if err != nil {
		t.Fatalf("validate model directory: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("validated model directory is not absolute: %q", got)
	}
	if _, err := validateConfiguredVoxCPM2ModelDirectory("models"); err == nil {
		t.Fatal("relative model directory unexpectedly succeeded")
	}
}
