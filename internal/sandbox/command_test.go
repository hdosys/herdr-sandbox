package sandbox

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"herdr-sandbox/internal/hiddenprocess"
)

const testCommandTimeout = 2 * time.Minute

const boundedOutputHelper = "HERDR_SANDBOX_BOUNDED_OUTPUT_HELPER"

type boundedTestCommand struct {
	*hiddenprocess.Command
	cancel context.CancelFunc
}

func hiddenCommand(name string, args ...string) *boundedTestCommand {
	ctx, cancel := context.WithTimeout(context.Background(), testCommandTimeout)
	return &boundedTestCommand{Command: hiddenCommandContext(ctx, name, args...), cancel: cancel}
}

func (c *boundedTestCommand) CombinedOutput() ([]byte, error) {
	defer c.cancel()
	return c.Command.CombinedOutput()
}

func (c *boundedTestCommand) Run() error {
	defer c.cancel()
	return c.Command.Run()
}

func (c *boundedTestCommand) Wait() error {
	defer c.cancel()
	return c.Command.Wait()
}

func TestRunBoundedGitHubCLITerminatesOwnedCommandOnOverflow(t *testing.T) {
	environment := append(os.Environ(), boundedOutputHelper+"=1")
	_, err := runBoundedGitHubCLI(t.Context(), os.Args[0], environment, 4, "-test.run=^TestBoundedOutputHelper$")
	if err == nil || !strings.Contains(err.Error(), "GitHub CLI output exceeds 4 bytes") {
		t.Fatalf("runBoundedGitHubCLI error = %v", err)
	}
}

func TestBoundedOutputHelper(t *testing.T) {
	if os.Getenv(boundedOutputHelper) != "1" {
		return
	}
	fmt.Fprint(os.Stdout, "overflow")
	time.Sleep(testCommandTimeout)
}
