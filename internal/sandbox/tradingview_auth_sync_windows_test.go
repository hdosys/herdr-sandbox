//go:build windows

package sandbox

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTradingViewHostExportAndGuestMergeBoundary(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	profile := filepath.Join(root, "TradingView")
	if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x3a}, 32)
	writeTradingViewLocalStateFixture(t, profile, key)

	cookieDatabase := filepath.Join(profile, "Network", "Cookies")
	createTradingViewCookieDatabaseFixture(t, cookieDatabase, key, "host-session", "unrelated-value")
	payload, count, err := exportTradingViewAuthentication(t.Context(), profile)
	if err != nil || count != 1 {
		t.Fatalf("export TradingView authentication: count=%d err=%v", count, err)
	}
	authentication, err := decodeTradingViewAuthentication(payload)
	if err != nil || len(authentication.Cookies) != 1 || authentication.Cookies[0].Value != "host-session" {
		t.Fatalf("exported authentication = %#v, err=%v", authentication, err)
	}

	destination := filepath.Join(root, "guest-profile", "Network", "Cookies")
	createTradingViewCookieDatabaseFixture(t, destination, key, "old-guest-session", "preserve-me")
	adapter := filepath.Join(root, "tradingview-cookie-sync.cs")
	if err := os.WriteFile(adapter, tradingViewCookieSyncSource, 0o600); err != nil {
		t.Fatal(err)
	}
	authenticationPath := filepath.Join(root, "authentication.json")
	if err := os.WriteFile(authenticationPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	runTradingViewCookieAdapterFixture(t, adapter, authenticationPath, destination)

	database, err := openTradingViewSQLiteReadOnly(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer database.close()
	session, found, err := database.readOneText("SELECT value FROM cookies WHERE name='sessionid' AND host_key='.tradingview.com'")
	if err != nil || !found || session != "host-session" {
		t.Fatalf("guest session cookie = %q, found=%v, err=%v", session, found, err)
	}
	unrelated, found, err := database.readOneText("SELECT value FROM cookies WHERE name='unrelated' AND host_key='.example.com'")
	if err != nil || !found || unrelated != "preserve-me" {
		t.Fatalf("unrelated guest cookie = %q, found=%v, err=%v", unrelated, found, err)
	}
}

func TestTradingViewHostExportMissingProfileIsNoop(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	payload, count, err := exportTradingViewAuthentication(t.Context(), filepath.Join(root, "TradingView"))
	if err != nil || count != 0 {
		t.Fatalf("missing TradingView profile export: count=%d err=%v", count, err)
	}
	authentication, err := decodeTradingViewAuthentication(payload)
	if err != nil || len(authentication.Cookies) != 0 {
		t.Fatalf("empty TradingView authentication = %#v, err=%v", authentication, err)
	}
}

func TestTradingViewCookieDecryptionRejectsWrongDomainBinding(t *testing.T) {
	key := bytes.Repeat([]byte{0x4b}, 32)
	encrypted, err := encryptTradingViewCookieFixture(key, ".tradingview.com", "fixture-session")
	if err != nil {
		t.Fatal(err)
	}
	defer clear(encrypted)
	if _, err := decryptTradingViewCookieValue(key, ".example.com", "", encrypted); err == nil {
		t.Fatal("TradingView cookie with the wrong domain binding unexpectedly decrypted")
	}
}

func TestTradingViewCookieDecryptionRejectsPlaintextHostValue(t *testing.T) {
	key := bytes.Repeat([]byte{0x4b}, 32)
	if _, err := decryptTradingViewCookieValue(key, ".tradingview.com", "plaintext-session", nil); err == nil {
		t.Fatal("plaintext host TradingView cookie unexpectedly succeeded")
	}
}

func TestTradingViewHostExportRejectsMalformedSQLiteMetadata(t *testing.T) {
	for _, test := range []struct {
		name       string
		assignment string
	}{
		{name: "non boolean", assignment: "is_secure=2"},
		{name: "wrong storage class", assignment: "priority='invalid'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("APPDATA", root)
			profile := filepath.Join(root, "TradingView")
			if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
				t.Fatal(err)
			}
			key := bytes.Repeat([]byte{0x3a}, 32)
			writeTradingViewLocalStateFixture(t, profile, key)
			database := filepath.Join(profile, "Network", "Cookies")
			createTradingViewCookieDatabaseFixture(t, database, key, "host-session", "unrelated-value")
			updateTradingViewCookieFixture(t, database, test.assignment)
			if _, _, err := exportTradingViewAuthentication(t.Context(), profile); err == nil {
				t.Fatal("malformed TradingView SQLite metadata unexpectedly exported")
			}
		})
	}
}

func writeTradingViewLocalStateFixture(t *testing.T, profile string, key []byte) {
	t.Helper()
	protectedKey, err := protectLocalData(key)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(protectedKey)
	keyEnvelope := append([]byte("DPAPI"), protectedKey...)
	defer clear(keyEnvelope)
	localState, err := json.Marshal(map[string]any{"os_crypt": map[string]any{"encrypted_key": base64.StdEncoding.EncodeToString(keyEnvelope)}})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(profile, "Local State"), string(localState))
}

func createTradingViewCookieDatabaseFixture(t *testing.T, path string, key []byte, sessionValue, unrelatedValue string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	adapter := filepath.Join(t.TempDir(), "tradingview-cookie-sync.cs")
	if err := os.WriteFile(adapter, tradingViewCookieSyncSource, 0o600); err != nil {
		t.Fatal(err)
	}
	script := `$ErrorActionPreference = 'Stop'
Add-Type -Path '` + strings.ReplaceAll(adapter, "'", "''") + `'
$session = New-Object 'HerdrSandbox.TradingViewCookieRecord'
$session.CreationUtc = 13390000000000000
$session.HostKey = '.tradingview.com'
$session.TopFrameSiteKey = ''
$session.Name = 'sessionid'
$session.Value = '` + strings.ReplaceAll(sessionValue, "'", "''") + `'
$session.Path = '/'
$session.ExpiresUtc = 13400000000000000
$session.Secure = $true
$session.HttpOnly = $true
$session.LastAccessUtc = 13390000000000001
$session.HasExpires = $true
$session.Persistent = $true
$session.Priority = 1
$session.SameSite = -1
$session.SourceScheme = 2
$session.SourcePort = 443
$session.LastUpdateUtc = 13390000000000002
$session.SourceType = 1
$session.CrossSiteAncestor = $false
[HerdrSandbox.TradingViewCookieSync]::Import('` + strings.ReplaceAll(path, "'", "''") + `', @($session)) | Out-Null
`
	runWindowsPowerShellFixture(t, script)
	insertUnrelatedTradingViewCookieFixture(t, path, unrelatedValue)
	if sessionValue == "host-session" {
		encryptTradingViewSessionFixture(t, path, key, sessionValue)
	}
}

func insertUnrelatedTradingViewCookieFixture(t *testing.T, path, value string) {
	t.Helper()
	script := `$ErrorActionPreference = 'Stop'
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class SQLiteFixture {
  [DllImport("winsqlite3.dll", CallingConvention=CallingConvention.Cdecl, CharSet=CharSet.Unicode)] public static extern int sqlite3_open16(string p, out IntPtr db);
  [DllImport("winsqlite3.dll", CallingConvention=CallingConvention.Cdecl, CharSet=CharSet.Ansi)] public static extern int sqlite3_exec(IntPtr db, string sql, IntPtr cb, IntPtr arg, IntPtr err);
  [DllImport("winsqlite3.dll", CallingConvention=CallingConvention.Cdecl)] public static extern int sqlite3_close_v2(IntPtr db);
}
'@
$db = [IntPtr]::Zero
if ([SQLiteFixture]::sqlite3_open16('` + strings.ReplaceAll(path, "'", "''") + `', [ref]$db) -ne 0) { throw 'open failed' }
try {
  $sql = "INSERT OR REPLACE INTO cookies VALUES(13390000000000000,'.example.com','','unrelated','` + strings.ReplaceAll(value, "'", "''") + `',zeroblob(0),'/',0,0,0,13390000000000001,0,0,1,-1,1,80,13390000000000002,1,0)"
  if ([SQLiteFixture]::sqlite3_exec($db, $sql, [IntPtr]::Zero, [IntPtr]::Zero, [IntPtr]::Zero) -ne 0) { throw 'insert failed' }
} finally { [void][SQLiteFixture]::sqlite3_close_v2($db) }
`
	runWindowsPowerShellFixture(t, script)
}

func updateTradingViewCookieFixture(t *testing.T, path, assignment string) {
	t.Helper()
	script := `$ErrorActionPreference = 'Stop'
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class SQLiteUpdateFixture {
  [DllImport("winsqlite3.dll", CallingConvention=CallingConvention.Cdecl, CharSet=CharSet.Unicode)] public static extern int sqlite3_open16(string p, out IntPtr db);
  [DllImport("winsqlite3.dll", CallingConvention=CallingConvention.Cdecl, CharSet=CharSet.Ansi)] public static extern int sqlite3_exec(IntPtr db, string sql, IntPtr cb, IntPtr arg, IntPtr err);
  [DllImport("winsqlite3.dll", CallingConvention=CallingConvention.Cdecl)] public static extern int sqlite3_close_v2(IntPtr db);
}
'@
$db = [IntPtr]::Zero
if ([SQLiteUpdateFixture]::sqlite3_open16('` + strings.ReplaceAll(path, "'", "''") + `', [ref]$db) -ne 0) { throw 'open failed' }
try {
  $sql = "UPDATE cookies SET ` + assignment + ` WHERE name='sessionid'"
  if ([SQLiteUpdateFixture]::sqlite3_exec($db, $sql, [IntPtr]::Zero, [IntPtr]::Zero, [IntPtr]::Zero) -ne 0) { throw 'update failed' }
} finally { [void][SQLiteUpdateFixture]::sqlite3_close_v2($db) }
`
	runWindowsPowerShellFixture(t, script)
}

func encryptTradingViewSessionFixture(t *testing.T, path string, key []byte, plaintext string) {
	t.Helper()
	encrypted, err := encryptTradingViewCookieFixture(key, ".tradingview.com", plaintext)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(encrypted)
	script := `$ErrorActionPreference = 'Stop'
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class SQLiteCipherFixture {
  [DllImport("winsqlite3.dll", CallingConvention=CallingConvention.Cdecl, CharSet=CharSet.Unicode)] public static extern int sqlite3_open16(string p, out IntPtr db);
  [DllImport("winsqlite3.dll", CallingConvention=CallingConvention.Cdecl, CharSet=CharSet.Ansi)] public static extern int sqlite3_exec(IntPtr db, string sql, IntPtr cb, IntPtr arg, IntPtr err);
  [DllImport("winsqlite3.dll", CallingConvention=CallingConvention.Cdecl)] public static extern int sqlite3_close_v2(IntPtr db);
}
'@
$db = [IntPtr]::Zero
if ([SQLiteCipherFixture]::sqlite3_open16('` + strings.ReplaceAll(path, "'", "''") + `', [ref]$db) -ne 0) { throw 'open failed' }
try {
  $sql = "UPDATE cookies SET value='', encrypted_value=x'` + hex.EncodeToString(encrypted) + `' WHERE name='sessionid'"
  if ([SQLiteCipherFixture]::sqlite3_exec($db, $sql, [IntPtr]::Zero, [IntPtr]::Zero, [IntPtr]::Zero) -ne 0) { throw 'update failed' }
} finally {
  [void][SQLiteCipherFixture]::sqlite3_close_v2($db)
}
`
	runWindowsPowerShellFixture(t, script)
}

func encryptTradingViewCookieFixture(key []byte, hostKey, value string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := bytes.Repeat([]byte{0x5c}, gcm.NonceSize())
	hash := sha256.Sum256([]byte(hostKey))
	plaintext := append(hash[:], []byte(value)...)
	defer clear(plaintext)
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	result := append([]byte("v10"), nonce...)
	result = append(result, ciphertext...)
	clear(ciphertext)
	return result, nil
}

func runTradingViewCookieAdapterFixture(t *testing.T, adapter, authentication, destination string) {
	t.Helper()
	script := `$ErrorActionPreference = 'Stop'
Add-Type -Path '` + strings.ReplaceAll(adapter, "'", "''") + `'
$input = [IO.File]::ReadAllText('` + strings.ReplaceAll(authentication, "'", "''") + `') | ConvertFrom-Json
$records = [Array]::CreateInstance(('HerdrSandbox.TradingViewCookieRecord' -as [type]), @($input.cookies).Count)
$index = 0
foreach ($cookie in @($input.cookies)) {
  $record = New-Object 'HerdrSandbox.TradingViewCookieRecord'
  foreach ($property in $cookie.PSObject.Properties) { $record.($property.Name) = $property.Value }
  $records.SetValue($record, $index)
  $index += 1
}
$count = [HerdrSandbox.TradingViewCookieSync]::Import('` + strings.ReplaceAll(destination, "'", "''") + `', $records)
if ($count -ne 1) { throw "imported $count cookies" }
`
	runWindowsPowerShellFixture(t, script)
}

func runWindowsPowerShellFixture(t *testing.T, script string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := hiddenCommandContext(ctx, mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(script))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Windows PowerShell fixture: %v: %s", err, output)
	}
}
