//go:build windows

package sandbox

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTradingViewHostExportAndGuestMergeBoundary(t *testing.T) {
	root := t.TempDir()
	localAppData := filepath.Join(root, "local")
	t.Setenv("LOCALAPPDATA", localAppData)
	profile := filepath.Join(localAppData, "Packages", tradingViewDesktopPackageFamilyName,
		"LocalCache", "Roaming", "TradingView")
	if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
		t.Fatal(err)
	}
	cookieDatabase := filepath.Join(profile, "Network", "Cookies")
	createTradingViewCookieDatabaseFixture(t, cookieDatabase, "host-session", "unrelated-value")
	createTradingViewUserSettingsFixture(t, profile, "12345678")
	payload, count, err := exportTradingViewAuthentication(t.Context(), profile)
	if err != nil || count != 2 {
		t.Fatalf("export TradingView authentication: count=%d err=%v", count, err)
	}
	authentication, err := decodeTradingViewAuthentication(payload)
	if err != nil || len(authentication.Cookies) != 2 || len(authentication.UserIDs) != 1 || authentication.UserIDs[0] != "12345678" || authentication.Cookies[0].Value != "host-session" ||
		authentication.Cookies[1].Value != "host-session-signature" {
		t.Fatalf("exported authentication = %#v, err=%v", authentication, err)
	}

	destination := filepath.Join(root, "guest-profile", "Network", "Cookies")
	createTradingViewCookieDatabaseFixture(t, destination, "old-guest-session", "preserve-me")
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
	signature, found, err := database.readOneText("SELECT value FROM cookies WHERE name='sessionid_sign' AND host_key='.tradingview.com'")
	if err != nil || !found || signature != "host-session-signature" {
		t.Fatalf("guest session signature = %q, found=%v, err=%v", signature, found, err)
	}
	unrelated, found, err := database.readOneText("SELECT value FROM cookies WHERE name='unrelated' AND host_key='.example.com'")
	if err != nil || !found || unrelated != "preserve-me" {
		t.Fatalf("unrelated guest cookie = %q, found=%v, err=%v", unrelated, found, err)
	}
	assertTradingViewEssentialOnlyPreferences(t, database)
}

func assertTradingViewEssentialOnlyPreferences(t *testing.T, database *tradingViewSQLiteDatabase) {
	t.Helper()
	settings, found, err := database.readOneText("SELECT value FROM cookies WHERE name='cookiesSettings' AND host_key='.tradingview.com' AND path='/'")
	if err != nil || !found || settings != `{"analytics":false,"advertising":false}` {
		t.Fatalf("TradingView cookie settings = %q, found=%v, err=%v", settings, found, err)
	}
	banner, found, err := database.readOneText("SELECT value FROM cookies WHERE name='cookiePrivacyPreferenceBannerProduction' AND host_key='.tradingview.com' AND path='/'")
	if err != nil || !found || banner != "reject" {
		t.Fatalf("TradingView privacy banner setting = %q, found=%v, err=%v", banner, found, err)
	}
}

func createTradingViewUserSettingsFixture(t *testing.T, profile, userID string) {
	t.Helper()
	directory := filepath.Join(profile, "TVUserStorage", "id-"+userID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "settings.json"), tradingViewInitialSettings, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTradingViewHostExportMissingProfileIsNoop(t *testing.T) {
	root := t.TempDir()
	localAppData := filepath.Join(root, "local")
	t.Setenv("LOCALAPPDATA", localAppData)
	profile := filepath.Join(localAppData, "Packages", tradingViewDesktopPackageFamilyName,
		"LocalCache", "Roaming", "TradingView")
	payload, count, err := exportTradingViewAuthentication(t.Context(), profile)
	if err != nil || count != 0 {
		t.Fatalf("missing TradingView profile export: count=%d err=%v", count, err)
	}
	authentication, err := decodeTradingViewAuthentication(payload)
	if err != nil || len(authentication.Cookies) != 0 {
		t.Fatalf("empty TradingView authentication = %#v, err=%v", authentication, err)
	}
}

func TestDefaultTradingViewProfilePathUsesPackagedDesktopProfile(t *testing.T) {
	root := t.TempDir()
	localAppData := filepath.Join(root, "local")
	t.Setenv("LOCALAPPDATA", localAppData)
	packaged := filepath.Join(localAppData, "Packages", tradingViewDesktopPackageFamilyName,
		"LocalCache", "Roaming", "TradingView")
	profile, err := defaultTradingViewProfilePath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(filepath.Clean(profile), filepath.Clean(packaged)) {
		t.Fatalf("packaged TradingView profile = %q, want %q", profile, packaged)
	}
}

func TestTradingViewGuestSettingsInitializationIsIdempotent(t *testing.T) {
	root := t.TempDir()
	expanded := filepath.Join(root, "expanded")
	appData := filepath.Join(root, "appdata")
	authenticationPath := filepath.Join(expanded, "tradingview", "authentication.json")
	templatePath := filepath.Join(expanded, "herdr-sandbox", "tradingview-settings.json")
	adapterPath := filepath.Join(expanded, "herdr-sandbox", "tradingview-cookie-sync.cs")
	if err := os.MkdirAll(filepath.Dir(authenticationPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(templatePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(templatePath, tradingViewInitialSettings, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(adapterPath, tradingViewCookieSyncSource, 0o600); err != nil {
		t.Fatal(err)
	}
	payload, _, err := encodeTradingViewAuthentication(tradingViewAuthentication{
		SchemaVersion: tradingViewAuthenticationSchema,
		Cookies:       []tradingViewCookie{},
		UserIDs:       []string{"12345678"},
	})
	if err != nil {
		t.Fatal(err)
	}
	source := string(configurationSyncScript)
	start := strings.Index(source, "$tradingViewAuthenticationPath =")
	end := strings.Index(source[start:], "[Console]::Error.WriteLine('[config-sync] apply-herdr')")
	if start < 0 || end <= 0 {
		t.Fatal("TradingView configuration apply section is missing")
	}
	section := source[start : start+end]
	section = strings.Replace(section, "@(Get-Process -Name 'TradingView' -ErrorAction SilentlyContinue)", "@()", 1)
	runApply := func() {
		t.Helper()
		if err := os.WriteFile(authenticationPath, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		script := `$ErrorActionPreference = 'Stop'
$expanded = '` + strings.ReplaceAll(expanded, "'", "''") + `'
$env:APPDATA = '` + strings.ReplaceAll(appData, "'", "''") + `'
$tradingViewAuthenticatedCookies = 0
$tradingViewAuthenticationVerified = $false
function Assert-ConfigurationDestinationPath {
    param([Parameter(Mandatory = $true)][string]$Path)
    if (-not [IO.Path]::IsPathRooted([IO.Path]::GetFullPath($Path))) { throw 'destination is not absolute' }
}
function Get-FileHash {
    param([Parameter(Mandatory = $true)][string]$LiteralPath, [string]$Algorithm)
    $sha256 = [Security.Cryptography.SHA256]::Create()
    try {
        $stream = [IO.File]::OpenRead($LiteralPath)
        try { $hash = ([BitConverter]::ToString($sha256.ComputeHash($stream))).Replace('-', '') } finally { $stream.Dispose() }
    } finally { $sha256.Dispose() }
    return [pscustomobject]@{ Hash = $hash }
}
` + section
		runWindowsPowerShellFileFixture(t, script)
	}
	runApply()
	cookieDatabase := filepath.Join(appData, "TradingView", "Network", "Cookies")
	database, err := openTradingViewSQLiteReadOnly(cookieDatabase)
	if err != nil {
		t.Fatal(err)
	}
	assertTradingViewEssentialOnlyPreferences(t, database)
	if _, found, readErr := database.readOneText("SELECT value FROM cookies WHERE name='sessionid'"); readErr != nil || found {
		database.close()
		t.Fatalf("unauthenticated TradingView profile contains a session cookie: found=%v err=%v", found, readErr)
	}
	database.close()
	destination := filepath.Join(appData, "TradingView", "TVUserStorage", "id-12345678", "settings.json")
	actual, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(actual, tradingViewInitialSettings) {
		t.Fatalf("initialized TradingView settings mismatch: err=%v", err)
	}
	preserved := bytes.Replace(tradingViewInitialSettings, []byte(`"TradingView"`), []byte(`"Preserved"`), 1)
	if err := os.WriteFile(destination, preserved, 0o600); err != nil {
		t.Fatal(err)
	}
	runApply()
	database, err = openTradingViewSQLiteReadOnly(cookieDatabase)
	if err != nil {
		t.Fatal(err)
	}
	assertTradingViewEssentialOnlyPreferences(t, database)
	database.close()
	actual, err = os.ReadFile(destination)
	if err != nil || !bytes.Equal(actual, preserved) {
		t.Fatalf("retained TradingView settings were replaced: err=%v", err)
	}
}

func TestNativeTradingViewAuthenticationExport(t *testing.T) {
	if os.Getenv("HERDR_SANDBOX_TEST_REAL_TRADINGVIEW_AUTH") != "1" {
		t.Skip("set HERDR_SANDBOX_TEST_REAL_TRADINGVIEW_AUTH=1 for the installed host TradingView boundary")
	}
	profile, err := defaultTradingViewProfilePath()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	payload, count, err := exportTradingViewAuthentication(ctx, profile)
	defer clear(payload)
	if err != nil {
		t.Fatal(err)
	}
	if count < 1 {
		t.Fatal("installed host TradingView profile exported no authenticated session cookies")
	}
	t.Logf("exported %d authenticated TradingView session cookie(s)", count)
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
			localAppData := filepath.Join(root, "local")
			t.Setenv("LOCALAPPDATA", localAppData)
			profile := filepath.Join(localAppData, "Packages", tradingViewDesktopPackageFamilyName,
				"LocalCache", "Roaming", "TradingView")
			if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
				t.Fatal(err)
			}
			database := filepath.Join(profile, "Network", "Cookies")
			createTradingViewCookieDatabaseFixture(t, database, "host-session", "unrelated-value")
			updateTradingViewCookieFixture(t, database, test.assignment)
			if _, _, err := exportTradingViewAuthentication(t.Context(), profile); err == nil {
				t.Fatal("malformed TradingView SQLite metadata unexpectedly exported")
			}
		})
	}
}

func TestTradingViewHostExportRejectsNonPortableSessionStorage(t *testing.T) {
	root := t.TempDir()
	localAppData := filepath.Join(root, "local")
	t.Setenv("LOCALAPPDATA", localAppData)
	profile := filepath.Join(localAppData, "Packages", tradingViewDesktopPackageFamilyName,
		"LocalCache", "Roaming", "TradingView")
	if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(profile, "Network", "Cookies")
	createTradingViewCookieDatabaseFixture(t, database, "host-session", "unrelated-value")
	updateTradingViewCookieFixture(t, database, "value='', encrypted_value=x'01'")
	if _, _, err := exportTradingViewAuthentication(t.Context(), profile); err == nil ||
		!strings.Contains(err.Error(), "not portable plaintext") {
		t.Fatalf("nonportable TradingView session storage error = %v", err)
	}
}

func createTradingViewCookieDatabaseFixture(t *testing.T, path, sessionValue, unrelatedValue string) {
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
$records = @()
foreach ($identity in @(
    [pscustomobject]@{ Name = 'sessionid'; Value = '` + strings.ReplaceAll(sessionValue, "'", "''") + `' },
    [pscustomobject]@{ Name = 'sessionid_sign'; Value = '` + strings.ReplaceAll(sessionValue, "'", "''") + `-signature' }
)) {
    $record = New-Object 'HerdrSandbox.TradingViewCookieRecord'
    $record.CreationUtc = 13390000000000000
    $record.HostKey = '.tradingview.com'
    $record.TopFrameSiteKey = ''
    $record.Name = [string]$identity.Name
    $record.Value = [string]$identity.Value
    $record.Path = '/'
    $record.ExpiresUtc = 13400000000000000
    $record.Secure = $true
    $record.HttpOnly = $true
    $record.LastAccessUtc = 13390000000000001
    $record.HasExpires = $true
    $record.Persistent = $true
    $record.Priority = 1
    $record.SameSite = -1
    $record.SourceScheme = 2
    $record.SourcePort = 443
    $record.LastUpdateUtc = 13390000000000002
    $record.SourceType = 1
    $record.CrossSiteAncestor = $true
    $records += $record
}
[HerdrSandbox.TradingViewCookieSync]::Import('` + strings.ReplaceAll(path, "'", "''") + `', $records) | Out-Null
`
	runWindowsPowerShellFixture(t, script)
	insertUnrelatedTradingViewCookieFixture(t, path, unrelatedValue)
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
if ($count -ne 2) { throw "imported $count cookies" }
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

func runWindowsPowerShellFileFixture(t *testing.T, script string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.ps1")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := hiddenCommandContext(ctx, mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Windows PowerShell file fixture: %v: %s", err, output)
	}
}
