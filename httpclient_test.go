package raml

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPClient(t *testing.T) {
	c := NewHTTPClient()
	require.NotNil(t, c)
	assert.Equal(t, DefaultHTTPTimeout, c.Timeout)

	rt, ok := c.Transport.(*retryTransport)
	require.True(t, ok, "Transport should be *retryTransport")
	assert.Equal(t, httpRetryMax, rt.maxTries)
	assert.Equal(t, http.DefaultTransport, rt.base)
}

func TestRetryTransport_UserAgent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := &retryTransport{base: http.DefaultTransport, maxTries: 1}
	c := &http.Client{Transport: rt}
	resp, err := c.Get(srv.URL)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, HTTPUserAgent, got)
}

func TestRetryTransport_SuccessNoRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := &retryTransport{base: http.DefaultTransport, maxTries: 3}
	c := &http.Client{Transport: rt}
	resp, err := c.Get(srv.URL)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "no retries expected on 200")
}

func TestRetryTransport_RetryOn5xx(t *testing.T) {
	const wantAttempts = 3
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	// Use zero base delay to keep the test fast.
	rt := &retryTransport{
		base:     http.DefaultTransport,
		maxTries: wantAttempts,
	}
	// Override the delay function is not directly injectable, so patch httpRetryBaseDelay
	// indirectly via a very small sleep — the test just checks attempt count.
	c := &http.Client{Transport: rt, Timeout: 5 * time.Second}
	resp, err := c.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Equal(t, int32(wantAttempts), atomic.LoadInt32(&calls), "should attempt exactly maxTries times")
}

func TestRetryTransport_RetryOn429WithRetryAfter(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.Header().Set("Retry-After", "0") // 0-second delay so the test is fast
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := &retryTransport{base: http.DefaultTransport, maxTries: 3}
	c := &http.Client{Transport: rt, Timeout: 5 * time.Second}
	resp, err := c.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestRetryTransport_NoRetryOn4xx(t *testing.T) {
	for _, code := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
	} {
		code := code
		t.Run(fmt.Sprintf("%d", code), func(t *testing.T) {
			var calls int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(code)
			}))
			defer srv.Close()

			rt := &retryTransport{base: http.DefaultTransport, maxTries: 3}
			c := &http.Client{Transport: rt}
			resp, err := c.Get(srv.URL)
			require.NoError(t, err)
			_ = resp.Body.Close()

			assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "non-retryable %d should not be retried", code)
		})
	}
}

func TestRetryTransport_NetworkErrorRetry(t *testing.T) {
	// Start a server that immediately closes the connection to force a network error.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		hj, ok := w.(http.Hijacker)
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	rt := &retryTransport{base: http.DefaultTransport, maxTries: 3}
	c := &http.Client{Transport: rt, Timeout: 5 * time.Second}
	_, err := c.Get(srv.URL)
	require.Error(t, err, "closed connection should produce an error")
	assert.Equal(t, int32(3), atomic.LoadInt32(&calls), "should retry all attempts on network error")
}

func TestHttpBackoffDelay(t *testing.T) {
	for attempt := 1; attempt <= 5; attempt++ {
		d := httpBackoffDelay(attempt)
		assert.Greater(t, d, time.Duration(0), "delay must be positive for attempt %d", attempt)
		assert.LessOrEqual(t, d, httpRetryMaxDelay+httpRetryMaxDelay/4,
			"delay must not greatly exceed httpRetryMaxDelay for attempt %d", attempt)
	}
}
