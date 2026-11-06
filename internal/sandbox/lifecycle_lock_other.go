//go:build !windows

package sandbox

import (
	"context"
	"errors"
	"sync"
)

func acquireNamedLifecycleLock(context.Context, string) (func() error, error) {
	return nil, errors.New("named setup locks require Windows")
}

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
