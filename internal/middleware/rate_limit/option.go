package rate_limit

import "log/slog"

type Option func(*ipRateLimiter)

func WithTrustedProxies(proxies ...string) Option {
	return func(i *ipRateLimiter) {
		i.trustedProxies = TrustedProxies(proxies)
	}
}

func WithLogger(logger *slog.Logger) Option {
	return func(i *ipRateLimiter) {
		i.logger = logger
	}
}
