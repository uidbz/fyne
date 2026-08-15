//go:build android || ios || mobile || wasm || test_web_driver

package cache

import "time"

// defaultValidDuration keeps the upstream 1-minute cache lifetime on
// memory-constrained platforms (mobile, wasm).
const defaultValidDuration = 1 * time.Minute
