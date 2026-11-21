using System;
using System.Runtime.InteropServices;

// Independent fresh-process probe. Never request a refreshed collection first.
public static class FontCollectionProbe
{
    [DllImport("dwrite.dll", PreserveSig = true)]
    private static extern int DWriteCreateFactory(uint kind, ref Guid iid, out IntPtr factory);

    [UnmanagedFunctionPointer(CallingConvention.StdCall)]
    private delegate int GetCollection(IntPtr self, out IntPtr collection,
        [MarshalAs(UnmanagedType.Bool)] bool checkForUpdates);

    [UnmanagedFunctionPointer(CallingConvention.StdCall, CharSet = CharSet.Unicode)]
    private delegate int FindFamily(IntPtr self, [MarshalAs(UnmanagedType.LPWStr)] string name,
        out uint index, [MarshalAs(UnmanagedType.Bool)] out bool exists);

    private static Delegate Method(IntPtr instance, int slot, Type type)
    {
        return Marshal.GetDelegateForFunctionPointer(
            Marshal.ReadIntPtr(Marshal.ReadIntPtr(instance), slot * IntPtr.Size), type);
    }

    public static int Main()
    {
        try
        {
            foreach (uint kind in new uint[] { 0, 1 })
            {
                Guid iid = new Guid("b859ee5a-d838-4b5b-a2e8-1adc7d93db48");
                IntPtr factory;
                Marshal.ThrowExceptionForHR(DWriteCreateFactory(kind, ref iid, out factory));
                try
                {
                    IntPtr collection;
                    Marshal.ThrowExceptionForHR(((GetCollection)Method(factory, 3, typeof(GetCollection)))(
                        factory, out collection, false));
                    try
                    {
                        foreach (string name in new string[] { "GeistMono Nerd Font", "GeistMono NF" })
                        {
                            uint index;
                            bool exists;
                            Marshal.ThrowExceptionForHR(((FindFamily)Method(collection, 5, typeof(FindFamily)))(
                                collection, name, out index, out exists));
                            Console.WriteLine("factory={0} checkForUpdates=FALSE family={1} found={2}", kind, name, exists);
                            if (!exists) return 1;
                        }
                    }
                    finally { Marshal.Release(collection); }
                }
                finally { Marshal.Release(factory); }
            }
            return 0;
        }
        catch (Exception error) { Console.Error.WriteLine(error); return 1; }
    }
}
