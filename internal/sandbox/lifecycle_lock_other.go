//go:build !windows

package sandbox

import (
	"context"
	"sync"
)

var lifecycleLockToken = func() chan struct{} {
	token := make(chan struct{}, 1)
	token <- struct{}{}
	return token
}()

func acquireLifecycleLock(ctx context.Context) (func() error, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-lifecycleLockToken:
	}
	var once sync.Once
	return func() error {
		once.Do(func() { lifecycleLockToken <- struct{}{} })
		return nil
	}, nil
}
