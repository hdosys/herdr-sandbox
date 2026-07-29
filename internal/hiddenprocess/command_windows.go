//go:build windows

package hiddenprocess

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

const (
	createSuspended                        = 0x00000004
	processSetQuota                        = 0x00000100
	threadSuspendResume                    = 0x0002
	jobObjectExtendedLimitInformationClass = 9
	jobObjectLimitKillOnJobClose           = 0x00002000
	jobTerminationExitCode                 = 1
)

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procCreateJobObject          = kernel32.NewProc("CreateJobObjectW")
	procOpenProcess              = kernel32.NewProc("OpenProcess")
	procOpenThread               = kernel32.NewProc("OpenThread")
	procResumeThread             = kernel32.NewProc("ResumeThread")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procTerminateJobObject       = kernel32.NewProc("TerminateJobObject")
	procTerminateProcess         = kernel32.NewProc("TerminateProcess")
	procThread32First            = kernel32.NewProc("Thread32First")
	procThread32Next             = kernel32.NewProc("Thread32Next")
)

// Command owns one noninteractive process tree through an unnamed Windows Job Object.
type Command struct {
	*exec.Cmd
	ctx context.Context

	mu            sync.Mutex
	startCalled   bool
	started       bool
	waitCalled    bool
	finished      bool
	contextErr    error
	processDone   chan struct{}
	watcherDone   chan struct{}
	job           syscall.Handle
	terminateOnce sync.Once
	terminateErr  error
	closeOnce     sync.Once
	closeErr      error
}

// CommandContext creates a hidden command whose descendants cannot escape cancellation.
func CommandContext(ctx context.Context, name string, args ...string) *Command {
	if ctx == nil {
		panic("nil Context")
	}
	command := exec.Command(name, args...)
	Configure(command)
	command.SysProcAttr.CreationFlags |= createSuspended
	command.WaitDelay = commandWaitDelay
	return &Command{Cmd: command, ctx: ctx}
}

// Start creates a kill-on-close Job Object, starts the target suspended, assigns
// the exact process to the job, and only then resumes its primary thread.
func (c *Command) Start() error {
	c.mu.Lock()
	if c.startCalled {
		c.mu.Unlock()
		return errors.New("exec: already started")
	}
	c.startCalled = true
	c.mu.Unlock()

	if err := c.ctx.Err(); err != nil {
		return err
	}
	Configure(c.Cmd)
	c.SysProcAttr.CreationFlags |= createSuspended
	c.WaitDelay = commandWaitDelay

	job, err := createKillOnCloseJob()
	if err != nil {
		return err
	}
	if err := c.Cmd.Start(); err != nil {
		return combineErrors(err, closeNativeHandle("hidden process job", job))
	}

	processHandle, err := openJobProcess(c.Process.Pid)
	if err != nil {
		return c.rollbackStart(job, 0, false, fmt.Errorf("open suspended process %d: %w", c.Process.Pid, err))
	}
	assigned := false
	if err := assignProcessToJob(job, processHandle); err != nil {
		err = fmt.Errorf("assign suspended process %d to Job Object: %w", c.Process.Pid, err)
		return c.rollbackStart(job, processHandle, assigned, err)
	}
	assigned = true

	setupErr := c.ctx.Err()
	if setupErr == nil {
		setupErr = resumeChildThread(c.Process.Pid)
		if setupErr != nil {
			setupErr = fmt.Errorf("resume suspended process %d: %w", c.Process.Pid, setupErr)
		}
	}
	processCloseErr := closeNativeHandle("suspended process", processHandle)
	if setupErr != nil || processCloseErr != nil {
		return c.rollbackStart(job, 0, assigned, combineErrors(setupErr, processCloseErr))
	}

	processDone := make(chan struct{})
	watcherDone := make(chan struct{})
	c.mu.Lock()
	c.job = job
	c.processDone = processDone
	c.watcherDone = watcherDone
	c.started = true
	c.mu.Unlock()
	if c.ctx.Done() == nil {
		close(watcherDone)
	} else {
		go c.watchContext()
	}
	return nil
}

// Wait reaps the immediate process, terminates any surviving descendants, and
// closes the Job Object after its context watcher has stopped.
func (c *Command) Wait() error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return errors.New("exec: not started")
	}
	if c.waitCalled {
		c.mu.Unlock()
		return errors.New("exec: Wait was already called")
	}
	c.waitCalled = true
	c.mu.Unlock()

	processErr := c.Cmd.Wait()
	c.mu.Lock()
	if c.contextErr == nil {
		c.contextErr = c.ctx.Err()
	}
	c.finished = true
	close(c.processDone)
	c.mu.Unlock()
	<-c.watcherDone

	terminateErr := c.Terminate()
	closeErr := c.closeJob()
	c.mu.Lock()
	contextErr := c.contextErr
	c.mu.Unlock()
	return combineErrors(processErr, contextErr, terminateErr, closeErr)
}

// Terminate force-terminates every process associated with the command's Job Object.
func (c *Command) Terminate() error {
	c.mu.Lock()
	started := c.started
	job := c.job
	c.mu.Unlock()
	if !started || job == 0 {
		return errors.New("exec: not started")
	}
	c.terminateOnce.Do(func() {
		if err := terminateJob(job); err != nil {
			c.terminateErr = fmt.Errorf("terminate hidden process Job Object: %w", err)
			// KILL_ON_JOB_CLOSE is the exact fallback when explicit job
			// termination fails; closing never falls back to a PID.
			_ = c.closeJob()
		}
	})
	return c.terminateErr
}

func (c *Command) watchContext() {
	defer close(c.watcherDone)
	select {
	case <-c.processDone:
		return
	case <-c.ctx.Done():
	}
	c.mu.Lock()
	if c.finished {
		c.mu.Unlock()
		return
	}
	c.contextErr = c.ctx.Err()
	c.mu.Unlock()
	_ = c.Terminate()
}

func (c *Command) closeJob() error {
	c.closeOnce.Do(func() {
		c.closeErr = closeNativeHandle("hidden process job", c.job)
	})
	return c.closeErr
}

func (c *Command) rollbackStart(job, process syscall.Handle, assigned bool, cause error) error {
	errs := []error{cause}
	if assigned {
		if err := terminateJob(job); err != nil {
			errs = append(errs, fmt.Errorf("terminate Job Object during start rollback: %w", err))
		}
	} else if process != 0 {
		if err := terminateProcess(process); err != nil {
			errs = append(errs, fmt.Errorf("terminate suspended process during start rollback: %w", err))
		}
	} else if c.Process != nil {
		// OpenProcess failed before assignment; os.Process still owns the only
		// exact handle to this suspended target. This is start rollback, not a
		// second runtime cancellation path.
		if err := c.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			errs = append(errs, fmt.Errorf("terminate unopened suspended process during start rollback: %w", err))
		}
	}
	if process != 0 {
		errs = append(errs, closeNativeHandle("suspended process", process))
	}
	errs = append(errs, closeNativeHandle("hidden process job", job))
	if waitErr := c.Cmd.Wait(); waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			errs = append(errs, fmt.Errorf("reap process during start rollback: %w", waitErr))
		}
	}
	return combineErrors(errs...)
}

func createKillOnCloseJob() (syscall.Handle, error) {
	raw, _, callErr := procCreateJobObject.Call(0, 0)
	if raw == 0 {
		return 0, fmt.Errorf("create hidden process Job Object: %w", windowsCallError(callErr))
	}
	job := syscall.Handle(raw)
	information := jobObjectExtendedLimitInformation{}
	information.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	result, _, callErr := procSetInformationJobObject.Call(
		uintptr(job),
		jobObjectExtendedLimitInformationClass,
		uintptr(unsafe.Pointer(&information)),
		unsafe.Sizeof(information),
	)
	if result == 0 {
		return 0, combineErrors(
			fmt.Errorf("set Job Object kill-on-close limit: %w", windowsCallError(callErr)),
			closeNativeHandle("hidden process job", job),
		)
	}
	return job, nil
}

func openJobProcess(pid int) (syscall.Handle, error) {
	raw, _, callErr := procOpenProcess.Call(processSetQuota|syscall.PROCESS_TERMINATE, 0, uintptr(uint32(pid)))
	if raw == 0 {
		return 0, windowsCallError(callErr)
	}
	return syscall.Handle(raw), nil
}

func assignProcessToJob(job, process syscall.Handle) error {
	result, _, callErr := procAssignProcessToJobObject.Call(uintptr(job), uintptr(process))
	if result == 0 {
		return windowsCallError(callErr)
	}
	return nil
}

func terminateJob(job syscall.Handle) error {
	result, _, callErr := procTerminateJobObject.Call(uintptr(job), jobTerminationExitCode)
	if result == 0 {
		return windowsCallError(callErr)
	}
	return nil
}

func terminateProcess(process syscall.Handle) error {
	result, _, callErr := procTerminateProcess.Call(uintptr(process), jobTerminationExitCode)
	if result == 0 {
		return windowsCallError(callErr)
	}
	return nil
}

// resumeChildThread follows the Go runtime's Toolhelp32/OpenThread/ResumeThread pattern.
func resumeChildThread(pid int) (err error) {
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer func() {
		err = combineErrors(err, closeNativeHandle("thread snapshot", snapshot))
	}()

	var entry threadEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	result, _, callErr := procThread32First.Call(uintptr(snapshot), uintptr(unsafe.Pointer(&entry)))
	if result == 0 {
		return windowsCallError(callErr)
	}
	// A process created suspended has not run code that could create another
	// thread, so its matching snapshot entry is the primary thread.
	for entry.OwnerProcessID != uint32(pid) {
		result, _, callErr = procThread32Next.Call(uintptr(snapshot), uintptr(unsafe.Pointer(&entry)))
		if result == 0 {
			return windowsCallError(callErr)
		}
	}

	rawThread, _, callErr := procOpenThread.Call(threadSuspendResume, 1, uintptr(entry.ThreadID))
	if rawThread == 0 {
		return windowsCallError(callErr)
	}
	thread := syscall.Handle(rawThread)
	defer func() {
		err = combineErrors(err, closeNativeHandle("suspended process thread", thread))
	}()
	result, _, callErr = procResumeThread.Call(rawThread)
	if result == 0xffffffff {
		return windowsCallError(callErr)
	}
	return nil
}

func closeNativeHandle(role string, handle syscall.Handle) error {
	if handle == 0 {
		return nil
	}
	if err := syscall.CloseHandle(handle); err != nil {
		return fmt.Errorf("close %s handle: %w", role, err)
	}
	return nil
}

func windowsCallError(err error) error {
	if err == nil || errors.Is(err, syscall.Errno(0)) {
		return syscall.EINVAL
	}
	return err
}

// These layouts are the minimal amd64-compatible forms used by
// JOBOBJECT_EXTENDED_LIMIT_INFORMATION in Go's vendored x/sys definitions.
type jobObjectBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobObjectExtendedLimitInformation struct {
	BasicLimitInformation jobObjectBasicLimitInformation
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

type threadEntry32 struct {
	Size           uint32
	Usage          uint32
	ThreadID       uint32
	OwnerProcessID uint32
	BasePriority   int32
	DeltaPriority  int32
	Flags          uint32
}
