package sandbox

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"time"
)

//go:embed assets/herdr-config-read.ps1
var guestHerdrConfigurationReadScript string

func readGuestHerdrConfiguration(ctx context.Context, connection Connection) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	output, err := runSSHPowerShell(ctx, connection, nil, guestHerdrConfigurationReadScript, "read guest Herdr configuration", 2*1024*1024)
	if err != nil {
		return nil, err
	}
	defer clear(output)
	return decodeGuestHerdrConfiguration(output)
}

func decodeGuestHerdrConfiguration(output []byte) ([]byte, error) {
	if bytes.Equal(output, []byte("missing")) {
		return nil, nil
	}
	encoded, found := bytes.CutPrefix(output, []byte("present:"))
	if !found {
		return nil, errors.New("invalid guest Herdr configuration snapshot")
	}
	decoded := make([]byte, base64.StdEncoding.DecodedLen(len(encoded)))
	n, err := base64.StdEncoding.Decode(decoded, encoded)
	if err != nil || n > 1024*1024 {
		clear(decoded)
		return nil, errors.New("invalid guest Herdr configuration snapshot encoding or size")
	}
	return decoded[:n], nil
}
