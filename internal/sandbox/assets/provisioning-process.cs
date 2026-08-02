// herdr-sandbox-provisioning-process-contract: 1
using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.Diagnostics;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using Microsoft.Win32.SafeHandles;

namespace HerdrSandbox
{
    public sealed class ProvisioningProcessSpec
    {
        public string Role { get; set; }
        public string FilePath { get; set; }
        public string[] Arguments { get; set; }
        public string WorkingDirectory { get; set; }
        public int TimeoutMilliseconds { get; set; }
        public int[] SuccessExitCodes { get; set; }
    }

    public sealed class ProvisioningProcessResult
    {
        public string Role { get; internal set; }
        public int ExitCode { get; internal set; }
        public string Output { get; internal set; }
        public long OutputBytes { get; internal set; }
        public bool OutputTruncated { get; internal set; }
        public long ElapsedMilliseconds { get; internal set; }
        public bool TimedOut { get; internal set; }
        public bool Stopped { get; internal set; }
        public bool Succeeded { get; internal set; }
    }

    public static class ProvisioningProcess
    {
        public const int ContractVersion = 1;
        public const int MaximumGroupTasks = 2;
        public const int MaximumConcurrentDownloads = 3;

        public static ProvisioningProcessResult Run(ProvisioningProcessSpec spec)
        {
            using (ProvisioningProcessTask task = ProvisioningProcessTask.Start(spec))
            {
                return task.Complete();
            }
        }

        public static ProvisioningProcessGroup StartGroup(ProvisioningProcessSpec[] specs)
        {
            if (specs == null || specs.Length != MaximumGroupTasks)
            {
                throw new ArgumentException("A provisioning process group requires exactly two tasks.", "specs");
            }
            return ProvisioningProcessGroup.Start(specs);
        }
    }

    public sealed class ProvisioningProcessGroup : IDisposable
    {
        private readonly ProvisioningProcessTask[] tasks;
        private bool completed;
        private bool disposed;

        private ProvisioningProcessGroup(ProvisioningProcessTask[] tasks)
        {
            this.tasks = tasks;
        }

        internal static ProvisioningProcessGroup Start(ProvisioningProcessSpec[] specs)
        {
            ProvisioningProcessTask[] tasks = new ProvisioningProcessTask[specs.Length];
            int started = 0;
            try
            {
                for (; started < specs.Length; started++)
                {
                    tasks[started] = ProvisioningProcessTask.Start(specs[started]);
                }
                return new ProvisioningProcessGroup(tasks);
            }
            catch
            {
                for (int index = 0; index < started; index++)
                {
                    try
                    {
                        tasks[index].Stop(false);
                    }
                    catch
                    {
                    }
                }
                for (int index = 0; index < started; index++)
                {
                    tasks[index].Dispose();
                }
                throw;
            }
        }

        public ProvisioningProcessResult[] Complete()
        {
            ThrowIfDisposed();
            if (completed)
            {
                throw new InvalidOperationException("The provisioning process group was already completed.");
            }
            completed = true;

            ProvisioningProcessResult[] results = new ProvisioningProcessResult[tasks.Length];
            List<int> pending = new List<int>();
            for (int index = 0; index < tasks.Length; index++)
            {
                pending.Add(index);
            }

            Exception infrastructureFailure = null;
            try
            {
                while (pending.Count != 0)
                {
                    int waitMilliseconds = Int32.MaxValue;
                    for (int index = 0; index < pending.Count; index++)
                    {
                        ProvisioningProcessTask task = tasks[pending[index]];
                        int remaining = task.RemainingTimeoutMilliseconds;
                        if (remaining < waitMilliseconds)
                        {
                            waitMilliseconds = remaining;
                        }
                    }

                    if (waitMilliseconds <= 0)
                    {
                        int timedOutIndex = FindExpiredTask(pending);
                        tasks[timedOutIndex].Stop(true);
                        StopPending(pending, timedOutIndex);
                        break;
                    }

                    Task<ProvisioningProcessResult>[] completions = new Task<ProvisioningProcessResult>[pending.Count];
                    for (int index = 0; index < pending.Count; index++)
                    {
                        completions[index] = tasks[pending[index]].Completion;
                    }
                    int completedPosition = Task.WaitAny(completions, waitMilliseconds);
                    if (completedPosition < 0)
                    {
                        int timedOutIndex = FindExpiredTask(pending);
                        tasks[timedOutIndex].Stop(true);
                        StopPending(pending, timedOutIndex);
                        break;
                    }

                    int completedIndex = pending[completedPosition];
                    try
                    {
                        results[completedIndex] = tasks[completedIndex].Completion.GetAwaiter().GetResult();
                    }
                    catch (Exception error)
                    {
                        infrastructureFailure = error;
                        StopPending(pending, completedIndex);
                        break;
                    }
                    pending.RemoveAt(completedPosition);
                    if (!results[completedIndex].Succeeded)
                    {
                        StopPending(pending, -1);
                        break;
                    }
                }

                for (int index = 0; index < tasks.Length; index++)
                {
                    if (results[index] != null)
                    {
                        continue;
                    }
                    try
                    {
                        results[index] = tasks[index].WaitAfterStop();
                    }
                    catch (Exception error)
                    {
                        if (infrastructureFailure == null)
                        {
                            infrastructureFailure = error;
                        }
                    }
                }
            }
            finally
            {
                for (int index = 0; index < tasks.Length; index++)
                {
                    tasks[index].Dispose();
                }
            }

            if (infrastructureFailure != null)
            {
                throw new InvalidOperationException("Provisioning process group ownership failed.", infrastructureFailure);
            }
            return results;
        }

        public void Stop()
        {
            if (disposed)
            {
                return;
            }
            Exception stopFailure = null;
            for (int index = 0; index < tasks.Length; index++)
            {
                try
                {
                    tasks[index].Stop(false);
                }
                catch (Exception error)
                {
                    if (stopFailure == null)
                    {
                        stopFailure = error;
                    }
                }
            }
            for (int index = 0; index < tasks.Length; index++)
            {
                try
                {
                    tasks[index].WaitAfterStop();
                }
                catch
                {
                }
            }
            completed = true;
            if (stopFailure != null)
            {
                throw new InvalidOperationException("Stopping the provisioning process group failed.", stopFailure);
            }
        }

        public void Dispose()
        {
            if (disposed)
            {
                return;
            }
            Exception stopFailure = null;
            if (!completed)
            {
                try
                {
                    Stop();
                }
                catch (Exception error)
                {
                    stopFailure = error;
                }
            }
            disposed = true;
            for (int index = 0; index < tasks.Length; index++)
            {
                tasks[index].Dispose();
            }
            if (stopFailure != null)
            {
                throw stopFailure;
            }
        }

        private int FindExpiredTask(List<int> pending)
        {
            int selected = pending[0];
            int remaining = tasks[selected].RemainingTimeoutMilliseconds;
            for (int index = 1; index < pending.Count; index++)
            {
                int candidate = pending[index];
                int candidateRemaining = tasks[candidate].RemainingTimeoutMilliseconds;
                if (candidateRemaining < remaining)
                {
                    selected = candidate;
                    remaining = candidateRemaining;
                }
            }
            return selected;
        }

        private void StopPending(List<int> pending, int exceptIndex)
        {
            Exception stopFailure = null;
            for (int index = 0; index < pending.Count; index++)
            {
                if (pending[index] != exceptIndex)
                {
                    try
                    {
                        tasks[pending[index]].Stop(false);
                    }
                    catch (Exception error)
                    {
                        if (stopFailure == null)
                        {
                            stopFailure = error;
                        }
                    }
                }
            }
            if (stopFailure != null)
            {
                throw new InvalidOperationException("Stopping a pending provisioning process failed.", stopFailure);
            }
        }

        private void ThrowIfDisposed()
        {
            if (disposed)
            {
                throw new ObjectDisposedException("ProvisioningProcessGroup");
            }
        }
    }

    internal sealed class ProvisioningProcessTask : IDisposable
    {
        private const int MaximumOutputBytes = 1024 * 1024;
        private const int CleanupTimeoutMilliseconds = 30000;
        private readonly ProvisioningProcessSpec spec;
        private readonly Stopwatch stopwatch;
        private readonly IntPtr processHandle;
        private readonly IntPtr jobHandle;
        private readonly IntPtr completionPort;
        private readonly Task<BoundedOutput> outputTask;
        private readonly Task<ProvisioningProcessResult> completion;
        private int stopRequested;
        private int timeoutRequested;
        private int disposed;

        private ProvisioningProcessTask(
            ProvisioningProcessSpec spec,
            Stopwatch stopwatch,
            IntPtr processHandle,
            IntPtr jobHandle,
            IntPtr completionPort,
            Task<BoundedOutput> outputTask)
        {
            this.spec = spec;
            this.stopwatch = stopwatch;
            this.processHandle = processHandle;
            this.jobHandle = jobHandle;
            this.completionPort = completionPort;
            this.outputTask = outputTask;
            completion = Task.Factory.StartNew<ProvisioningProcessResult>(
                new Func<ProvisioningProcessResult>(CompleteOwnedProcessTree),
                CancellationToken.None,
                TaskCreationOptions.LongRunning,
                TaskScheduler.Default);
        }

        internal Task<ProvisioningProcessResult> Completion
        {
            get { return completion; }
        }

        internal int RemainingTimeoutMilliseconds
        {
            get
            {
                long remaining = (long)spec.TimeoutMilliseconds - stopwatch.ElapsedMilliseconds;
                if (remaining <= 0)
                {
                    return 0;
                }
                return remaining > Int32.MaxValue ? Int32.MaxValue : (int)remaining;
            }
        }

        internal static ProvisioningProcessTask Start(ProvisioningProcessSpec spec)
        {
            ValidateSpec(spec);
            IntPtr job = IntPtr.Zero;
            IntPtr completionPort = IntPtr.Zero;
            IntPtr readPipe = IntPtr.Zero;
            IntPtr writePipe = IntPtr.Zero;
            IntPtr nullInput = IntPtr.Zero;
            NativeMethods.PROCESS_INFORMATION process = new NativeMethods.PROCESS_INFORMATION();
            bool processCreated = false;
            bool processAssigned = false;
            try
            {
                job = NativeMethods.CreateJobObject(IntPtr.Zero, null);
                NativeMethods.ThrowIfInvalid(job, "create provisioning process job object");
                NativeMethods.JOBOBJECT_EXTENDED_LIMIT_INFORMATION limits = new NativeMethods.JOBOBJECT_EXTENDED_LIMIT_INFORMATION();
                limits.BasicLimitInformation.LimitFlags = NativeMethods.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
                if (!NativeMethods.SetInformationJobObject(
                    job,
                    NativeMethods.JobObjectExtendedLimitInformation,
                    ref limits,
                    Marshal.SizeOf(typeof(NativeMethods.JOBOBJECT_EXTENDED_LIMIT_INFORMATION))))
                {
                    NativeMethods.ThrowLastError("configure provisioning process job object");
                }

                completionPort = NativeMethods.CreateIoCompletionPort(new IntPtr(-1), IntPtr.Zero, IntPtr.Zero, 1);
                NativeMethods.ThrowIfInvalid(completionPort, "create provisioning process completion port");
                NativeMethods.JOBOBJECT_ASSOCIATE_COMPLETION_PORT association = new NativeMethods.JOBOBJECT_ASSOCIATE_COMPLETION_PORT();
                association.CompletionKey = new IntPtr(ProvisioningProcess.ContractVersion);
                association.CompletionPort = completionPort;
                if (!NativeMethods.SetInformationJobObject(
                    job,
                    NativeMethods.JobObjectAssociateCompletionPortInformation,
                    ref association,
                    Marshal.SizeOf(typeof(NativeMethods.JOBOBJECT_ASSOCIATE_COMPLETION_PORT))))
                {
                    NativeMethods.ThrowLastError("associate provisioning process completion port");
                }

                NativeMethods.SECURITY_ATTRIBUTES security = new NativeMethods.SECURITY_ATTRIBUTES();
                security.Length = Marshal.SizeOf(typeof(NativeMethods.SECURITY_ATTRIBUTES));
                security.InheritHandle = true;
                if (!NativeMethods.CreatePipe(out readPipe, out writePipe, ref security, 65536))
                {
                    NativeMethods.ThrowLastError("create provisioning process output pipe");
                }
                if (!NativeMethods.SetHandleInformation(readPipe, NativeMethods.HANDLE_FLAG_INHERIT, 0))
                {
                    NativeMethods.ThrowLastError("protect provisioning process output reader");
                }
                nullInput = NativeMethods.CreateFile(
                    "NUL",
                    NativeMethods.GENERIC_READ,
                    NativeMethods.FILE_SHARE_READ | NativeMethods.FILE_SHARE_WRITE,
                    ref security,
                    NativeMethods.OPEN_EXISTING,
                    NativeMethods.FILE_ATTRIBUTE_NORMAL,
                    IntPtr.Zero);
                NativeMethods.ThrowIfInvalid(nullInput, "open provisioning process null input");

                NativeMethods.STARTUPINFO startup = new NativeMethods.STARTUPINFO();
                startup.cb = Marshal.SizeOf(typeof(NativeMethods.STARTUPINFO));
                startup.dwFlags = NativeMethods.STARTF_USESHOWWINDOW | NativeMethods.STARTF_USESTDHANDLES;
                startup.wShowWindow = NativeMethods.SW_HIDE;
                startup.hStdInput = nullInput;
                startup.hStdOutput = writePipe;
                startup.hStdError = writePipe;
                StringBuilder commandLine = new StringBuilder(BuildCommandLine(spec.FilePath, spec.Arguments));
                uint flags = NativeMethods.CREATE_SUSPENDED | NativeMethods.CREATE_NEW_CONSOLE | NativeMethods.CREATE_UNICODE_ENVIRONMENT;
                if (!NativeMethods.CreateProcess(
                    spec.FilePath,
                    commandLine,
                    IntPtr.Zero,
                    IntPtr.Zero,
                    true,
                    flags,
                    IntPtr.Zero,
                    spec.WorkingDirectory,
                    ref startup,
                    out process))
                {
                    NativeMethods.ThrowLastError("create provisioning process " + spec.Role);
                }
                processCreated = true;
                if (!NativeMethods.AssignProcessToJobObject(job, process.Process))
                {
                    NativeMethods.ThrowLastError("assign provisioning process tree " + spec.Role);
                }
                processAssigned = true;

                NativeMethods.CloseHandle(writePipe);
                writePipe = IntPtr.Zero;
                NativeMethods.CloseHandle(nullInput);
                nullInput = IntPtr.Zero;
                SafeFileHandle safeReadPipe = new SafeFileHandle(readPipe, true);
                readPipe = IntPtr.Zero;
                Task<BoundedOutput> outputTask = Task.Factory.StartNew(
                    delegate { return ReadOutput(safeReadPipe); },
                    CancellationToken.None,
                    TaskCreationOptions.LongRunning,
                    TaskScheduler.Default);
                Stopwatch stopwatch = Stopwatch.StartNew();
                ProvisioningProcessTask owned = new ProvisioningProcessTask(
                    spec,
                    stopwatch,
                    process.Process,
                    job,
                    completionPort,
                    outputTask);
                process.Process = IntPtr.Zero;
                job = IntPtr.Zero;
                completionPort = IntPtr.Zero;
                if (NativeMethods.ResumeThread(process.Thread) == UInt32.MaxValue)
                {
                    int resumeError = Marshal.GetLastWin32Error();
                    owned.Stop(false);
                    owned.Dispose();
                    throw new Win32Exception(resumeError, "resume provisioning process " + spec.Role);
                }
                NativeMethods.CloseHandle(process.Thread);
                process.Thread = IntPtr.Zero;
                return owned;
            }
            catch
            {
                if (processCreated && !processAssigned && process.Process != IntPtr.Zero)
                {
                    NativeMethods.TerminateProcess(process.Process, 1);
                }
                throw;
            }
            finally
            {
                NativeMethods.CloseIfValid(process.Thread);
                NativeMethods.CloseIfValid(process.Process);
                NativeMethods.CloseIfValid(nullInput);
                NativeMethods.CloseIfValid(writePipe);
                NativeMethods.CloseIfValid(readPipe);
                NativeMethods.CloseIfValid(completionPort);
                NativeMethods.CloseIfValid(job);
            }
        }

        internal ProvisioningProcessResult Complete()
        {
            if (Task.WaitAny(new Task[] { completion }, RemainingTimeoutMilliseconds) < 0)
            {
                Stop(true);
            }
            return WaitAfterStop();
        }

        internal ProvisioningProcessResult WaitAfterStop()
        {
            if (Task.WaitAny(new Task[] { completion }, CleanupTimeoutMilliseconds) < 0)
            {
                throw new TimeoutException("Provisioning process tree cleanup exceeded 30 seconds: " + spec.Role);
            }
            return completion.GetAwaiter().GetResult();
        }

        internal void Stop(bool timedOut)
        {
            if (timedOut)
            {
                Interlocked.Exchange(ref timeoutRequested, 1);
            }
            if (Interlocked.Exchange(ref stopRequested, 1) == 0 && !completion.IsCompleted)
            {
                if (!NativeMethods.TerminateJobObject(jobHandle, 1))
                {
                    int error = Marshal.GetLastWin32Error();
                    if (error != NativeMethods.ERROR_ACCESS_DENIED)
                    {
                        throw new Win32Exception(error, "terminate provisioning process tree " + spec.Role);
                    }
                }
            }
        }

        public void Dispose()
        {
            if (Interlocked.Exchange(ref disposed, 1) != 0)
            {
                return;
            }
            if (!completion.IsCompleted)
            {
                try
                {
                    Stop(false);
                    completion.Wait(CleanupTimeoutMilliseconds);
                }
                catch
                {
                }
            }
            NativeMethods.CloseIfValid(processHandle);
            NativeMethods.CloseIfValid(completionPort);
            NativeMethods.CloseIfValid(jobHandle);
        }

        private ProvisioningProcessResult CompleteOwnedProcessTree()
        {
            bool sawProcess = false;
            while (true)
            {
                uint message;
                UIntPtr completionKey;
                IntPtr processID;
                if (!NativeMethods.GetQueuedCompletionStatus(
                    completionPort,
                    out message,
                    out completionKey,
                    out processID,
                    NativeMethods.INFINITE))
                {
                    NativeMethods.ThrowLastError("wait for provisioning process tree " + spec.Role);
                }
                if (message == NativeMethods.JOB_OBJECT_MSG_NEW_PROCESS)
                {
                    sawProcess = true;
                }
                if (message == NativeMethods.JOB_OBJECT_MSG_ACTIVE_PROCESS_ZERO && sawProcess)
                {
                    break;
                }
            }
            if (NativeMethods.WaitForSingleObject(processHandle, CleanupTimeoutMilliseconds) != NativeMethods.WAIT_OBJECT_0)
            {
                throw new TimeoutException("Provisioning root process did not reach terminal state: " + spec.Role);
            }
            uint exitCode;
            if (!NativeMethods.GetExitCodeProcess(processHandle, out exitCode))
            {
                NativeMethods.ThrowLastError("read provisioning process exit code " + spec.Role);
            }
            BoundedOutput output = outputTask.GetAwaiter().GetResult();
            stopwatch.Stop();
            bool timedOut = Interlocked.CompareExchange(ref timeoutRequested, 0, 0) != 0;
            bool stopped = Interlocked.CompareExchange(ref stopRequested, 0, 0) != 0 && !timedOut;
            int signedExitCode = unchecked((int)exitCode);
            return new ProvisioningProcessResult
            {
                Role = spec.Role,
                ExitCode = signedExitCode,
                Output = output.Text,
                OutputBytes = output.TotalBytes,
                OutputTruncated = output.Truncated,
                ElapsedMilliseconds = stopwatch.ElapsedMilliseconds,
                TimedOut = timedOut,
                Stopped = stopped,
                Succeeded = !timedOut && !stopped && AcceptsExitCode(spec.SuccessExitCodes, signedExitCode)
            };
        }

        private static void ValidateSpec(ProvisioningProcessSpec spec)
        {
            if (spec == null)
            {
                throw new ArgumentNullException("spec");
            }
            if (String.IsNullOrWhiteSpace(spec.Role) || spec.Role.Length > 256)
            {
                throw new ArgumentException("Provisioning process role is empty or too long.", "spec");
            }
            if (String.IsNullOrWhiteSpace(spec.FilePath) || !Path.IsPathRooted(spec.FilePath) || !File.Exists(spec.FilePath))
            {
                throw new ArgumentException("Provisioning process executable must be one existing absolute file: " + spec.FilePath, "spec");
            }
            if (String.IsNullOrWhiteSpace(spec.WorkingDirectory) || !Path.IsPathRooted(spec.WorkingDirectory) || !Directory.Exists(spec.WorkingDirectory))
            {
                throw new ArgumentException("Provisioning process working directory must be one existing absolute directory.", "spec");
            }
            if (spec.Arguments == null || spec.Arguments.Length > 256)
            {
                throw new ArgumentException("Provisioning process argument count is invalid.", "spec");
            }
            for (int index = 0; index < spec.Arguments.Length; index++)
            {
                if (spec.Arguments[index] == null || spec.Arguments[index].IndexOf('\0') >= 0)
                {
                    throw new ArgumentException("Provisioning process argument is null or contains NUL.", "spec");
                }
            }
            if (spec.TimeoutMilliseconds < 1000 || spec.TimeoutMilliseconds > 7200000)
            {
                throw new ArgumentException("Provisioning process timeout must be between 1 and 7200 seconds.", "spec");
            }
            if (spec.SuccessExitCodes == null || spec.SuccessExitCodes.Length == 0 || spec.SuccessExitCodes.Length > 8)
            {
                throw new ArgumentException("Provisioning process success exit-code contract is invalid.", "spec");
            }
            HashSet<int> exitCodes = new HashSet<int>();
            for (int index = 0; index < spec.SuccessExitCodes.Length; index++)
            {
                int exitCode = spec.SuccessExitCodes[index];
                if (exitCode < 0 || exitCode > 65535 || !exitCodes.Add(exitCode))
                {
                    throw new ArgumentException("Provisioning process success exit-code contract is invalid.", "spec");
                }
            }
        }

        private static bool AcceptsExitCode(int[] accepted, int value)
        {
            for (int index = 0; index < accepted.Length; index++)
            {
                if (accepted[index] == value)
                {
                    return true;
                }
            }
            return false;
        }

        private static string BuildCommandLine(string filePath, string[] arguments)
        {
            StringBuilder commandLine = new StringBuilder(QuoteArgument(filePath));
            for (int index = 0; index < arguments.Length; index++)
            {
                commandLine.Append(' ');
                commandLine.Append(QuoteArgument(arguments[index]));
            }
            if (commandLine.Length > 32767)
            {
                throw new ArgumentException("Provisioning process command line exceeds 32767 characters.");
            }
            return commandLine.ToString();
        }

        private static string QuoteArgument(string argument)
        {
            if (argument.Length != 0 && argument.IndexOfAny(new char[] { ' ', '\t', '\n', '\v', '"' }) < 0)
            {
                return argument;
            }
            StringBuilder quoted = new StringBuilder();
            quoted.Append('"');
            int backslashes = 0;
            for (int index = 0; index < argument.Length; index++)
            {
                char character = argument[index];
                if (character == '\\')
                {
                    backslashes++;
                    continue;
                }
                if (character == '"')
                {
                    quoted.Append('\\', backslashes * 2 + 1);
                    quoted.Append('"');
                    backslashes = 0;
                    continue;
                }
                quoted.Append('\\', backslashes);
                backslashes = 0;
                quoted.Append(character);
            }
            quoted.Append('\\', backslashes * 2);
            quoted.Append('"');
            return quoted.ToString();
        }

        private static BoundedOutput ReadOutput(SafeFileHandle handle)
        {
            using (handle)
            using (FileStream stream = new FileStream(handle, FileAccess.Read, 65536, false))
            {
                BoundedOutputBuffer output = new BoundedOutputBuffer(MaximumOutputBytes);
                byte[] buffer = new byte[65536];
                int read;
                while ((read = stream.Read(buffer, 0, buffer.Length)) != 0)
                {
                    output.Append(buffer, read);
                }
                return output.Complete();
            }
        }
    }

    internal sealed class BoundedOutput
    {
        internal string Text;
        internal long TotalBytes;
        internal bool Truncated;
    }

    internal sealed class BoundedOutputBuffer
    {
        private readonly int maximumBytes;
        private readonly int headLimit;
        private readonly int tailLimit;
        private readonly MemoryStream head;
        private readonly byte[] tail;
        private int tailCount;
        private int tailOffset;
        private long totalBytes;

        internal BoundedOutputBuffer(int maximumBytes)
        {
            this.maximumBytes = maximumBytes;
            headLimit = maximumBytes / 2;
            tailLimit = maximumBytes - headLimit;
            head = new MemoryStream(headLimit);
            tail = new byte[tailLimit];
        }

        internal void Append(byte[] source, int count)
        {
            totalBytes += count;
            int offset = 0;
            if (head.Length < headLimit)
            {
                int copy = Math.Min(count, headLimit - (int)head.Length);
                head.Write(source, 0, copy);
                offset += copy;
            }
            for (; offset < count; offset++)
            {
                if (tailCount < tailLimit)
                {
                    tail[tailCount++] = source[offset];
                }
                else
                {
                    tail[tailOffset] = source[offset];
                    tailOffset = (tailOffset + 1) % tailLimit;
                }
            }
        }

        internal BoundedOutput Complete()
        {
            MemoryStream selected = new MemoryStream(maximumBytes + 256);
            byte[] headBytes = head.ToArray();
            selected.Write(headBytes, 0, headBytes.Length);
            bool truncated = totalBytes > maximumBytes;
            if (truncated)
            {
                byte[] marker = Encoding.UTF8.GetBytes(String.Format(
                    "\r\n... output truncated; original bytes: {0} ...\r\n",
                    totalBytes));
                selected.Write(marker, 0, marker.Length);
            }
            if (tailCount != 0)
            {
                if (tailCount < tailLimit || tailOffset == 0)
                {
                    selected.Write(tail, 0, tailCount);
                }
                else
                {
                    selected.Write(tail, tailOffset, tailLimit - tailOffset);
                    selected.Write(tail, 0, tailOffset);
                }
            }
            return new BoundedOutput
            {
                Text = new UTF8Encoding(false, false).GetString(selected.ToArray()),
                TotalBytes = totalBytes,
                Truncated = truncated
            };
        }
    }

    internal static class NativeMethods
    {
        internal const int JobObjectAssociateCompletionPortInformation = 7;
        internal const int JobObjectExtendedLimitInformation = 9;
        internal const uint JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE = 0x00002000;
        internal const uint JOB_OBJECT_MSG_ACTIVE_PROCESS_ZERO = 4;
        internal const uint JOB_OBJECT_MSG_NEW_PROCESS = 6;
        internal const uint CREATE_SUSPENDED = 0x00000004;
        internal const uint CREATE_NEW_CONSOLE = 0x00000010;
        internal const uint CREATE_UNICODE_ENVIRONMENT = 0x00000400;
        internal const int STARTF_USESHOWWINDOW = 0x00000001;
        internal const int STARTF_USESTDHANDLES = 0x00000100;
        internal const short SW_HIDE = 0;
        internal const uint HANDLE_FLAG_INHERIT = 0x00000001;
        internal const uint GENERIC_READ = 0x80000000;
        internal const uint FILE_SHARE_READ = 0x00000001;
        internal const uint FILE_SHARE_WRITE = 0x00000002;
        internal const uint OPEN_EXISTING = 3;
        internal const uint FILE_ATTRIBUTE_NORMAL = 0x00000080;
        internal const uint INFINITE = 0xffffffff;
        internal const uint WAIT_OBJECT_0 = 0;
        internal const int ERROR_ACCESS_DENIED = 5;

        [StructLayout(LayoutKind.Sequential)]
        internal struct SECURITY_ATTRIBUTES
        {
            internal int Length;
            internal IntPtr SecurityDescriptor;
            [MarshalAs(UnmanagedType.Bool)]
            internal bool InheritHandle;
        }

        [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
        internal struct STARTUPINFO
        {
            internal int cb;
            internal string lpReserved;
            internal string lpDesktop;
            internal string lpTitle;
            internal int dwX;
            internal int dwY;
            internal int dwXSize;
            internal int dwYSize;
            internal int dwXCountChars;
            internal int dwYCountChars;
            internal int dwFillAttribute;
            internal int dwFlags;
            internal short wShowWindow;
            internal short cbReserved2;
            internal IntPtr lpReserved2;
            internal IntPtr hStdInput;
            internal IntPtr hStdOutput;
            internal IntPtr hStdError;
        }

        [StructLayout(LayoutKind.Sequential)]
        internal struct PROCESS_INFORMATION
        {
            internal IntPtr Process;
            internal IntPtr Thread;
            internal int ProcessID;
            internal int ThreadID;
        }

        [StructLayout(LayoutKind.Sequential)]
        internal struct JOBOBJECT_BASIC_LIMIT_INFORMATION
        {
            internal long PerProcessUserTimeLimit;
            internal long PerJobUserTimeLimit;
            internal uint LimitFlags;
            internal UIntPtr MinimumWorkingSetSize;
            internal UIntPtr MaximumWorkingSetSize;
            internal uint ActiveProcessLimit;
            internal UIntPtr Affinity;
            internal uint PriorityClass;
            internal uint SchedulingClass;
        }

        [StructLayout(LayoutKind.Sequential)]
        internal struct IO_COUNTERS
        {
            internal ulong ReadOperationCount;
            internal ulong WriteOperationCount;
            internal ulong OtherOperationCount;
            internal ulong ReadTransferCount;
            internal ulong WriteTransferCount;
            internal ulong OtherTransferCount;
        }

        [StructLayout(LayoutKind.Sequential)]
        internal struct JOBOBJECT_EXTENDED_LIMIT_INFORMATION
        {
            internal JOBOBJECT_BASIC_LIMIT_INFORMATION BasicLimitInformation;
            internal IO_COUNTERS IoInfo;
            internal UIntPtr ProcessMemoryLimit;
            internal UIntPtr JobMemoryLimit;
            internal UIntPtr PeakProcessMemoryUsed;
            internal UIntPtr PeakJobMemoryUsed;
        }

        [StructLayout(LayoutKind.Sequential)]
        internal struct JOBOBJECT_ASSOCIATE_COMPLETION_PORT
        {
            internal IntPtr CompletionKey;
            internal IntPtr CompletionPort;
        }

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        internal static extern IntPtr CreateJobObject(IntPtr securityAttributes, string name);

        [DllImport("kernel32.dll", SetLastError = true)]
        [return: MarshalAs(UnmanagedType.Bool)]
        internal static extern bool SetInformationJobObject(
            IntPtr job,
            int informationClass,
            ref JOBOBJECT_EXTENDED_LIMIT_INFORMATION information,
            int informationLength);

        [DllImport("kernel32.dll", SetLastError = true)]
        [return: MarshalAs(UnmanagedType.Bool)]
        internal static extern bool SetInformationJobObject(
            IntPtr job,
            int informationClass,
            ref JOBOBJECT_ASSOCIATE_COMPLETION_PORT information,
            int informationLength);

        [DllImport("kernel32.dll", SetLastError = true)]
        internal static extern IntPtr CreateIoCompletionPort(
            IntPtr fileHandle,
            IntPtr existingCompletionPort,
            IntPtr completionKey,
            uint concurrentThreads);

        [DllImport("kernel32.dll", SetLastError = true)]
        [return: MarshalAs(UnmanagedType.Bool)]
        internal static extern bool GetQueuedCompletionStatus(
            IntPtr completionPort,
            out uint numberOfBytes,
            out UIntPtr completionKey,
            out IntPtr overlapped,
            uint milliseconds);

        [DllImport("kernel32.dll", SetLastError = true)]
        [return: MarshalAs(UnmanagedType.Bool)]
        internal static extern bool CreatePipe(
            out IntPtr readPipe,
            out IntPtr writePipe,
            ref SECURITY_ATTRIBUTES securityAttributes,
            int size);

        [DllImport("kernel32.dll", SetLastError = true)]
        [return: MarshalAs(UnmanagedType.Bool)]
        internal static extern bool SetHandleInformation(IntPtr handle, uint mask, uint flags);

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        internal static extern IntPtr CreateFile(
            string fileName,
            uint desiredAccess,
            uint shareMode,
            ref SECURITY_ATTRIBUTES securityAttributes,
            uint creationDisposition,
            uint flagsAndAttributes,
            IntPtr templateFile);

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        [return: MarshalAs(UnmanagedType.Bool)]
        internal static extern bool CreateProcess(
            string applicationName,
            StringBuilder commandLine,
            IntPtr processAttributes,
            IntPtr threadAttributes,
            [MarshalAs(UnmanagedType.Bool)] bool inheritHandles,
            uint creationFlags,
            IntPtr environment,
            string currentDirectory,
            ref STARTUPINFO startupInfo,
            out PROCESS_INFORMATION processInformation);

        [DllImport("kernel32.dll", SetLastError = true)]
        [return: MarshalAs(UnmanagedType.Bool)]
        internal static extern bool AssignProcessToJobObject(IntPtr job, IntPtr process);

        [DllImport("kernel32.dll", SetLastError = true)]
        internal static extern uint ResumeThread(IntPtr thread);

        [DllImport("kernel32.dll", SetLastError = true)]
        [return: MarshalAs(UnmanagedType.Bool)]
        internal static extern bool TerminateJobObject(IntPtr job, uint exitCode);

        [DllImport("kernel32.dll", SetLastError = true)]
        [return: MarshalAs(UnmanagedType.Bool)]
        internal static extern bool TerminateProcess(IntPtr process, uint exitCode);

        [DllImport("kernel32.dll", SetLastError = true)]
        internal static extern uint WaitForSingleObject(IntPtr handle, int milliseconds);

        [DllImport("kernel32.dll", SetLastError = true)]
        [return: MarshalAs(UnmanagedType.Bool)]
        internal static extern bool GetExitCodeProcess(IntPtr process, out uint exitCode);

        [DllImport("kernel32.dll", SetLastError = true)]
        [return: MarshalAs(UnmanagedType.Bool)]
        internal static extern bool CloseHandle(IntPtr handle);

        internal static void ThrowLastError(string operation)
        {
            throw new Win32Exception(Marshal.GetLastWin32Error(), operation);
        }

        internal static void ThrowIfInvalid(IntPtr handle, string operation)
        {
            if (handle == IntPtr.Zero || handle == new IntPtr(-1))
            {
                ThrowLastError(operation);
            }
        }

        internal static void CloseIfValid(IntPtr handle)
        {
            if (handle != IntPtr.Zero && handle != new IntPtr(-1))
            {
                CloseHandle(handle);
            }
        }
    }
}
