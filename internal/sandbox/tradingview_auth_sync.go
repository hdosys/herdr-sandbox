package sandbox

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

//go:embed assets/tradingview-cookie-sync.cs
var tradingViewCookieSyncSource []byte

const (
	tradingViewAuthenticationSchema        = 1
	tradingViewAuthenticationArchivePath   = "tradingview/authentication.json"
	tradingViewCookieSyncSourceArchivePath = "herdr-sandbox/tradingview-cookie-sync.cs"
	maximumTradingViewAuthenticationSize   = 64 * 1024
	maximumTradingViewCookies              = 4
	maximumTradingViewCookieValueSize      = 16 * 1024
	tradingViewSessionCookieName           = "sessionid"
)

var tradingViewCookieJSONFields = []string{
	"creationUtc", "hostKey", "topFrameSiteKey", "name", "value", "path", "expiresUtc",
	"secure", "httpOnly", "lastAccessUtc", "hasExpires", "persistent", "priority", "sameSite",
	"sourceScheme", "sourcePort", "lastUpdateUtc", "sourceType", "crossSiteAncestor",
}

func provisioningStacksContain(userStacks []projectStack, workspaces []workspacePlan, expected projectStack) bool {
	if stacksContain(userStacks, expected) {
		return true
	}
	for _, workspace := range workspaces {
		if stacksContain(workspace.Stacks, expected) {
			return true
		}
	}
	return false
}

func defaultTradingViewProfilePath() (string, error) {
	appData := strings.TrimSpace(os.Getenv("APPDATA"))
	if !filepath.IsAbs(appData) {
		return "", fmt.Errorf("APPDATA is not absolute: %q", appData)
	}
	return filepath.Clean(filepath.Join(appData, "TradingView")), nil
}

type tradingViewAuthentication struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Cookies       []tradingViewCookie `json:"cookies"`
}

type tradingViewCookie struct {
	CreationUTC       int64  `json:"creationUtc"`
	HostKey           string `json:"hostKey"`
	TopFrameSiteKey   string `json:"topFrameSiteKey"`
	Name              string `json:"name"`
	Value             string `json:"value"`
	Path              string `json:"path"`
	ExpiresUTC        int64  `json:"expiresUtc"`
	Secure            bool   `json:"secure"`
	HTTPOnly          bool   `json:"httpOnly"`
	LastAccessUTC     int64  `json:"lastAccessUtc"`
	HasExpires        bool   `json:"hasExpires"`
	Persistent        bool   `json:"persistent"`
	Priority          int    `json:"priority"`
	SameSite          int    `json:"sameSite"`
	SourceScheme      int    `json:"sourceScheme"`
	SourcePort        int    `json:"sourcePort"`
	LastUpdateUTC     int64  `json:"lastUpdateUtc"`
	SourceType        int    `json:"sourceType"`
	CrossSiteAncestor bool   `json:"crossSiteAncestor"`
}

func emptyTradingViewAuthenticationPayload() ([]byte, int, error) {
	return encodeTradingViewAuthentication(tradingViewAuthentication{
		SchemaVersion: tradingViewAuthenticationSchema,
		Cookies:       []tradingViewCookie{},
	})
}

func encodeTradingViewAuthentication(authentication tradingViewAuthentication) ([]byte, int, error) {
	if err := validateTradingViewAuthentication(authentication); err != nil {
		return nil, 0, err
	}
	payload, err := json.Marshal(authentication)
	if err != nil {
		return nil, 0, fmt.Errorf("encode TradingView authentication: %w", err)
	}
	if len(payload) > maximumTradingViewAuthenticationSize {
		return nil, 0, fmt.Errorf("TradingView authentication exceeds %d bytes", maximumTradingViewAuthenticationSize)
	}
	return payload, len(authentication.Cookies), nil
}

func decodeTradingViewAuthentication(payload []byte) (tradingViewAuthentication, error) {
	if len(payload) == 0 || len(payload) > maximumTradingViewAuthenticationSize {
		return tradingViewAuthentication{}, errors.New("TradingView authentication payload size is invalid")
	}
	if err := validateExactJSONObjectShape(payload, "TradingView authentication", []string{"schemaVersion", "cookies"}); err != nil {
		return tradingViewAuthentication{}, errors.New("decode TradingView authentication payload")
	}
	var envelope struct {
		SchemaVersion int               `json:"schemaVersion"`
		Cookies       []json.RawMessage `json:"cookies"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return tradingViewAuthentication{}, errors.New("decode TradingView authentication payload")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return tradingViewAuthentication{}, errors.New("decode TradingView authentication payload: trailing JSON data")
	}
	authentication := tradingViewAuthentication{
		SchemaVersion: envelope.SchemaVersion,
		Cookies:       make([]tradingViewCookie, 0, len(envelope.Cookies)),
	}
	if envelope.Cookies == nil || len(envelope.Cookies) > maximumTradingViewCookies {
		return tradingViewAuthentication{}, fmt.Errorf("TradingView authentication cookie count exceeds %d", maximumTradingViewCookies)
	}
	for _, rawCookie := range envelope.Cookies {
		if err := validateExactJSONObjectShape(rawCookie, "TradingView authentication cookie", tradingViewCookieJSONFields); err != nil {
			return tradingViewAuthentication{}, errors.New("decode TradingView authentication cookie")
		}
		var cookie tradingViewCookie
		cookieDecoder := json.NewDecoder(bytes.NewReader(rawCookie))
		cookieDecoder.DisallowUnknownFields()
		if err := cookieDecoder.Decode(&cookie); err != nil {
			return tradingViewAuthentication{}, errors.New("decode TradingView authentication cookie")
		}
		if err := cookieDecoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return tradingViewAuthentication{}, errors.New("decode TradingView authentication cookie: trailing JSON data")
		}
		authentication.Cookies = append(authentication.Cookies, cookie)
	}
	if err := validateTradingViewAuthentication(authentication); err != nil {
		return tradingViewAuthentication{}, err
	}
	return authentication, nil
}

func validateTradingViewAuthentication(authentication tradingViewAuthentication) error {
	if authentication.SchemaVersion != tradingViewAuthenticationSchema {
		return fmt.Errorf("unsupported TradingView authentication schema %d", authentication.SchemaVersion)
	}
	if authentication.Cookies == nil || len(authentication.Cookies) > maximumTradingViewCookies {
		return fmt.Errorf("TradingView authentication cookie count exceeds %d", maximumTradingViewCookies)
	}
	seen := make(map[string]bool, len(authentication.Cookies))
	for _, cookie := range authentication.Cookies {
		if err := validateTradingViewCookie(cookie); err != nil {
			return err
		}
		identity := strings.ToLower(cookie.HostKey) + "\x00" + cookie.TopFrameSiteKey + "\x00" + cookie.Name + "\x00" +
			cookie.Path + fmt.Sprintf("\x00%d\x00%d\x00%t", cookie.SourceScheme, cookie.SourcePort, cookie.CrossSiteAncestor)
		if seen[identity] {
			return errors.New("TradingView authentication contains duplicate session cookies")
		}
		seen[identity] = true
	}
	return nil
}

func validateTradingViewCookie(cookie tradingViewCookie) error {
	if cookie.Name != tradingViewSessionCookieName || len(cookie.Name) > 128 || !tradingViewCookieHostAllowed(cookie.HostKey) {
		return errors.New("TradingView authentication contains a cookie outside the session allowlist")
	}
	if cookie.TopFrameSiteKey != "" || cookie.CrossSiteAncestor {
		return errors.New("TradingView authentication contains a partitioned session cookie")
	}
	if cookie.Path == "" || len(cookie.Path) > 1024 || cookie.Path[0] != '/' || strings.ContainsAny(cookie.Path, "\x00\r\n") {
		return errors.New("TradingView authentication cookie path is invalid")
	}
	if !validTradingViewCookieValue(cookie.Value) {
		return errors.New("TradingView authentication cookie value is invalid")
	}
	if cookie.CreationUTC < 0 || cookie.ExpiresUTC < 0 || cookie.LastAccessUTC < 0 || cookie.LastUpdateUTC < 0 ||
		cookie.Priority < 0 || cookie.Priority > 2 || cookie.SameSite < -1 || cookie.SameSite > 2 ||
		cookie.SourceScheme < 0 || cookie.SourceScheme > 2 || cookie.SourceType < 0 || cookie.SourceType > 3 ||
		cookie.SourcePort < -1 || cookie.SourcePort > 65535 {
		return errors.New("TradingView authentication cookie metadata is invalid")
	}
	if cookie.HasExpires != cookie.Persistent || (cookie.HasExpires && cookie.ExpiresUTC == 0) {
		return errors.New("TradingView authentication cookie persistence is inconsistent")
	}
	return nil
}

func tradingViewCookieHostAllowed(host string) bool {
	if host == "" || len(host) > 512 || strings.TrimSpace(host) != host || strings.ContainsAny(host, "\x00\r\n") {
		return false
	}
	folded := strings.ToLower(host)
	return folded == "tradingview.com" || folded == ".tradingview.com" || strings.HasSuffix(folded, ".tradingview.com")
}

func validTradingViewCookieValue(value string) bool {
	if value == "" || len(value) > maximumTradingViewCookieValueSize || !utf8.ValidString(value) {
		return false
	}
	for _, octet := range []byte(value) {
		if octet != 0x21 && (octet < 0x23 || octet > 0x2b) && (octet < 0x2d || octet > 0x3a) &&
			(octet < 0x3c || octet > 0x5b) && (octet < 0x5d || octet > 0x7e) {
			return false
		}
	}
	return true
}
