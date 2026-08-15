package sandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func testTradingViewCookie(name, value string) tradingViewCookie {
	return tradingViewCookie{
		CreationUTC:       13390000000000000,
		HostKey:           ".tradingview.com",
		Name:              name,
		Value:             value,
		Path:              "/",
		ExpiresUTC:        13400000000000000,
		Secure:            true,
		HTTPOnly:          true,
		LastAccessUTC:     13390000000000001,
		HasExpires:        true,
		Persistent:        true,
		Priority:          1,
		SameSite:          -1,
		SourceScheme:      2,
		SourcePort:        443,
		LastUpdateUTC:     13390000000000002,
		SourceType:        1,
		CrossSiteAncestor: true,
	}
}

func testTradingViewAuthentication(value string) tradingViewAuthentication {
	return tradingViewAuthentication{
		SchemaVersion: tradingViewAuthenticationSchema,
		UserIDs:       []string{"12345678"},
		Cookies: []tradingViewCookie{
			testTradingViewCookie(tradingViewSessionCookieName, value),
			testTradingViewCookie(tradingViewSessionSignatureCookieName, value+"-signature"),
		},
	}
}

func TestTradingViewAuthenticationContractIsStrictAndSecretSafe(t *testing.T) {
	const secret = "signed-session-secret"
	authentication := testTradingViewAuthentication(secret)
	payload, count, err := encodeTradingViewAuthentication(authentication)
	if err != nil || count != 2 {
		t.Fatalf("encode TradingView authentication: count=%d err=%v", count, err)
	}
	decoded, err := decodeTradingViewAuthentication(payload)
	if err != nil || len(decoded.Cookies) != 2 || len(decoded.UserIDs) != 1 || decoded.UserIDs[0] != "12345678" || decoded.Cookies[0].Value != secret || decoded.Cookies[1].Value != secret+"-signature" {
		t.Fatalf("decoded TradingView authentication = %#v, err=%v", decoded, err)
	}

	duplicate := authentication
	duplicate.Cookies = append(append([]tradingViewCookie(nil), authentication.Cookies...), authentication.Cookies[0])
	if _, _, encodeErr := encodeTradingViewAuthentication(duplicate); encodeErr == nil {
		t.Fatal("duplicate TradingView authentication unexpectedly encoded")
	}
	incomplete := authentication
	incomplete.Cookies = append([]tradingViewCookie(nil), authentication.Cookies[:1]...)
	if _, _, encodeErr := encodeTradingViewAuthentication(incomplete); encodeErr == nil {
		t.Fatal("incomplete signed TradingView session unexpectedly encoded")
	}
	missingUser := authentication
	missingUser.UserIDs = []string{}
	if _, _, encodeErr := encodeTradingViewAuthentication(missingUser); encodeErr == nil {
		t.Fatal("authenticated TradingView state without a user settings identity unexpectedly encoded")
	}
	invalid := [][]byte{
		[]byte(`{"schemaVersion":3,"cookies":[],"userIds":[],"extra":true}`),
		[]byte(`{"schemaVersion":3,"schemaVersion":3,"cookies":[],"userIds":[]}`),
		[]byte(`{"schemaVersion":3,"cookies":[{"creationUtc":0,"hostKey":".tradingview.com","topFrameSiteKey":"","name":"sessionid","value":"` + secret + `","path":"/","expiresUtc":0,"secure":true,"httpOnly":true,"lastAccessUtc":0,"hasExpires":false,"persistent":false,"priority":1,"sameSite":-1,"sourceScheme":2,"sourcePort":443,"lastUpdateUtc":0,"sourceType":1}],"userIds":["12345678"]}`),
		[]byte(`{"schemaVersion":3,"cookies":[{"creationUtc":0,"hostKey":".tradingview.com","hostKey":".tradingview.com","topFrameSiteKey":"","name":"sessionid","value":"` + secret + `","path":"/","expiresUtc":0,"secure":true,"httpOnly":true,"lastAccessUtc":0,"hasExpires":false,"persistent":false,"priority":1,"sameSite":-1,"sourceScheme":2,"sourcePort":443,"lastUpdateUtc":0,"sourceType":1,"crossSiteAncestor":true}],"userIds":["12345678"]}`),
		[]byte(`{"schemaVersion":3,"cookies":[{"creationUtc":0,"hostKey":"evil.example","topFrameSiteKey":"","name":"sessionid","value":"` + secret + `","path":"/","expiresUtc":0,"secure":true,"httpOnly":true,"lastAccessUtc":0,"hasExpires":false,"persistent":false,"priority":1,"sameSite":-1,"sourceScheme":2,"sourcePort":443,"lastUpdateUtc":0,"sourceType":1,"crossSiteAncestor":true}],"userIds":["12345678"]}`),
		[]byte(`{"schemaVersion":3,"cookies":[],"userIds":["01"]}`),
	}
	for _, candidate := range invalid {
		if _, decodeErr := decodeTradingViewAuthentication(candidate); decodeErr == nil {
			t.Fatalf("invalid TradingView authentication unexpectedly succeeded: %s", candidate)
		} else if strings.Contains(decodeErr.Error(), secret) {
			t.Fatalf("TradingView authentication error exposed credential content: %v", decodeErr)
		}
	}

	for _, host := range []string{"tradingview.com", ".tradingview.com", "www.tradingview.com"} {
		if !tradingViewCookieHostAllowed(host) {
			t.Fatalf("allowlisted TradingView host rejected: %q", host)
		}
	}
	for _, host := range []string{"eviltradingview.com", "tradingview.com.evil.example", " tradingview.com"} {
		if tradingViewCookieHostAllowed(host) {
			t.Fatalf("non-TradingView host accepted: %q", host)
		}
	}
}

func TestTradingViewStackSelectionIncludesGlobalAndWorkspaceOwners(t *testing.T) {
	workspaces := []workspacePlan{{Name: "one", Stacks: []projectStack{stackGo}}, {Name: "two", Stacks: []projectStack{stackTradingView}}}
	if !provisioningStacksContain(nil, workspaces, stackTradingView) {
		t.Fatal("workspace TradingView selection was not detected")
	}
	if !provisioningStacksContain([]projectStack{stackTradingView}, nil, stackTradingView) {
		t.Fatal("global TradingView selection was not detected")
	}
	if provisioningStacksContain([]projectStack{stackGo}, workspaces[:1], stackTradingView) {
		t.Fatal("unselected TradingView stack was detected")
	}
}

func TestDevelopmentConfigurationArchiveIncludesOnlyFilteredTradingViewAuthentication(t *testing.T) {
	root := t.TempDir()
	packagePlan := filepath.Join(root, wingetPackagePlanFileName)
	writeTestFile(t, packagePlan, `{}`)
	herdrConfig := filepath.Join(root, "herdr-config.toml")
	writeTestFile(t, herdrConfig, "[terminal]\ndefault_shell = \"nu\"\n")
	payload, _, err := encodeTradingViewAuthentication(testTradingViewAuthentication("archive-secret"))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := buildDevelopmentConfigurationArchive(context.Background(), hostConfigurationSources{
		TradingViewAuthentication: payload,
		HerdrConfig:               herdrConfig,
		PackagePlan:               packagePlan,
	}, []byte("Write-Output 'apply fixture'\n"))
	if err != nil {
		t.Fatalf("build archive with TradingView authentication: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]bool{}
	for _, file := range reader.File {
		entries[file.Name] = true
	}
	for _, required := range []string{tradingViewAuthenticationArchivePath, tradingViewCookieSyncSourceArchivePath, tradingViewSettingsArchivePath} {
		if !entries[required] {
			t.Fatalf("TradingView archive is missing %s: %#v", required, entries)
		}
	}
	for _, forbidden := range []string{"tradingview/Local State", "tradingview/Network/Cookies", "tradingview/TVUserStorage"} {
		if entries[forbidden] {
			t.Fatalf("TradingView archive contains raw profile state: %s", forbidden)
		}
	}
	if count, err := configurationArchivePayloadFileCount(archive); err != nil || count != 1 {
		t.Fatalf("payload file count = %d, err = %v", count, err)
	}
	if !bytes.Contains(tradingViewInitialSettings, []byte(`"https://www.tradingview.com/chart/"`)) ||
		bytes.Contains(tradingViewInitialSettings, []byte(`tvd://new-tab`)) {
		t.Fatal("TradingView initial settings do not open one controllable chart")
	}
}

func TestTradingViewGuestAuthenticationNeverLaunchesOrTerminatesDesktop(t *testing.T) {
	script := string(configurationSyncScript)
	start := strings.Index(script, "[config-sync] apply-tradingview-authentication")
	end := strings.Index(script[start:], "[config-sync] apply-herdr")
	if start < 0 || end <= 0 {
		t.Fatal("TradingView authentication apply section is missing")
	}
	section := script[start : start+end]
	for _, required := range []string{"Get-Process -Name 'TradingView'", "close it before reapplying authentication", "TVUserStorage", "tradingview-settings.json", "Existing TradingView user settings"} {
		if !strings.Contains(section, required) {
			t.Fatalf("TradingView authentication guard is missing %q", required)
		}
	}
	for _, forbidden := range []string{"Stop-Process", "Start-Process", "taskkill", "TradingView.exe"} {
		if strings.Contains(section, forbidden) {
			t.Fatalf("TradingView authentication owns forbidden process behavior %q", forbidden)
		}
	}
}
