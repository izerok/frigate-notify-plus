package util

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/izerok/frigate-notify-plus/util/proxyx"
)

var AppUserAgent string
var HTTPTimeout int
var HTTPMaxAttempts int

// buildParams creates an escaped param string from a slice
func BuildHTTPParams(params ...map[string]string) string {
	paramList := url.Values{}
	if len(params) > 0 {
		for _, h := range params {
			for k, v := range h {
				paramList.Add(k, v)
			}
		}
	}
	return "?" + paramList.Encode()
}

func setUserAgent(req *http.Request) *http.Request {
	req.Header.Add("User-Agent", AppUserAgent)
	return req
}

// HTTPGet is a simple HTTP client function to return page body
func HTTPGet(url string, insecure bool, params string, headers ...map[string]string) ([]byte, error) {
	// New HTTP Client
	client := &http.Client{
		Timeout: time.Duration(HTTPTimeout) * time.Second,
		Jar:     cookies,
	}

	// Ignore SSL verification if set
	if insecure {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}

	return httpGet(url, client, insecure, params, headers...)
}

// HTTPGetProxied is HTTPGet with the transport resolved from a per-profile
// proxy setting (see the proxyx package): the request goes through the
// configured outbound proxy unless the setting forces a direct connection
// or the target is on the bypass list. It exists for notification-side
// GETs (webhook GET deliveries, the monitor check-in); Frigate-bound
// callers keep using HTTPGet, which never proxies.
func HTTPGetProxied(url string, insecure bool, proxySetting string, params string, headers ...map[string]string) ([]byte, error) {
	client := proxyx.Client(proxySetting, time.Duration(HTTPTimeout)*time.Second, insecure)
	client.Jar = cookies
	return httpGet(url, client, insecure, params, headers...)
}

func httpGet(url string, client *http.Client, insecure bool, params string, headers ...map[string]string) ([]byte, error) {
	// Append HTTP params if any
	if len(params) > 0 {
		url = url + params
	}

	// Set auth cookies if Frigate request & auth is enabled
	if strings.HasPrefix(url, FrigateServer) && AuthEnabled {
		// `/api/profile` is used to check token validity, so skip auth check
		if !strings.HasSuffix(url, "/api/profile") {
			if err := checkFrigateAuth(); err != nil {
				return nil, err
			}
		}
	}

	// Setup new HTTP Request
	req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}

	// Add headers
	if len(headers) > 0 {
		for _, h := range headers {
			for k, v := range h {
				req.Header.Add(k, v)
			}
		}
	}
	// Set User Agent
	setUserAgent(req)

	// Remove authorization header value for logging
	var logheaders []map[string]string
	for _, h := range headers {
		for k := range h {
			if strings.ToLower(k) == "authorization" {
				modifiedHeader := make(map[string]string)
				modifiedHeader[k] = "--secret removed--"
				logheaders = append(logheaders, modifiedHeader)
			} else {
				logheaders = append(logheaders, h)
			}
		}
	}

	// Send HTTP GET
	log.Trace().
		Str("url", url).
		Interface("headers", logheaders).
		Bool("insecure", insecure).
		Msg("HTTP GET")
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// Read response body
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	// Skip logging contents of snapshot image
	if strings.Contains(url, "snapshot.jpg") {
		log.Trace().
			Int64("content_length", response.ContentLength).
			Int("status_code", response.StatusCode).
			Msg("HTTP Response")
	} else {
		if json.Valid(body) {
			log.Trace().
				RawJSON("body", body).
				Int("status_code", response.StatusCode).
				Msg("HTTP Response")
		} else {
			log.Trace().
				Str("body", string(body)).
				Int("status_code", response.StatusCode).
				Msg("HTTP Response")
		}
	}

	if response.StatusCode != 200 {
		return nil, errors.New(strconv.Itoa(response.StatusCode))
	}

	return body, nil
}

// HTTPPostFrigate performs a single HTTP POST with the same treatment
// HTTPGet applies to GET requests: the shared cookie jar, a Frigate login
// session via checkFrigateAuth when auth is enabled, InsecureSkipVerify
// when insecure is set, the given headers and the shared User-Agent. It
// differs from HTTPPost in two deliberate ways: the timeout is supplied by
// the caller instead of the global HTTP timeout, and there is no retry
// loop — it is meant for long-running, non-idempotent Frigate calls (e.g.
// GenAI chat completions) where silently re-sending could duplicate
// minutes of server-side work. It also does not turn a non-2xx status
// into an error; it returns the response body and status code so the
// caller can surface the server's own error message. err is only non-nil
// when the request itself could not be sent or read.
func HTTPPostFrigate(url string, insecure bool, payload []byte, contentType string, timeout time.Duration, headers ...map[string]string) ([]byte, int, error) {
	// New HTTP Client
	client := &http.Client{
		Timeout: timeout,
		Jar:     cookies,
	}

	// Ignore SSL verification if set
	if insecure {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}

	// Set auth cookies if Frigate request & auth is enabled
	if strings.HasPrefix(url, FrigateServer) && AuthEnabled {
		// `/api/profile` is used to check token validity, so skip auth check
		if !strings.HasSuffix(url, "/api/profile") {
			if err := checkFrigateAuth(); err != nil {
				return nil, 0, err
			}
		}
	}

	// Setup new HTTP Request
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, 0, err
	}

	// Add headers
	if len(headers) > 0 {
		for _, h := range headers {
			for k, v := range h {
				req.Header.Add(k, v)
			}
		}
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	// Set User Agent
	setUserAgent(req)

	// Remove authorization header value for logging
	var logheaders []map[string]string
	for _, h := range headers {
		for k := range h {
			if strings.ToLower(k) == "authorization" {
				modifiedHeader := make(map[string]string)
				modifiedHeader[k] = "--secret removed--"
				logheaders = append(logheaders, modifiedHeader)
			} else {
				logheaders = append(logheaders, h)
			}
		}
	}

	// Send HTTP POST
	log.Trace().
		Str("url", url).
		Interface("headers", logheaders).
		Bool("insecure", insecure).
		Msg("HTTP POST (Frigate)")
	response, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, response.StatusCode, err
	}

	if json.Valid(respBody) {
		log.Trace().
			RawJSON("body", respBody).
			Int("status_code", response.StatusCode).
			Msg("HTTP Response")
	} else {
		log.Trace().
			Str("body", string(respBody)).
			Int("status_code", response.StatusCode).
			Msg("HTTP Response")
	}

	return respBody, response.StatusCode, nil
}

// HTTPPost performs an HTTP POST to the target URL
// and includes auth parameters, ignoring certificates, etc
func HTTPPost(url string, insecure bool, payload []byte, params string, headers ...map[string]string) ([]byte, error) {
	// New HTTP Client
	client := http.Client{Timeout: time.Duration(HTTPTimeout) * time.Second}

	// Ignore SSL verification if set
	if insecure {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}

	return httpPost(url, &client, "", insecure, payload, params, headers...)
}

// HTTPPostProxied is HTTPPost with the transport resolved from a
// per-profile proxy setting (see the proxyx package): the request goes
// through the configured outbound proxy unless the setting forces a
// direct connection or the target is on the bypass list. Only
// notification senders use it — Frigate-bound POSTs go through
// HTTPPostFrigate and never touch the proxy. When the proxy itself is
// unreachable the returned error names it (with any password masked);
// there is deliberately no silent fallback to a direct connection.
func HTTPPostProxied(url string, insecure bool, proxySetting string, payload []byte, params string, headers ...map[string]string) ([]byte, error) {
	client := proxyx.Client(proxySetting, time.Duration(HTTPTimeout)*time.Second, insecure)
	return httpPost(url, client, proxySetting, insecure, payload, params, headers...)
}

// HTTPPostProxiedOnce is HTTPPostProxied with exactly one attempt. Senders
// whose API may have accepted a message even though the response was lost
// (Pushover, whose previous client sent once) use it: retrying a
// non-idempotent delivery can produce duplicate notifications.
func HTTPPostProxiedOnce(url string, insecure bool, proxySetting string, payload []byte, params string, headers ...map[string]string) ([]byte, error) {
	client := proxyx.Client(proxySetting, time.Duration(HTTPTimeout)*time.Second, insecure)
	return httpPostAttempts(url, client, proxySetting, insecure, payload, params, 1, headers...)
}

func httpPost(url string, client *http.Client, proxySetting string, insecure bool, payload []byte, params string, headers ...map[string]string) ([]byte, error) {
	return httpPostAttempts(url, client, proxySetting, insecure, payload, params, HTTPMaxAttempts, headers...)
}

func httpPostAttempts(url string, client *http.Client, proxySetting string, insecure bool, payload []byte, params string, maxAttempts int, headers ...map[string]string) ([]byte, error) {
	// Append HTTP params if any
	if len(params) > 0 {
		url = url + params
	}

	// The historical loop had a hard cap of six attempts; keep it so a
	// misconfigured attempt count cannot spin forever (and never leaves
	// the loop with a nil response).
	attempts := maxAttempts
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 6 {
		attempts = 6
	}

	var response *http.Response
	for attempt := 1; ; attempt++ {
		// Setup new HTTP Request
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(payload))
		if err != nil {
			return nil, err
		}

		// Add headers
		if len(headers) > 0 {
			for _, h := range headers {
				for k, v := range h {
					req.Header.Add(k, v)
				}
			}
		}
		// Set User Agent
		setUserAgent(req)

		// Remove authorization header value for logging
		var logheaders []map[string]string
		for _, h := range headers {
			for k := range h {
				if strings.ToLower(k) == "authorization" {
					modifiedHeader := make(map[string]string)
					modifiedHeader[k] = "--secret removed--"
					logheaders = append(logheaders, modifiedHeader)
				} else {
					logheaders = append(logheaders, h)
				}
			}
		}

		// Send HTTP POST
		if json.Valid(payload) {
			log.Trace().
				Str("url", url).
				Interface("headers", logheaders).
				RawJSON("body", payload).
				Bool("insecure", insecure).
				Msg("HTTP POST")
		} else {
			log.Trace().
				Str("url", url).
				Interface("headers", logheaders).
				Interface("body", payload).
				Bool("insecure", insecure).
				Msg("HTTP POST")
		}

		response, err = client.Do(req)
		if err == nil {
			break
		}
		if attempt >= attempts {
			log.Warn().
				Int("attempt", attempt).
				Int("max_tries", attempts).
				Err(err).
				Msg("HTTP Request failed, retries exceeded")
			if proxyx.WouldUseProxy(proxySetting, url) {
				return nil, fmt.Errorf("request via proxy %s failed: %w", proxyx.Describe(proxySetting), err)
			}
			return nil, err
		}
		log.Warn().
			Int("attempt", attempt).
			Int("max_tries", attempts).
			Err(err).
			Msg("HTTP Request failed, retrying in 2 seconds...")
		time.Sleep(2 * time.Second)
	}

	defer response.Body.Close()

	// Read response body
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	log.Trace().
		RawJSON("body", body).
		Int("status_code", response.StatusCode).
		Msg("HTTP Response")

	// Check status codes
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return body, fmt.Errorf("failed to send request, got status code %v", response.StatusCode)
	}

	return body, nil
}
