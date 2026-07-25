package sandbox

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	visualStudioLayoutScriptName = "prepare-visual-studio-layout.ps1"
	visualStudioLayoutTimeout    = 90 * time.Minute
	maximumVisualStudioOutput    = 4000
)

//go:embed assets/visual-studio-layout.ps1
var visualStudioLayoutScript []byte

type boundedOutputCapture struct {
	data []byte
}

func (capture *boundedOutputCapture) Write(value []byte) (int, error) {
	capture.data = append(capture.data, value...)
	if len(capture.data) > maximumVisualStudioOutput {
		capture.data = append([]byte(nil), capture.data[len(capture.data)-maximumVisualStudioOutput:]...)
	}
	return len(value), nil
}

func prepareVisualStudioLayout(ctx context.Context, plan runPlan, output io.Writer) error {
	if !plan.RequiresVisualStudioLayout {
		return nil
	}
	if !filepath.IsAbs(plan.CacheDirectory) || !filepath.IsAbs(plan.RunDirectory) {
		return fmt.Errorf("Visual Studio layout preparation requires absolute run and cache directories")
	}
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		return err
	}
	scriptPath := filepath.Join(plan.RunDirectory, visualStudioLayoutScriptName)
	if err := os.WriteFile(scriptPath, visualStudioLayoutScript, 0o600); err != nil {
		return fmt.Errorf("write Visual Studio host layout script: %w", err)
	}

	layoutContext, cancel := context.WithTimeout(ctx, visualStudioLayoutTimeout)
	defer cancel()
	command := hiddenCommandContext(layoutContext, powerShell,
		"-NoLogo", "-NoProfile", "-NonInteractive", "-File", scriptPath,
		"-CacheDirectory", plan.CacheDirectory,
		"-TimeoutSeconds", strconv.Itoa(int(visualStudioLayoutTimeout/time.Second)),
	)
	capture := &boundedOutputCapture{}
	writer := io.MultiWriter(output, capture)
	command.Stdout = writer
	command.Stderr = writer
	if err := command.Run(); err != nil {
		if layoutContext.Err() != nil {
			return fmt.Errorf("prepare Visual Studio host layout: %w", layoutContext.Err())
		}
		return fmt.Errorf("prepare Visual Studio host layout: %w: %s", err, boundedText(capture.data))
	}
	return nil
}
