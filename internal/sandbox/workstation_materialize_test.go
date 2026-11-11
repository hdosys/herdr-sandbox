package sandbox

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWorkstationInputsExtractAndRejectCorruptEntry(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("uses the Windows input-publication lock")
	}
	payload := []byte("installer payload fixture")
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, entry := range []struct{ name, contents string }{
		{"prepared-inputs.json", `{"schemaVersion":1,"packages":[],"tools":[]}`},
		{"cache/packages/fixture/payload.bin", string(payload)},
	} {
		file, err := writer.CreateHeader(&zip.FileHeader{Name: entry.name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(file, entry.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, corrupt := range []bool{false, true} {
		name := "complete"
		if corrupt {
			name = "corrupt payload is not published"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("ProgramData", root)
			data := bytes.Clone(archive.Bytes())
			if corrupt {
				position := bytes.Index(data, payload)
				if position < 0 {
					t.Fatal("fixture payload missing")
				}
				data[position] ^= 1
			}
			source := filepath.Join(root, "inputs.zip")
			if err := os.WriteFile(source, data, 0o600); err != nil {
				t.Fatal(err)
			}
			err := applyWorkstationInputs(t.Context(), filepath.Join(root, "input"), source, io.Discard)
			destination := filepath.Join(root, "herdr-workstation", "cache", "packages", "fixture", "payload.bin")
			if corrupt {
				if err == nil {
					t.Fatal("corrupt input reported success")
				}
				if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
					t.Fatalf("corrupt payload published: %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(destination)
			if err != nil || !bytes.Equal(actual, payload) {
				t.Fatalf("extracted payload differs: %v", err)
			}
		})
	}
}
