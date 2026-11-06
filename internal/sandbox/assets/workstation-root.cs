// Direct setup control state is writable only by elevated administrators/System.
using System;
using System.Collections.Generic;
using System.IO;
using System.Security.AccessControl;
using System.Security.Principal;

namespace HerdrWorkstation
{
    public static class Directories
    {
        private static string Rules(DirectorySecurity security)
        {
            var entries = new List<string>();
            foreach (FileSystemAccessRule rule in security.GetAccessRules(true, true, typeof(SecurityIdentifier)))
            {
                entries.Add(rule.IdentityReference.Value + ":" + (int)rule.FileSystemRights + ":" +
                    (int)rule.AccessControlType + ":" + (int)rule.InheritanceFlags + ":" +
                    (int)rule.PropagationFlags + ":" + rule.IsInherited);
            }
            entries.Sort(StringComparer.Ordinal);
            return String.Join("|", entries.ToArray());
        }

        public static void Ensure(string path)
        {
            using (WindowsIdentity identity = WindowsIdentity.GetCurrent())
            {
                if (!new WindowsPrincipal(identity).IsInRole(WindowsBuiltInRole.Administrator))
                    throw new UnauthorizedAccessException("Direct setup requires elevation as the intended Windows user.");
                if (identity.User == null || identity.User.IsWellKnown(WellKnownSidType.LocalSystemSid) ||
                    identity.User.IsWellKnown(WellKnownSidType.LocalServiceSid) || identity.User.IsWellKnown(WellKnownSidType.NetworkServiceSid))
                    throw new UnauthorizedAccessException("Service-account provisioning is not supported.");
                string common = Path.GetFullPath(Environment.GetFolderPath(Environment.SpecialFolder.CommonApplicationData)).TrimEnd('\\');
                string root = Path.GetFullPath(path).TrimEnd('\\');
                if (!root.StartsWith(common + "\\", StringComparison.OrdinalIgnoreCase))
                    throw new InvalidOperationException("Setup control state must be below the system ProgramData directory.");
                string current = common;
                string[] parts = root.Substring(common.Length + 1).Split('\\');
                for (int index = 0; index < parts.Length; index++)
                {
                    if (parts[index].Length == 0 || parts[index].EndsWith(".") || parts[index].EndsWith(" "))
                        throw new InvalidOperationException("Setup directory has an unsafe path component.");
                    current = Path.Combine(current, parts[index]);
                    string reader = index == parts.Length - 1 ? identity.User.Value : "BU";
                    var expected = new DirectorySecurity();
                    // OWNER RIGHTS prevents a creator's implicit WRITE_DAC from
                    // weakening inherited control-file protection under split tokens.
                    expected.SetSecurityDescriptorSddlForm("O:BAG:BAD:P(A;OICI;FA;;;BA)(A;OICI;FA;;;SY)(A;OICI;0x1200a9;;;" + reader + ")(A;OICI;RC;;;OW)");
                    if (!Directory.Exists(current)) Directory.CreateDirectory(current, expected);
                    if ((File.GetAttributes(current) & FileAttributes.ReparsePoint) != 0)
                        throw new InvalidOperationException("Setup directory is a reparse point.");
                    DirectorySecurity actual = Directory.GetAccessControl(current, AccessControlSections.Access | AccessControlSections.Owner);
                    string owner = actual.GetOwner(typeof(SecurityIdentifier)).Value;
                    if ((owner != "S-1-5-32-544" && owner != "S-1-5-18") || !actual.AreAccessRulesProtected || Rules(actual) != Rules(expected))
                        throw new UnauthorizedAccessException("Existing setup directory has an unknown or writable security boundary; it was preserved.");
                }
            }
        }
    }
}
