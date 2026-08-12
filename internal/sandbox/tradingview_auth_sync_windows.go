//go:build windows

package sandbox

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	maximumTradingViewCookieDatabaseSize = 64 * 1024 * 1024
	maximumTradingViewLocalStateSize     = 1024 * 1024
	sqliteOK                             = 0
	sqliteRow                            = 100
	sqliteDone                           = 101
	sqliteOpenReadOnly                   = 0x00000001
	sqliteOpenNoFollow                   = 0x01000000
	chromiumCookieDatabaseVersion        = 24
	sqliteInteger                        = 1
	sqliteText                           = 3
	sqliteBlob                           = 4
)

var (
	winSQLiteDLL      = syscall.NewLazyDLL("winsqlite3.dll")
	tradingViewMemory = syscall.NewLazyDLL("kernel32.dll").NewProc("RtlMoveMemory")
	sqliteOpenV2      = winSQLiteDLL.NewProc("sqlite3_open_v2")
	sqliteCloseV2     = winSQLiteDLL.NewProc("sqlite3_close_v2")
	sqliteBusyTimeout = winSQLiteDLL.NewProc("sqlite3_busy_timeout")
	sqlitePrepareV2   = winSQLiteDLL.NewProc("sqlite3_prepare_v2")
	sqliteStep        = winSQLiteDLL.NewProc("sqlite3_step")
	sqliteFinalize    = winSQLiteDLL.NewProc("sqlite3_finalize")
	sqliteColumnInt   = winSQLiteDLL.NewProc("sqlite3_column_int")
	sqliteColumnInt64 = winSQLiteDLL.NewProc("sqlite3_column_int64")
	sqliteColumnType  = winSQLiteDLL.NewProc("sqlite3_column_type")
	sqliteColumnText  = winSQLiteDLL.NewProc("sqlite3_column_text")
	sqliteColumnBlob  = winSQLiteDLL.NewProc("sqlite3_column_blob")
	sqliteColumnBytes = winSQLiteDLL.NewProc("sqlite3_column_bytes")
)

type tradingViewSQLiteDatabase struct {
	handle uintptr
}

func exportTradingViewAuthentication(ctx context.Context, profile string) ([]byte, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if !filepath.IsAbs(profile) {
		return nil, 0, fmt.Errorf("TradingView profile path is not absolute: %q", profile)
	}
	profile = filepath.Clean(profile)
	expectedProfile, err := defaultTradingViewProfilePath()
	if err != nil {
		return nil, 0, err
	}
	if !strings.EqualFold(profile, expectedProfile) {
		return nil, 0, fmt.Errorf("TradingView profile path is not the standard host profile: %q", profile)
	}
	info, err := os.Lstat(profile)
	if errors.Is(err, os.ErrNotExist) {
		return emptyTradingViewAuthenticationPayload()
	}
	if err != nil {
		return nil, 0, fmt.Errorf("inspect host TradingView profile: %w", err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return nil, 0, fmt.Errorf("inspect host TradingView profile: %w", err)
	}
	if reparse || !info.IsDir() {
		return nil, 0, errors.New("host TradingView profile is not a physical directory")
	}
	if err := rejectMappedPathReparsePoints(profile); err != nil {
		return nil, 0, fmt.Errorf("inspect host TradingView profile path: %w", err)
	}
	cookiePath := filepath.Join(profile, "Network", "Cookies")
	cookieInfo, err := boundedTradingViewSourceFile(cookiePath, maximumTradingViewCookieDatabaseSize)
	if errors.Is(err, os.ErrNotExist) {
		return emptyTradingViewAuthenticationPayload()
	}
	if err != nil {
		return nil, 0, fmt.Errorf("inspect host TradingView cookie database: %w", err)
	}
	if cookieInfo.Size() == 0 {
		return emptyTradingViewAuthenticationPayload()
	}
	database, err := openTradingViewSQLiteReadOnly(cookiePath)
	if err != nil {
		return nil, 0, err
	}
	defer database.close()
	if err := database.validateCookieSchema(); err != nil {
		return nil, 0, err
	}
	encryptedCookies, err := database.readSessionCookies(ctx)
	if err != nil {
		return nil, 0, err
	}
	if len(encryptedCookies) == 0 {
		return emptyTradingViewAuthenticationPayload()
	}

	localStatePath := filepath.Join(profile, "Local State")
	localState, err := readProvisioningScript(localStatePath, "host TradingView Local State", maximumTradingViewLocalStateSize)
	if err != nil {
		return nil, 0, err
	}
	key, err := decryptTradingViewProfileKey(localState)
	clear(localState)
	if err != nil {
		return nil, 0, err
	}
	defer clear(key)

	authentication := tradingViewAuthentication{SchemaVersion: tradingViewAuthenticationSchema, Cookies: make([]tradingViewCookie, 0, len(encryptedCookies))}
	for index := range encryptedCookies {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		cookie := encryptedCookies[index]
		value, err := decryptTradingViewCookieValue(key, cookie.HostKey, cookie.Value, cookie.encryptedValue)
		clear(cookie.encryptedValue)
		if err != nil {
			return nil, 0, err
		}
		cookie.Value = value
		if err := validateTradingViewCookie(cookie.tradingViewCookie); err != nil {
			return nil, 0, err
		}
		authentication.Cookies = append(authentication.Cookies, cookie.tradingViewCookie)
	}
	return encodeTradingViewAuthentication(authentication)
}

type encryptedTradingViewCookie struct {
	tradingViewCookie
	encryptedValue []byte
}

func boundedTradingViewSourceFile(path string, maximum int64) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return nil, err
	}
	if reparse || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximum {
		return nil, fmt.Errorf("source is not a bounded regular non-reparse file: %s", path)
	}
	if err := rejectMappedPathReparsePoints(path); err != nil {
		return nil, err
	}
	return info, nil
}

func decryptTradingViewProfileKey(localState []byte) ([]byte, error) {
	var state struct {
		OSCrypt struct {
			EncryptedKey string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	decoder := json.NewDecoder(bytes.NewReader(localState))
	if err := decoder.Decode(&state); err != nil {
		return nil, errors.New("decode host TradingView Local State")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode host TradingView Local State: trailing JSON data")
	}
	protected, err := base64.StdEncoding.DecodeString(state.OSCrypt.EncryptedKey)
	if err != nil || len(protected) <= len("DPAPI") || !bytes.Equal(protected[:len("DPAPI")], []byte("DPAPI")) {
		clear(protected)
		return nil, errors.New("host TradingView profile key has an unsupported format")
	}
	key, err := unprotectLocalData(protected[len("DPAPI"):])
	clear(protected)
	if err != nil {
		return nil, fmt.Errorf("decrypt host TradingView profile key: %w", err)
	}
	if len(key) != 32 {
		clear(key)
		return nil, errors.New("host TradingView profile key has an unsupported length")
	}
	return key, nil
}

func decryptTradingViewCookieValue(key []byte, hostKey, plaintext string, encrypted []byte) (string, error) {
	if plaintext != "" {
		return "", errors.New("host TradingView session cookie is not encrypted")
	}
	if len(encrypted) < len("v10")+12+16 || !bytes.Equal(encrypted[:len("v10")], []byte("v10")) {
		return "", errors.New("host TradingView session cookie encryption is unsupported")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", errors.New("initialize host TradingView session decryption")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errors.New("initialize host TradingView authenticated decryption")
	}
	nonceStart := len("v10")
	nonceEnd := nonceStart + gcm.NonceSize()
	decrypted, err := gcm.Open(nil, encrypted[nonceStart:nonceEnd], encrypted[nonceEnd:], nil)
	if err != nil {
		return "", errors.New("decrypt host TradingView session cookie")
	}
	defer clear(decrypted)
	domainHash := sha256.Sum256([]byte(hostKey))
	if len(decrypted) <= len(domainHash) || !bytes.Equal(decrypted[:len(domainHash)], domainHash[:]) {
		return "", errors.New("host TradingView session cookie domain binding is invalid")
	}
	return string(decrypted[len(domainHash):]), nil
}

func openTradingViewSQLiteReadOnly(path string) (*tradingViewSQLiteDatabase, error) {
	encoded := append([]byte(path), 0)
	var handle uintptr
	result, _, _ := sqliteOpenV2.Call(
		uintptr(unsafe.Pointer(&encoded[0])),
		uintptr(unsafe.Pointer(&handle)),
		sqliteOpenReadOnly|sqliteOpenNoFollow,
		0,
	)
	runtime.KeepAlive(encoded)
	if int(result) != sqliteOK || handle == 0 {
		if handle != 0 {
			sqliteCloseV2.Call(handle)
		}
		return nil, fmt.Errorf("open host TradingView cookie database: SQLite code %d", result)
	}
	if busyResult, _, _ := sqliteBusyTimeout.Call(handle, 5000); int(busyResult) != sqliteOK {
		sqliteCloseV2.Call(handle)
		return nil, fmt.Errorf("configure host TradingView cookie database timeout: SQLite code %d", busyResult)
	}
	return &tradingViewSQLiteDatabase{handle: handle}, nil
}

func (database *tradingViewSQLiteDatabase) close() {
	if database != nil && database.handle != 0 {
		sqliteCloseV2.Call(database.handle)
		database.handle = 0
	}
}

func (database *tradingViewSQLiteDatabase) validateCookieSchema() error {
	for _, key := range []string{"version", "last_compatible_version"} {
		value, found, err := database.readOneText("SELECT value FROM meta WHERE key='" + key + "'")
		if err != nil {
			return err
		}
		version, parseErr := strconv.Atoi(value)
		if !found || parseErr != nil || version != chromiumCookieDatabaseVersion {
			return fmt.Errorf("host TradingView cookie database %s is unsupported", key)
		}
	}
	statement, err := database.prepare("PRAGMA table_info(cookies)")
	if err != nil {
		return err
	}
	defer sqliteFinalize.Call(statement)
	columns := []string{}
	for {
		result, _, _ := sqliteStep.Call(statement)
		switch int(result) {
		case sqliteRow:
			name, err := sqliteStatementText(statement, 1, 128)
			if err != nil {
				return err
			}
			columns = append(columns, name)
		case sqliteDone:
			expected := []string{"creation_utc", "host_key", "top_frame_site_key", "name", "value", "encrypted_value", "path", "expires_utc", "is_secure", "is_httponly", "last_access_utc", "has_expires", "is_persistent", "priority", "samesite", "source_scheme", "source_port", "last_update_utc", "source_type", "has_cross_site_ancestor"}
			if strings.Join(columns, "|") != strings.Join(expected, "|") {
				return errors.New("host TradingView cookie database columns are unsupported")
			}
			return nil
		default:
			return fmt.Errorf("inspect host TradingView cookie database columns: SQLite code %d", result)
		}
	}
}

func (database *tradingViewSQLiteDatabase) readOneText(query string) (string, bool, error) {
	statement, err := database.prepare(query)
	if err != nil {
		return "", false, err
	}
	defer sqliteFinalize.Call(statement)
	result, _, _ := sqliteStep.Call(statement)
	if int(result) == sqliteDone {
		return "", false, nil
	}
	if int(result) != sqliteRow {
		return "", false, fmt.Errorf("inspect host TradingView cookie database metadata: SQLite code %d", result)
	}
	value, err := sqliteStatementText(statement, 0, 128)
	if err != nil {
		return "", false, err
	}
	if next, _, _ := sqliteStep.Call(statement); int(next) != sqliteDone {
		return "", false, errors.New("host TradingView cookie database metadata is duplicated")
	}
	return value, true, nil
}

func (database *tradingViewSQLiteDatabase) readSessionCookies(ctx context.Context) ([]encryptedTradingViewCookie, error) {
	const query = `SELECT creation_utc, host_key, top_frame_site_key, name, value, encrypted_value, path,
expires_utc, is_secure, is_httponly, last_access_utc, has_expires, is_persistent, priority,
samesite, source_scheme, source_port, last_update_utc, source_type, has_cross_site_ancestor
FROM cookies WHERE name='sessionid' AND top_frame_site_key='' AND has_cross_site_ancestor=0
AND (lower(host_key)='tradingview.com' OR lower(host_key)='.tradingview.com' OR lower(host_key) LIKE '%.tradingview.com')
ORDER BY lower(host_key), path, source_scheme, source_port`
	statement, err := database.prepare(query)
	if err != nil {
		return nil, err
	}
	defer sqliteFinalize.Call(statement)
	cookies := []encryptedTradingViewCookie{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, _, _ := sqliteStep.Call(statement)
		switch int(result) {
		case sqliteRow:
			if len(cookies) >= maximumTradingViewCookies {
				return nil, fmt.Errorf("host TradingView session cookie count exceeds %d", maximumTradingViewCookies)
			}
			cookie, err := readTradingViewCookieRow(statement)
			if err != nil {
				return nil, err
			}
			cookies = append(cookies, cookie)
		case sqliteDone:
			return cookies, nil
		default:
			return nil, fmt.Errorf("read host TradingView session cookies: SQLite code %d", result)
		}
	}
}

func readTradingViewCookieRow(statement uintptr) (encryptedTradingViewCookie, error) {
	expectedTypes := []int{
		sqliteInteger, sqliteText, sqliteText, sqliteText, sqliteText, sqliteBlob, sqliteText,
		sqliteInteger, sqliteInteger, sqliteInteger, sqliteInteger, sqliteInteger, sqliteInteger,
		sqliteInteger, sqliteInteger, sqliteInteger, sqliteInteger, sqliteInteger, sqliteInteger, sqliteInteger,
	}
	for column, expected := range expectedTypes {
		actual, _, _ := sqliteColumnType.Call(statement, uintptr(column))
		if int(actual) != expected {
			return encryptedTradingViewCookie{}, errors.New("host TradingView cookie column storage type is invalid")
		}
	}
	hostKey, err := sqliteStatementText(statement, 1, 512)
	if err != nil {
		return encryptedTradingViewCookie{}, err
	}
	topFrame, err := sqliteStatementText(statement, 2, 4096)
	if err != nil {
		return encryptedTradingViewCookie{}, err
	}
	name, err := sqliteStatementText(statement, 3, 128)
	if err != nil {
		return encryptedTradingViewCookie{}, err
	}
	value, err := sqliteStatementText(statement, 4, maximumTradingViewCookieValueSize)
	if err != nil {
		return encryptedTradingViewCookie{}, err
	}
	encrypted, err := sqliteStatementBlob(statement, 5, maximumTradingViewCookieValueSize+128)
	if err != nil {
		return encryptedTradingViewCookie{}, err
	}
	path, err := sqliteStatementText(statement, 6, 1024)
	if err != nil {
		clear(encrypted)
		return encryptedTradingViewCookie{}, err
	}
	intValue := func(column int) int {
		value, _, _ := sqliteColumnInt.Call(statement, uintptr(column))
		return int(int32(value))
	}
	int64Value := func(column int) int64 {
		value, _, _ := sqliteColumnInt64.Call(statement, uintptr(column))
		return int64(value)
	}
	boolValue := func(column int) (bool, error) {
		value := intValue(column)
		if value != 0 && value != 1 {
			return false, errors.New("host TradingView cookie boolean metadata is invalid")
		}
		return value == 1, nil
	}
	secure, err := boolValue(8)
	if err != nil {
		clear(encrypted)
		return encryptedTradingViewCookie{}, err
	}
	httpOnly, err := boolValue(9)
	if err != nil {
		clear(encrypted)
		return encryptedTradingViewCookie{}, err
	}
	hasExpires, err := boolValue(11)
	if err != nil {
		clear(encrypted)
		return encryptedTradingViewCookie{}, err
	}
	persistent, err := boolValue(12)
	if err != nil {
		clear(encrypted)
		return encryptedTradingViewCookie{}, err
	}
	crossSiteAncestor, err := boolValue(19)
	if err != nil {
		clear(encrypted)
		return encryptedTradingViewCookie{}, err
	}
	return encryptedTradingViewCookie{tradingViewCookie: tradingViewCookie{
		CreationUTC:       int64Value(0),
		HostKey:           hostKey,
		TopFrameSiteKey:   topFrame,
		Name:              name,
		Value:             value,
		Path:              path,
		ExpiresUTC:        int64Value(7),
		Secure:            secure,
		HTTPOnly:          httpOnly,
		LastAccessUTC:     int64Value(10),
		HasExpires:        hasExpires,
		Persistent:        persistent,
		Priority:          intValue(13),
		SameSite:          intValue(14),
		SourceScheme:      intValue(15),
		SourcePort:        intValue(16),
		LastUpdateUTC:     int64Value(17),
		SourceType:        intValue(18),
		CrossSiteAncestor: crossSiteAncestor,
	}, encryptedValue: encrypted}, nil
}

func (database *tradingViewSQLiteDatabase) prepare(query string) (uintptr, error) {
	encoded := append([]byte(query), 0)
	var statement uintptr
	result, _, _ := sqlitePrepareV2.Call(
		database.handle,
		uintptr(unsafe.Pointer(&encoded[0])),
		uintptr(len(encoded)-1),
		uintptr(unsafe.Pointer(&statement)),
		0,
	)
	runtime.KeepAlive(encoded)
	if int(result) != sqliteOK || statement == 0 {
		return 0, fmt.Errorf("prepare host TradingView cookie query: SQLite code %d", result)
	}
	return statement, nil
}

func sqliteStatementText(statement uintptr, column, maximum int) (string, error) {
	pointer, _, _ := sqliteColumnText.Call(statement, uintptr(column))
	length, _, _ := sqliteColumnBytes.Call(statement, uintptr(column))
	if int(length) < 0 || int(length) > maximum {
		return "", errors.New("host TradingView cookie text exceeds its bound")
	}
	if length == 0 {
		return "", nil
	}
	if pointer == 0 {
		return "", errors.New("host TradingView cookie text is unavailable")
	}
	data := make([]byte, int(length))
	tradingViewMemory.Call(uintptr(unsafe.Pointer(&data[0])), pointer, length)
	runtime.KeepAlive(data)
	return string(data), nil
}

func sqliteStatementBlob(statement uintptr, column, maximum int) ([]byte, error) {
	pointer, _, _ := sqliteColumnBlob.Call(statement, uintptr(column))
	length, _, _ := sqliteColumnBytes.Call(statement, uintptr(column))
	if int(length) < 0 || int(length) > maximum {
		return nil, errors.New("host TradingView cookie ciphertext exceeds its bound")
	}
	if length == 0 {
		return nil, nil
	}
	if pointer == 0 {
		return nil, errors.New("host TradingView cookie ciphertext is unavailable")
	}
	data := make([]byte, int(length))
	tradingViewMemory.Call(uintptr(unsafe.Pointer(&data[0])), pointer, length)
	runtime.KeepAlive(data)
	return data, nil
}
