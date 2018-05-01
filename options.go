package clccam

/*
 * Options handling: taken from and thanks to github.com/zpatrick/rclient
 */

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/rehttp"
	"github.com/grrtrr/clccam/logger"
)

// A ClientOption configures the @client using the functional option pattern.
type ClientOption func(client *Client)

// HostURL sets the base @url of the client (thanks to go-resty/resty)
func HostURL(url string) ClientOption {
	return func(r *Client) {
		r.baseURL = strings.TrimRight(url, "/")
	}
}

// Retryer configures the retry mechanism of the client
// @maxRetries: maximum number of retries per request
// @stepDelay:  base value for exponential backoff + jitter delay
// @maxTimeout: maximum overall client timeout
func Retryer(maxRetries int, stepDelay, maxTimeout time.Duration) ClientOption {
	return func(r *Client) {
		r.client.Transport = rehttp.NewTransport(
			nil, // use http.DefaultTransport
			rehttp.RetryFn(func(at rehttp.Attempt) bool {
				if at.Index < maxRetries {
					if at.Response == nil {
						logger.Warnf("%s %s failed (%s) - retry #%d",
							at.Request.Method, at.Request.URL.Path, at.Error, at.Index+1)
						return true
					}
					switch at.Response.StatusCode {
					// Request timeout, server error, bad gateway, service unavailable, gateway timeout
					case 408, 500, 502, 503, 504:
						logger.Warnf("%s %s returned %q - retry #%d",
							at.Request.Method, at.Request.URL.Path, at.Response.Status, at.Index+1)
						return true
					}
				}
				return false
			}),
			// Reuse @maxTimeout as upper bound for the exponential backoff.
			rehttp.ExpJitterDelay(stepDelay, maxTimeout),
		)
		// Set the overall client timeout in lock-step with that of the retryer.
		r.client.Timeout = maxTimeout
	}
}

// Context adds @ctx to the client
func Context(ctx context.Context) ClientOption {
	return func(r *Client) {
		r.ctx = ctx
	}
}

// Debug enables per-request logging
func Debug(enabled bool) ClientOption {
	return func(r *Client) {
		r.requestDebug = enabled
	}
}

// RequestOptions sets the RequestOptions field of @r.
func RequestOptions(options ...RequestOption) ClientOption {
	return func(r *Client) {
		r.requestOptions = append(r.requestOptions, options...)
	}
}

// RequestOption modifies the HTTP @req in place
type RequestOption func(req *http.Request)

// Headers adds the specified names and values as headers to a request
func Headers(headers map[string]string) RequestOption {
	return func(req *http.Request) {
		for name, val := range headers {
			req.Header.Set(name, val)
		}
	}
}

// Query adds the specified query to a request.
func Query(query url.Values) RequestOption {
	return func(req *http.Request) {
		req.URL.RawQuery = query.Encode()
	}
}