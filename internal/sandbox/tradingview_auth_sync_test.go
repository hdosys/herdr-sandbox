package sandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func testTradingViewCookie(value string) tradingViewCookie {
	return tradingViewCookie{
		CreationUTC:   13390000000000000,
		HostKey:       ".tradingview.com",
		Name:          tradingViewSessionCookieName,
		Value:         value,
		Path:          "/",
		ExpiresUTC:    13400000000000000,
		Secure:        true,
		HTTPOnly:      true,
		LastAccessUTC: 13390000000000001,
		HasExpires:    true,
		Persistent:    true,
		Priority:      1,
		SameSite:      -1,
		SourceScheme:  2,
		SourcePort:    443,
		LastUpdateUTC: 13390000000000002,
		SourceType:    1,
	}
}

func TestTradingViewAuthenticationContractIsStrictAndSecretSafe(t *testing.T) {
	const secret = "signed-session-secret"
	authentication := tradingViewAuthentication{
		SchemaVersion: tradingViewAuthenticationSchema,
		Cookies:       []tradingViewCookie{testTradingViewCookie(secret)},
	}
	payload, count, err := encodeTradingViewAuthentication(authentication)
	if err != nil || count != 1 {
		t.Fatalf("encode TradingView authentication: count=%d err=%v", count, err)
	}
	decoded, err := decodeTradingViewAuthentication(payload)
	if err != nil || len(decoded.Cookies) != 1 || decoded.Cookies[0].Value != secret {
		t.Fatalf("decoded TradingView authentication = %#v, err=%v", decoded, err)
	}

	duplicate := authentication
	duplicate.Cookies = append(append([]tradingViewCookie(nil), authentication.Cookies...), authentication.Cookies[0])
	if _, _, encodeErr := encodeTradingViewAuthentication(duplicate); encodeErr == nil {
		t.Fatal("duplicate TradingView authentication unexpectedly encoded")
	}
	invalid := [][]byte{
		[]byte(`{"schemaVersion":1,"cookies":[],"extra":true}`),
		[]byte(`{"schemaVersion":1,"schemaVersion":1,"cookies":[]}`),
		[]byte(`{"schemaVersion":1,"cookies":[{"creationUtc":0,"hostKey":".tradingview.com","topFrameSiteKey":"","name":"sessionid","value":"` + secret + `","path":"/","expiresUtc":0,"secure":true,"httpOnly":true,"lastAccessUtc":0,"hasExpires":false,"persistent":false,"priority":1,"sameSite":-1,"sourceScheme":2,"sourcePort":443,"lastUpdateUtc":0,"sourceType":1}]}`),
		[]byte(`{"schemaVersion":1,"cookies":[{"creationUtc":0,"hostKey":".tradingview.com","hostKey":".tradingview.com","topFrameSiteKey":"","name":"sessionid","value":"` + secret + `","path":"/","expiresUtc":0,"secure":true,"httpOnly":true,"lastAccessUtc":0,"hasExpires":false,"persistent":false,"priority":1,"sameSite":-1,"sourceScheme":2,"sourcePort":443,"lastUpdateUtc":0,"sourceType":1,"crossSiteAncestor":false}]}`),
		[]byte(`{"schemaVersion":1,"cookies":[{"creationUtc":0,"hostKey":"evil.example","topFrameSiteKey":"","name":"sessionid","value":"` + secret + `","path":"/","expiresUtc":0,"secure":true,"httpOnly":true,"lastAccessUtc":0,"hasExpires":false,"persistent":false,"priority":1,"sameSite":-1,"sourceScheme":2,"sourcePort":443,"lastUpdateUtc":0,"sourceType":1,"crossSiteAncestor":false}]}`),
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
	payload, _, err := encodeTradingViewAuthentication(tradingViewAuthentication{
		SchemaVersion: tradingViewAuthenticationSchema,
		Cookies:       []tradingViewCookie{testTradingViewCookie("archive-secret")},
	})
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
	for _, required := range []string{tradingViewAuthenticationArchivePath, tradingViewCookieSyncSourceArchivePath} {
		if !entries[required] {
			t.Fatalf("TradingView archive is missing %s: %#v", required, entries)
		}
	}
	for _, forbidden := range []string{"tradingview/Local State", "tradingview/Network/Cookies"} {
		if entries[forbidden] {
			t.Fatalf("TradingView archive contains raw profile state: %s", forbidden)
		}
	}
	if count, err := configurationArchivePayloadFileCount(archive); err != nil || count != 1 {
		t.Fatalf("payload file count = %d, err = %v", count, err)
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
	for _, required := range []string{"Get-Process -Name 'TradingView'", "close it before reapplying authentication"} {
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
