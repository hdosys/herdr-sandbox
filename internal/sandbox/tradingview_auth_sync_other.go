//go:build !windows

package sandbox

import "context"

func exportTradingViewAuthentication(context.Context, string) ([]byte, int, error) {
	return emptyTradingViewAuthenticationPayload()
}
