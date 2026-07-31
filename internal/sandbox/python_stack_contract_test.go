package sandbox

import (
	"strings"
	"testing"
)

func TestPythonStackBuildsDottedRuntimeVersion(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	if !strings.Contains(text, `$runtimeVersion = ($Version -split '\.')[0..2] -join '.'`) {
		t.Fatal("Python stack does not build a dotted runtime version")
	}
	if strings.Contains(text, `-join '\.'`) {
		t.Fatal("Python stack joins version components with a literal regex escape")
	}
}
