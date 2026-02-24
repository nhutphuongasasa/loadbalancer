package resilience

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"log/slog"
)

type ResilientTransport struct {
	base        http.RoundTripper
	breaker     CircuitBreaker
	retryPolicy RetryPolicy
	logger      *slog.Logger
}

func NewResilientTransport(
	base http.RoundTripper,
	breaker CircuitBreaker,
	retryPolicy RetryPolicy,
	logger *slog.Logger,
) *ResilientTransport {
	if logger == nil {
		logger = slog.Default()
	}
	if base == nil {
		base = http.DefaultTransport
	}

	return &ResilientTransport{
		base:        base,
		breaker:     breaker,
		retryPolicy: retryPolicy,
		logger:      logger,
	}
}

func (t *ResilientTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

	var resp *http.Response

	err := t.retryPolicy.Do(func() error {
		_, cbErr := t.breaker.Execute(func() (interface{}, error) {
			cloneReq := req.Clone(req.Context())
			if len(bodyBytes) > 0 {
				cloneReq.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}

			var rErr error
			resp, rErr = t.base.RoundTrip(cloneReq)
			if rErr != nil {
				return nil, rErr
			}

			if resp.StatusCode >= 500 {
				_ = resp.Body.Close()
				resp = nil
				return nil, fmt.Errorf("backend error status: %d", resp.StatusCode)
			}

			return resp, nil
		})

		if cbErr != nil {
			t.logger.Warn("Circuit breaker prevented execution",
				slog.String("error", cbErr.Error()),
				slog.String("state", t.breaker.State().String()),
			)
			return cbErr
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return resp, nil
}
