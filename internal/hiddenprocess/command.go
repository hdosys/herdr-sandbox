// Package hiddenprocess launches bounded process trees without visible console windows.
package hiddenprocess

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	commandWaitDelay           = 15 * time.Second
	maximumCombinedOutputBytes = 1024 * 1024
)

var errCombinedOutputLimit = errors.New("combined command output limit exceeded")

// Run starts the command through its platform owner and waits for completion.
func (c *Command) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

// CombinedOutput runs the owned command and returns combined standard output and error.
func (c *Command) CombinedOutput() ([]byte, error) {
	if c.Stdout != nil {
		return nil, errors.New("exec: Stdout already set")
	}
	if c.Stderr != nil {
		return nil, errors.New("exec: Stderr already set")
	}
	output := newBoundedOutput(maximumCombinedOutputBytes)
	c.Stdout = output
	c.Stderr = output
	if err := c.Start(); err != nil {
		return nil, err
	}
	waited := make(chan error, 1)
	go func() { waited <- c.Wait() }()
	select {
	case err := <-waited:
		if output.Exceeded() {
			err = errors.Join(errCombinedOutputLimit, err)
		}
		return output.Bytes(), err
	case <-output.LimitExceeded():
		terminateErr := c.Terminate()
		waitErr := <-waited
		return output.Bytes(), errors.Join(
			fmt.Errorf("%w: maximum %d bytes", errCombinedOutputLimit, maximumCombinedOutputBytes),
			terminateErr,
			waitErr,
		)
	}
}

type boundedOutput struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	limit    int
	exceeded bool
	notify   chan struct{}
}

func newBoundedOutput(limit int) *boundedOutput {
	return &boundedOutput{limit: limit, notify: make(chan struct{})}
}

func (output *boundedOutput) Write(data []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	remaining := output.limit - output.buffer.Len()
	if remaining > 0 {
		_, _ = output.buffer.Write(data[:min(len(data), remaining)])
	}
	if len(data) > remaining && !output.exceeded {
		output.exceeded = true
		close(output.notify)
	}
	return len(data), nil
}

func (output *boundedOutput) Bytes() []byte {
	output.mu.Lock()
	defer output.mu.Unlock()
	return bytes.Clone(output.buffer.Bytes())
}

func (output *boundedOutput) Exceeded() bool {
	output.mu.Lock()
	defer output.mu.Unlock()
	return output.exceeded
}

func (output *boundedOutput) LimitExceeded() <-chan struct{} {
	return output.notify
}

func combineErrors(errs ...error) error {
	nonNil := errs[:0]
	for _, err := range errs {
		if err != nil {
			nonNil = append(nonNil, err)
		}
	}
	switch len(nonNil) {
	case 0:
		return nil
	case 1:
		return nonNil[0]
	default:
		return errors.Join(nonNil...)
	}
}
