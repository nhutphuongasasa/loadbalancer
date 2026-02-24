package provider

import "time"

const (
	defaultMaxFailures  = 3
	defaultTimeout      = 5 * time.Second
	defaultInterval     = 10 * time.Second
	defaultMaxRetries   = 3
	defaultBaseDelay    = 200 * time.Millisecond
	defaultMaxDelay     = 3 * time.Second
	defaultJitterFactor = 0.2
)
