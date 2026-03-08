package memory

import "time"

const (
	maxBatchSize           = 20
	maxConcurrentCheck     = 5
	maxInstancesPerService = 50
	defaultMaxFailures     = 3
	defaultTimeout         = 5 * time.Second
	defaultInterval        = 10 * time.Second
	defaultMaxRetries      = 3
	defaultBaseDelay       = 200 * time.Millisecond
	defaultMaxDelay        = 3 * time.Second
	defaultJitterFactor    = 0.2
)
