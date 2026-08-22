// herdr-sandbox-tradingview-cookie-sync-contract: 3
using System;
using System.Collections.Generic;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;

namespace HerdrSandbox
{
    public sealed class TradingViewCookieRecord
    {
        public long CreationUtc { get; set; }
        public string HostKey { get; set; }
        public string TopFrameSiteKey { get; set; }
        public string Name { get; set; }
        public string Value { get; set; }
        public string Path { get; set; }
        public long ExpiresUtc { get; set; }
        public bool Secure { get; set; }
        public bool HttpOnly { get; set; }
        public long LastAccessUtc { get; set; }
        public bool HasExpires { get; set; }
        public bool Persistent { get; set; }
        public int Priority { get; set; }
        public int SameSite { get; set; }
        public int SourceScheme { get; set; }
        public int SourcePort { get; set; }
        public long LastUpdateUtc { get; set; }
        public int SourceType { get; set; }
        public bool CrossSiteAncestor { get; set; }
    }

    public static class TradingViewCookieSync
    {
        private const int SqliteOk = 0;
        private const int SqliteRow = 100;
        private const int SqliteDone = 101;
        private const int SqliteOpenReadWrite = 0x00000002;
        private const int SqliteOpenCreate = 0x00000004;
        private const int SqliteOpenNoFollow = 0x01000000;
        private const int MaximumAuthenticationCookies = 4;
        private const int MaximumValueBytes = 16 * 1024;
        private static readonly IntPtr SqliteTransient = new IntPtr(-1);

        private const string CookieColumns =
            "creation_utc, host_key, top_frame_site_key, name, value, encrypted_value, path, " +
            "expires_utc, is_secure, is_httponly, last_access_utc, has_expires, is_persistent, " +
            "priority, samesite, source_scheme, source_port, last_update_utc, source_type, " +
            "has_cross_site_ancestor";

        private const string SessionRows =
            "(name='sessionid' OR name='sessionid_sign') AND top_frame_site_key='' AND " +
            "(lower(host_key)='tradingview.com' OR lower(host_key)='.tradingview.com' OR " +
            "lower(host_key) LIKE '%.tradingview.com')";

        private const string PreferenceRows =
            "(name='cookiesSettings' OR name='cookiePrivacyPreferenceBannerProduction') AND " +
            "lower(host_key)='.tradingview.com' AND top_frame_site_key='' AND path='/' AND " +
            "source_scheme=2 AND source_port=443";

        private const string OwnedRows = "(" + SessionRows + ") OR (" + PreferenceRows + ")";
        private const string AllowedRows = "(" + OwnedRows + ") AND has_cross_site_ancestor=1";

        public static int Import(string destination, TradingViewCookieRecord[] cookies)
        {
            if (String.IsNullOrWhiteSpace(destination) || !Path.IsPathRooted(destination))
            {
                throw new ArgumentException("TradingView cookie destination must be absolute.", "destination");
            }
            if (cookies == null || cookies.Length > MaximumAuthenticationCookies)
            {
                throw new ArgumentException("TradingView cookie input count is invalid.", "cookies");
            }
            ValidateCookies(cookies);
            TradingViewCookieRecord[] desired = WithEssentialOnlyPreferences(cookies);

            string parent = Path.GetDirectoryName(destination);
            if (String.IsNullOrWhiteSpace(parent))
            {
                throw new ArgumentException("TradingView cookie destination parent is invalid.", "destination");
            }
            Directory.CreateDirectory(parent);

            IntPtr database = IntPtr.Zero;
            try
            {
                database = Open(destination);
                Check(sqlite3_busy_timeout(database, 5000), "configure SQLite busy timeout");
                EnsureSchema(database);
                Exec(database, "BEGIN IMMEDIATE");
                bool committed = false;
                try
                {
                    Exec(database, "DELETE FROM cookies WHERE " + OwnedRows);
                    InsertCookies(database, desired);
                    VerifyCookies(database, desired);
                    Exec(database, "COMMIT");
                    committed = true;
                }
                finally
                {
                    if (!committed)
                    {
                        try { Exec(database, "ROLLBACK"); } catch { }
                    }
                }
            }
            finally
            {
                if (database != IntPtr.Zero)
                {
                    int result = sqlite3_close_v2(database);
                    if (result != SqliteOk)
                    {
                        throw new InvalidOperationException("close TradingView cookie database failed with SQLite code " + result + ".");
                    }
                }
            }
            return cookies.Length;
        }

        private static TradingViewCookieRecord[] WithEssentialOnlyPreferences(TradingViewCookieRecord[] authentication)
        {
            DateTime nowTime = DateTime.UtcNow;
            long now = nowTime.ToFileTimeUtc() / 10;
            long expires = nowTime.AddYears(1).ToFileTimeUtc() / 10;
            TradingViewCookieRecord[] desired = new TradingViewCookieRecord[authentication.Length + 2];
            Array.Copy(authentication, desired, authentication.Length);
            desired[authentication.Length] = Preference(
                "cookiesSettings", "{\"analytics\":false,\"advertising\":false}", now, expires);
            desired[authentication.Length + 1] = Preference(
                "cookiePrivacyPreferenceBannerProduction", "reject", now, expires);
            Array.Sort(desired, delegate(TradingViewCookieRecord left, TradingViewCookieRecord right)
            {
                int comparison = StringComparer.OrdinalIgnoreCase.Compare(left.HostKey, right.HostKey);
                if (comparison != 0) { return comparison; }
                comparison = StringComparer.Ordinal.Compare(left.Name, right.Name);
                if (comparison != 0) { return comparison; }
                comparison = StringComparer.Ordinal.Compare(left.Path, right.Path);
                if (comparison != 0) { return comparison; }
                comparison = left.SourceScheme.CompareTo(right.SourceScheme);
                return comparison != 0 ? comparison : left.SourcePort.CompareTo(right.SourcePort);
            });
            return desired;
        }

        private static TradingViewCookieRecord Preference(string name, string value, long now, long expires)
        {
            return new TradingViewCookieRecord
            {
                CreationUtc = now,
                HostKey = ".tradingview.com",
                TopFrameSiteKey = String.Empty,
                Name = name,
                Value = value,
                Path = "/",
                ExpiresUtc = expires,
                Secure = false,
                HttpOnly = false,
                LastAccessUtc = now,
                HasExpires = true,
                Persistent = true,
                Priority = 1,
                SameSite = -1,
                SourceScheme = 2,
                SourcePort = 443,
                LastUpdateUtc = now,
                SourceType = 1,
                CrossSiteAncestor = true,
            };
        }

        private static IntPtr Open(string path)
        {
            byte[] encoded = Encoding.UTF8.GetBytes(path + "\0");
            GCHandle pinned = GCHandle.Alloc(encoded, GCHandleType.Pinned);
            try
            {
                IntPtr database;
                int result = sqlite3_open_v2(pinned.AddrOfPinnedObject(), out database,
                    SqliteOpenReadWrite | SqliteOpenCreate | SqliteOpenNoFollow, IntPtr.Zero);
                if (result != SqliteOk || database == IntPtr.Zero)
                {
                    if (database != IntPtr.Zero) { sqlite3_close_v2(database); }
                    throw new InvalidOperationException("open TradingView cookie database failed with SQLite code " + result + ".");
                }
                return database;
            }
            finally
            {
                pinned.Free();
                Array.Clear(encoded, 0, encoded.Length);
            }
        }

        private static void EnsureSchema(IntPtr database)
        {
            int metaCount = ScalarInt(database, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='meta'");
            int cookieCount = ScalarInt(database, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='cookies'");
            if (metaCount == 0 && cookieCount == 0)
            {
                Exec(database,
                    "BEGIN IMMEDIATE;" +
                    "CREATE TABLE meta(key LONGVARCHAR NOT NULL UNIQUE PRIMARY KEY, value LONGVARCHAR);" +
                    "INSERT INTO meta(key,value) VALUES('mmap_status','-1'),('version','24'),('last_compatible_version','24');" +
                    "CREATE TABLE cookies(" +
                    "creation_utc INTEGER NOT NULL,host_key TEXT NOT NULL,top_frame_site_key TEXT NOT NULL," +
                    "name TEXT NOT NULL,value TEXT NOT NULL,encrypted_value BLOB NOT NULL,path TEXT NOT NULL," +
                    "expires_utc INTEGER NOT NULL,is_secure INTEGER NOT NULL,is_httponly INTEGER NOT NULL," +
                    "last_access_utc INTEGER NOT NULL,has_expires INTEGER NOT NULL,is_persistent INTEGER NOT NULL," +
                    "priority INTEGER NOT NULL,samesite INTEGER NOT NULL,source_scheme INTEGER NOT NULL," +
                    "source_port INTEGER NOT NULL,last_update_utc INTEGER NOT NULL,source_type INTEGER NOT NULL," +
                    "has_cross_site_ancestor INTEGER NOT NULL);" +
                    "CREATE UNIQUE INDEX cookies_unique_index ON cookies(host_key, top_frame_site_key, " +
                    "has_cross_site_ancestor, name, path, source_scheme, source_port);" +
                    "COMMIT;");
            }
            else if (metaCount != 1 || cookieCount != 1)
            {
                throw new InvalidOperationException("TradingView cookie database has an incomplete schema.");
            }

            if (ScalarText(database, "SELECT value FROM meta WHERE key='version'") != "24" ||
                ScalarText(database, "SELECT value FROM meta WHERE key='last_compatible_version'") != "24")
            {
                throw new InvalidOperationException("TradingView cookie database schema version is unsupported.");
            }
            string[] expected = CookieColumns.Split(new string[] { ", " }, StringSplitOptions.None);
            List<string> actual = new List<string>();
            IntPtr statement = Prepare(database, "PRAGMA table_info(cookies)");
            try
            {
                while (true)
                {
                    int result = sqlite3_step(statement);
                    if (result == SqliteDone) { break; }
                    if (result != SqliteRow) { Check(result, "inspect TradingView cookie columns"); }
                    actual.Add(ColumnText(statement, 1, 128));
                }
            }
            finally
            {
                Finalize(statement);
            }
            if (actual.Count != expected.Length)
            {
                throw new InvalidOperationException("TradingView cookie database column count is unsupported.");
            }
            for (int index = 0; index < expected.Length; index++)
            {
                if (!String.Equals(actual[index], expected[index], StringComparison.Ordinal))
                {
                    throw new InvalidOperationException("TradingView cookie database columns are unsupported.");
                }
            }
        }

        private static void InsertCookies(IntPtr database, TradingViewCookieRecord[] cookies)
        {
            string parameters = "?, ?, ?, ?, ?, zeroblob(0), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?";
            IntPtr statement = Prepare(database, "INSERT OR REPLACE INTO cookies(" + CookieColumns + ") VALUES(" + parameters + ")");
            try
            {
                foreach (TradingViewCookieRecord cookie in cookies)
                {
                    BindInt64(statement, 1, cookie.CreationUtc);
                    BindText(statement, 2, cookie.HostKey);
                    BindText(statement, 3, cookie.TopFrameSiteKey);
                    BindText(statement, 4, cookie.Name);
                    BindText(statement, 5, cookie.Value);
                    BindText(statement, 6, cookie.Path);
                    BindInt64(statement, 7, cookie.ExpiresUtc);
                    BindInt(statement, 8, cookie.Secure ? 1 : 0);
                    BindInt(statement, 9, cookie.HttpOnly ? 1 : 0);
                    BindInt64(statement, 10, cookie.LastAccessUtc);
                    BindInt(statement, 11, cookie.HasExpires ? 1 : 0);
                    BindInt(statement, 12, cookie.Persistent ? 1 : 0);
                    BindInt(statement, 13, cookie.Priority);
                    BindInt(statement, 14, cookie.SameSite);
                    BindInt(statement, 15, cookie.SourceScheme);
                    BindInt(statement, 16, cookie.SourcePort);
                    BindInt64(statement, 17, cookie.LastUpdateUtc);
                    BindInt(statement, 18, cookie.SourceType);
                    BindInt(statement, 19, cookie.CrossSiteAncestor ? 1 : 0);
                    Check(sqlite3_step(statement), "insert TradingView session cookie", SqliteDone);
                    Check(sqlite3_reset(statement), "reset TradingView session cookie insert");
                    Check(sqlite3_clear_bindings(statement), "clear TradingView session cookie bindings");
                }
            }
            finally
            {
                Finalize(statement);
            }
        }

        private static void VerifyCookies(IntPtr database, TradingViewCookieRecord[] expected)
        {
            List<TradingViewCookieRecord> actual = new List<TradingViewCookieRecord>();
            IntPtr statement = Prepare(database, "SELECT " + CookieColumns + " FROM cookies WHERE " + AllowedRows +
                " ORDER BY lower(host_key), name, path, source_scheme, source_port");
            try
            {
                while (true)
                {
                    int result = sqlite3_step(statement);
                    if (result == SqliteDone) { break; }
                    if (result != SqliteRow) { Check(result, "verify TradingView session cookies"); }
                    if (sqlite3_column_bytes(statement, 5) != 0)
                    {
                        throw new InvalidOperationException("TradingView session cookie was not stored in portable plaintext form.");
                    }
                    actual.Add(ReadCookie(statement));
                }
            }
            finally
            {
                Finalize(statement);
            }
            if (actual.Count != expected.Length)
            {
                throw new InvalidOperationException("TradingView session cookie verification count does not match.");
            }
            for (int index = 0; index < expected.Length; index++)
            {
                if (!CookiesEqual(expected[index], actual[index]))
                {
                    throw new InvalidOperationException("TradingView session cookie verification failed.");
                }
            }
        }

        private static TradingViewCookieRecord ReadCookie(IntPtr statement)
        {
            return new TradingViewCookieRecord
            {
                CreationUtc = sqlite3_column_int64(statement, 0),
                HostKey = ColumnText(statement, 1, 512),
                TopFrameSiteKey = ColumnText(statement, 2, 4096),
                Name = ColumnText(statement, 3, 128),
                Value = ColumnText(statement, 4, MaximumValueBytes),
                Path = ColumnText(statement, 6, 1024),
                ExpiresUtc = sqlite3_column_int64(statement, 7),
                Secure = sqlite3_column_int(statement, 8) != 0,
                HttpOnly = sqlite3_column_int(statement, 9) != 0,
                LastAccessUtc = sqlite3_column_int64(statement, 10),
                HasExpires = sqlite3_column_int(statement, 11) != 0,
                Persistent = sqlite3_column_int(statement, 12) != 0,
                Priority = sqlite3_column_int(statement, 13),
                SameSite = sqlite3_column_int(statement, 14),
                SourceScheme = sqlite3_column_int(statement, 15),
                SourcePort = sqlite3_column_int(statement, 16),
                LastUpdateUtc = sqlite3_column_int64(statement, 17),
                SourceType = sqlite3_column_int(statement, 18),
                CrossSiteAncestor = sqlite3_column_int(statement, 19) != 0,
            };
        }

        private static bool CookiesEqual(TradingViewCookieRecord left, TradingViewCookieRecord right)
        {
            return left.CreationUtc == right.CreationUtc && left.HostKey == right.HostKey &&
                left.TopFrameSiteKey == right.TopFrameSiteKey && left.Name == right.Name && left.Value == right.Value &&
                left.Path == right.Path && left.ExpiresUtc == right.ExpiresUtc && left.Secure == right.Secure &&
                left.HttpOnly == right.HttpOnly && left.LastAccessUtc == right.LastAccessUtc &&
                left.HasExpires == right.HasExpires && left.Persistent == right.Persistent &&
                left.Priority == right.Priority && left.SameSite == right.SameSite &&
                left.SourceScheme == right.SourceScheme && left.SourcePort == right.SourcePort &&
                left.LastUpdateUtc == right.LastUpdateUtc && left.SourceType == right.SourceType &&
                left.CrossSiteAncestor == right.CrossSiteAncestor;
        }

        private static void ValidateCookies(TradingViewCookieRecord[] cookies)
        {
            HashSet<string> identities = new HashSet<string>(StringComparer.OrdinalIgnoreCase);
            Dictionary<string, int> pairs = new Dictionary<string, int>(StringComparer.OrdinalIgnoreCase);
            foreach (TradingViewCookieRecord cookie in cookies)
            {
                if (cookie == null || (cookie.Name != "sessionid" && cookie.Name != "sessionid_sign") || !AllowedHost(cookie.HostKey) ||
                    cookie.TopFrameSiteKey != "" || !cookie.CrossSiteAncestor || String.IsNullOrEmpty(cookie.Path) ||
                    cookie.Path[0] != '/' || Encoding.UTF8.GetByteCount(cookie.Path) > 1024 ||
                    cookie.Path.IndexOfAny(new char[] { '\0', '\r', '\n' }) >= 0 || !ValidValue(cookie.Value) ||
                    cookie.Priority < 0 || cookie.Priority > 2 || cookie.SameSite < -1 || cookie.SameSite > 2 ||
                    cookie.SourceScheme < 0 || cookie.SourceScheme > 2 || cookie.SourcePort < -1 || cookie.SourcePort > 65535 ||
                    cookie.SourceType < 0 || cookie.SourceType > 3 || cookie.CreationUtc < 0 || cookie.ExpiresUtc < 0 ||
                    cookie.LastAccessUtc < 0 || cookie.LastUpdateUtc < 0 || cookie.HasExpires != cookie.Persistent ||
                    (cookie.HasExpires && cookie.ExpiresUtc == 0))
                {
                    throw new ArgumentException("TradingView session cookie input is invalid.", "cookies");
                }
                string identity = cookie.HostKey + "\0" + cookie.TopFrameSiteKey + "\0" + cookie.Name + "\0" +
                    cookie.Path + "\0" + cookie.SourceScheme + "\0" + cookie.SourcePort + "\0" + cookie.CrossSiteAncestor;
                if (!identities.Add(identity))
                {
                    throw new ArgumentException("TradingView session cookie input is duplicated.", "cookies");
                }
                string pair = cookie.HostKey + "\0" + cookie.TopFrameSiteKey + "\0" + cookie.Path + "\0" +
                    cookie.SourceScheme + "\0" + cookie.SourcePort + "\0" + cookie.CrossSiteAncestor;
                int mask;
                pairs.TryGetValue(pair, out mask);
                pairs[pair] = mask | (cookie.Name == "sessionid" ? 1 : 2);
            }
            foreach (int pair in pairs.Values)
            {
                if (pair != 3)
                {
                    throw new ArgumentException("TradingView signed session cookie input is incomplete.", "cookies");
                }
            }
        }

        private static bool AllowedHost(string host)
        {
            if (String.IsNullOrWhiteSpace(host) || host.Length > 512 || host.Trim() != host ||
                host.IndexOfAny(new char[] { '\0', '\r', '\n' }) >= 0) { return false; }
            string folded = host.ToLowerInvariant();
            return folded == "tradingview.com" || folded == ".tradingview.com" || folded.EndsWith(".tradingview.com", StringComparison.Ordinal);
        }

        private static bool ValidValue(string value)
        {
            if (String.IsNullOrEmpty(value) || Encoding.UTF8.GetByteCount(value) > MaximumValueBytes) { return false; }
            foreach (char character in value)
            {
                int octet = (int)character;
                if (octet != 0x21 && (octet < 0x23 || octet > 0x2b) && (octet < 0x2d || octet > 0x3a) &&
                    (octet < 0x3c || octet > 0x5b) && (octet < 0x5d || octet > 0x7e)) { return false; }
            }
            return true;
        }

        private static int ScalarInt(IntPtr database, string sql)
        {
            IntPtr statement = Prepare(database, sql);
            try
            {
                Check(sqlite3_step(statement), "read TradingView SQLite integer", SqliteRow);
                int value = sqlite3_column_int(statement, 0);
                Check(sqlite3_step(statement), "finish TradingView SQLite integer", SqliteDone);
                return value;
            }
            finally { Finalize(statement); }
        }

        private static string ScalarText(IntPtr database, string sql)
        {
            IntPtr statement = Prepare(database, sql);
            try
            {
                Check(sqlite3_step(statement), "read TradingView SQLite text", SqliteRow);
                string value = ColumnText(statement, 0, 128);
                Check(sqlite3_step(statement), "finish TradingView SQLite text", SqliteDone);
                return value;
            }
            finally { Finalize(statement); }
        }

        private static IntPtr Prepare(IntPtr database, string sql)
        {
            IntPtr statement;
            Check(sqlite3_prepare16_v2(database, sql, -1, out statement, IntPtr.Zero), "prepare TradingView SQLite statement");
            if (statement == IntPtr.Zero) { throw new InvalidOperationException("prepare TradingView SQLite statement returned no handle."); }
            return statement;
        }

        private static string ColumnText(IntPtr statement, int column, int maximumBytes)
        {
            IntPtr value = sqlite3_column_text16(statement, column);
            int bytes = sqlite3_column_bytes16(statement, column);
            if (bytes < 0 || bytes > maximumBytes) { throw new InvalidOperationException("TradingView SQLite text exceeds its bound."); }
            if (bytes == 0) { return String.Empty; }
            if (value == IntPtr.Zero) { throw new InvalidOperationException("TradingView SQLite text is unavailable."); }
            return Marshal.PtrToStringUni(value, bytes / 2);
        }

        private static void BindText(IntPtr statement, int index, string value)
        {
            Check(sqlite3_bind_text16(statement, index, value, Encoding.Unicode.GetByteCount(value), SqliteTransient), "bind TradingView SQLite text");
        }

        private static void BindInt(IntPtr statement, int index, int value)
        {
            Check(sqlite3_bind_int(statement, index, value), "bind TradingView SQLite integer");
        }

        private static void BindInt64(IntPtr statement, int index, long value)
        {
            Check(sqlite3_bind_int64(statement, index, value), "bind TradingView SQLite 64-bit integer");
        }

        private static void Exec(IntPtr database, string sql)
        {
            byte[] encoded = Encoding.UTF8.GetBytes(sql + "\0");
            GCHandle pinned = GCHandle.Alloc(encoded, GCHandleType.Pinned);
            try
            {
                Check(sqlite3_exec(database, pinned.AddrOfPinnedObject(), IntPtr.Zero, IntPtr.Zero, IntPtr.Zero), "execute TradingView SQLite statement");
            }
            finally
            {
                pinned.Free();
                Array.Clear(encoded, 0, encoded.Length);
            }
        }

        private static void Finalize(IntPtr statement)
        {
            if (statement == IntPtr.Zero) { return; }
            int result = sqlite3_finalize(statement);
            if (result != SqliteOk) { throw new InvalidOperationException("finalize TradingView SQLite statement failed with code " + result + "."); }
        }

        private static void Check(int result, string role, int expected = SqliteOk)
        {
            if (result != expected) { throw new InvalidOperationException(role + " failed with SQLite code " + result + "."); }
        }

        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl)]
        private static extern int sqlite3_open_v2(IntPtr filename, out IntPtr database, int flags, IntPtr vfs);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl)]
        private static extern int sqlite3_close_v2(IntPtr database);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl)]
        private static extern int sqlite3_busy_timeout(IntPtr database, int milliseconds);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Unicode)]
        private static extern int sqlite3_prepare16_v2(IntPtr database, string sql, int bytes, out IntPtr statement, IntPtr tail);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl)]
        private static extern int sqlite3_step(IntPtr statement);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl)]
        private static extern int sqlite3_reset(IntPtr statement);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl)]
        private static extern int sqlite3_clear_bindings(IntPtr statement);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl)]
        private static extern int sqlite3_finalize(IntPtr statement);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl)]
        private static extern int sqlite3_column_int(IntPtr statement, int column);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl)]
        private static extern long sqlite3_column_int64(IntPtr statement, int column);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl)]
        private static extern IntPtr sqlite3_column_text16(IntPtr statement, int column);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl)]
        private static extern int sqlite3_column_bytes16(IntPtr statement, int column);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl)]
        private static extern int sqlite3_column_bytes(IntPtr statement, int column);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Unicode)]
        private static extern int sqlite3_bind_text16(IntPtr statement, int index, string value, int bytes, IntPtr destructor);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl)]
        private static extern int sqlite3_bind_int(IntPtr statement, int index, int value);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl)]
        private static extern int sqlite3_bind_int64(IntPtr statement, int index, long value);
        [DllImport("winsqlite3.dll", CallingConvention = CallingConvention.Cdecl)]
        private static extern int sqlite3_exec(IntPtr database, IntPtr sql, IntPtr callback, IntPtr argument, IntPtr error);
    }
}
