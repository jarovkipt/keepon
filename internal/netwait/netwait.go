// Package netwait blocks until DNS resolution works, so sessions started at
// boot don't launch their command before the network is up.
package netwait

import (
	"context"
	"fmt"
	"net"
	"time"
)

// Wait polls until host resolves or timeout elapses.
func Wait(host string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := net.DefaultResolver.LookupHost(ctx, host)
		cancel()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("network not ready after %s (resolving %s): %w", timeout, host, err)
		}
		time.Sleep(3 * time.Second)
	}
}
