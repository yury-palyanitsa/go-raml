package raml

import (
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

const (
	// DefaultHTTPTimeout is the end-to-end timeout applied to every request
	// made by NewHTTPClient.
	DefaultHTTPTimeout = 30 * time.Second

	// httpRetryMax is the total number of attempts (initial + retries).
	httpRetryMax = 3

	// httpRetryBaseDelay is the back-off duration before the first retry.
	httpRetryBaseDelay = 500 * time.Millisecond

	// httpRetryMaxDelay caps the computed back-off so it never exceeds 10 s.
	httpRetryMaxDelay = 10 * time.Second

	// HTTPUserAgent is the User-Agent header value sent on every request.
	HTTPUserAgent = "go-raml/2.0"
)

// retryTransport is an http.RoundTripper that:
//   - Injects a User-Agent header on every outgoing request.
//   - Retries transient failures (network errors, 429, 5xx) up to maxTries
//     total attempts using exponential back-off with ±25 % jitter.
//   - Respects the Retry-After header for 429 responses.
type retryTransport struct {
	base     http.RoundTripper
	maxTries int
}

// retryableStatus reports whether an HTTP status code warrants a retry.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone so we can add headers without mutating the caller's Request.
	r := req.Clone(req.Context())
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	r.Header.Set("User-Agent", HTTPUserAgent)

	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	var (
		resp *http.Response
		err  error
	)

	for attempt := 0; attempt < t.maxTries; attempt++ {
		if attempt > 0 {
			delay := httpBackoffDelay(attempt)

			// Prefer the server-supplied Retry-After delay for 429 responses.
			if resp != nil {
				if ra := resp.Header.Get("Retry-After"); ra != "" {
					if secs, convErr := strconv.Atoi(ra); convErr == nil {
						if d := time.Duration(secs) * time.Second; d > delay {
							delay = d
						}
					}
				}
				_ = resp.Body.Close()
				resp = nil
			}

			// Wait for the back-off period, but bail immediately on context cancellation.
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(delay):
			}
		}

		resp, err = base.RoundTrip(r)
		if err != nil {
			// Network-level error — retry.
			continue
		}
		if !retryableStatus(resp.StatusCode) {
			return resp, nil
		}
	}

	if err != nil {
		return nil, err
	}
	return resp, nil
}

// httpBackoffDelay returns exponential back-off with ±25 % jitter for the
// given 1-based attempt number.
//
//	attempt 1 → ~500 ms
//	attempt 2 → ~1 s
//	attempt 3 → ~2 s  (and so on, capped at httpRetryMaxDelay)
func httpBackoffDelay(attempt int) time.Duration {
	// Shift left safely: cap the exponent to avoid overflow on large attempt counts.
	exp := attempt - 1
	if exp > 10 {
		exp = 10
	}
	d := httpRetryBaseDelay * (1 << exp) //nolint:gosec // non-cryptographic shift
	if d > httpRetryMaxDelay {
		d = httpRetryMaxDelay
	}
	// ±25 % jitter: random value in [-d/4, +d/4).
	jitter := time.Duration(rand.Int63n(int64(d/2))) - d/4 //nolint:gosec
	return d + jitter
}

// NewHTTPClient returns an *http.Client suitable for loading remote RAML
// fragments over HTTP or HTTPS. The client provides:
//   - A 30-second end-to-end request timeout (DefaultHTTPTimeout).
//   - A User-Agent header of "go-raml/v2" on every request.
//   - Automatic retries (up to 3 total attempts) for transient server
//     errors (500, 502, 503, 504) and rate-limit responses (429),
//     using exponential back-off with ±25 % jitter. The Retry-After
//     header is honoured for 429 responses.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout: DefaultHTTPTimeout,
		Transport: &retryTransport{
			base:     http.DefaultTransport,
			maxTries: httpRetryMax,
		},
	}
}
