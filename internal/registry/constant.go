package registry

import "time"

const (
	MaxBatchSize           = 20
	MaxConcurrentCheck     = 5
	MaxInstancesPerService = 50
	DefaultMaxFailures     = 3
	DefaultTimeout         = 5 * time.Second
	DefaultInterval        = 10 * time.Second
	DefaultMaxRetries      = 3
	DefaultBaseDelay       = 200 * time.Millisecond
	DefaultMaxDelay        = 3 * time.Second
	DefaultJitterFactor    = 0.2
)

type VersionData int64

var (
	VersionDataBackend VersionData = 1
)
