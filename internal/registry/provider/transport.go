package provider

import (
	"fmt"
	"net/http"

	"log/slog"

	"github.com/nhutphuongasasa/loadbalancer/internal/resilience"
)

/*
*Khoi tao http.RoundTripper voi retry circuit breaker
 */
func (p *ProviderServer) createResilientTransport(
	serviceName, instanceID string,
	baseTransport http.RoundTripper,
	logger *slog.Logger,
) http.RoundTripper {
	breakerName := fmt.Sprintf("cb-%s-%s", serviceName, instanceID)

	breaker := resilience.NewSonyGoBreaker(
		breakerName,
		defaultMaxFailures,
		defaultTimeout,
		defaultInterval,
		logger,
	)

	retryPol := resilience.NewExponentialRetry(
		defaultMaxRetries,
		defaultBaseDelay,
		defaultMaxDelay,
		defaultJitterFactor,
		logger,
	)

	return resilience.NewResilientTransport(
		baseTransport,
		breaker,
		retryPol,
		logger,
	)
}
