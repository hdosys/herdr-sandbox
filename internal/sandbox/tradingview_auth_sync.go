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

//go:embed assets/tradingview-settings.json
var tradingViewInitialSettings []byte

const (
	tradingViewAuthenticationSchema        = 3
	tradingViewAuthenticationArchivePath   = "tradingview/authentication.json"
	tradingViewCookieSyncSourceArchivePath = "herdr-sandbox/tradingview-cookie-sync.cs"
	tradingViewSettingsArchivePath         = "herdr-sandbox/tradingview-settings.json"
	tradingViewDesktopPackageFamilyName    = "TradingView.Desktop_n534cwy3pjxzj"
	maximumTradingViewAuthenticationSize   = 64 * 1024
	maximumTradingViewCookies              = 4
	maximumTradingViewCookieValueSize      = 16 * 1024
	maximumTradingViewUserIDs              = 8
	tradingViewSessionCookieName           = "sessionid"
	tradingViewSessionSignatureCookieName  = "sessionid_sign"
)

var tradingViewCookieJSONFields = []string{
	"creationUtc", "hostKey", "topFrameSiteKey", "name", "value", "path", "expiresUtc",
	"secure", "httpOnly", "lastAccessUtc", "hasExpires", "persistent", "priority", "sameSite",
	"sourceScheme", "sourcePort", "lastUpdateUtc", "sourceType", "crossSiteAncestor",
}

var tradingViewAuthenticationJSONFields = []string{"schemaVersion", "cookies", "userIds"}

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
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if !filepath.IsAbs(localAppData) {
		return "", fmt.Errorf("LOCALAPPDATA is not absolute: %q", localAppData)
	}
	return filepath.Clean(filepath.Join(localAppData, "Packages", tradingViewDesktopPackageFamilyName,
		"LocalCache", "Roaming", "TradingView")), nil
}

type tradingViewAuthentication struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Cookies       []tradingViewCookie `json:"cookies"`
	UserIDs       []string            `json:"userIds"`
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
		UserIDs:       []string{},
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
	if err := validateExactJSONObjectShape(payload, "TradingView authentication", tradingViewAuthenticationJSONFields); err != nil {
		return tradingViewAuthentication{}, errors.New("decode TradingView authentication payload")
	}
	var envelope struct {
		SchemaVersion int               `json:"schemaVersion"`
		Cookies       []json.RawMessage `json:"cookies"`
		UserIDs       []string          `json:"userIds"`
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
		UserIDs:       envelope.UserIDs,
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
	if authentication.UserIDs == nil || len(authentication.UserIDs) > maximumTradingViewUserIDs {
		return fmt.Errorf("TradingView user ID count exceeds %d", maximumTradingViewUserIDs)
	}
	seenUserIDs := make(map[string]bool, len(authentication.UserIDs))
	for index, userID := range authentication.UserIDs {
		if !validTradingViewUserID(userID) || seenUserIDs[userID] || (index > 0 && authentication.UserIDs[index-1] >= userID) {
			return errors.New("TradingView authentication contains invalid user IDs")
		}
		seenUserIDs[userID] = true
	}
	if len(authentication.Cookies) > 0 && len(authentication.UserIDs) == 0 {
		return errors.New("TradingView authentication is missing user settings identity")
	}
	seen := make(map[string]bool, len(authentication.Cookies))
	pairs := make(map[string]uint8, len(authentication.Cookies)/2)
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
		pairIdentity := strings.ToLower(cookie.HostKey) + "\x00" + cookie.TopFrameSiteKey + "\x00" + cookie.Path +
			fmt.Sprintf("\x00%d\x00%d\x00%t", cookie.SourceScheme, cookie.SourcePort, cookie.CrossSiteAncestor)
		if cookie.Name == tradingViewSessionCookieName {
			pairs[pairIdentity] |= 1
		} else {
			pairs[pairIdentity] |= 2
		}
	}
	for _, pair := range pairs {
		if pair != 3 {
			return errors.New("TradingView authentication contains an incomplete signed session")
		}
	}
	return nil
}

func validTradingViewUserID(value string) bool {
	if value == "" || len(value) > 19 || value[0] == '0' {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validateTradingViewCookie(cookie tradingViewCookie) error {
	if !tradingViewAuthenticationCookieNameAllowed(cookie.Name) || !tradingViewCookieHostAllowed(cookie.HostKey) {
		return errors.New("TradingView authentication contains a cookie outside the session allowlist")
	}
	if cookie.TopFrameSiteKey != "" || !cookie.CrossSiteAncestor {
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

func tradingViewAuthenticationCookieNameAllowed(name string) bool {
	return name == tradingViewSessionCookieName || name == tradingViewSessionSignatureCookieName
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
