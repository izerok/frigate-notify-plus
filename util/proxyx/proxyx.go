// Package proxyx resolves and applies the outbound proxy settings used for
// notification delivery.
//
// A proxy setting is a single string with three forms:
//
//   - ""        follow the deployment-wide default (alerts.general.proxy);
//     when no default is configured either, connections behave exactly
//     like they did before proxy support existed.
//   - "direct"  force a direct connection, ignoring any configured proxy.
//   - a URL     use this proxy: http://, https://, socks5:// or
//     socks5h://, optionally with user:password@ credentials.
//
// The package never proxies traffic to the configured Frigate server: its
// host is always part of the bypass list (see Configure).
package proxyx

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/net/http/httpproxy"
	"golang.org/x/net/proxy"
)

// dialTimeout bounds a single connection attempt made through DialContext.
const dialTimeout = 30 * time.Second

// maxConnectHeaderBytes caps how much of a proxy's CONNECT response is
// read before the tunnel is considered established.
const maxConnectHeaderBytes = 16 * 1024

// runtimeState is the deployment-wide part of the proxy configuration: the
// global proxy URL, the user-configured bypass list and the Frigate server
// host (always bypassed). It is replaced wholesale on every config load or
// save, so senders always resolve against the live configuration.
type runtimeState struct {
	globalURL   string
	noProxy     []string
	frigateHost string
}

var state atomic.Pointer[runtimeState]

func init() {
	state.Store(&runtimeState{})
}

// Configure records the deployment-wide proxy settings. The config package
// calls it during validation, which runs on startup and on every config
// save, so changes take effect without a restart. frigateServer is the
// configured Frigate base URL; its host is always added to the bypass
// list so Frigate traffic can never end up in the proxy.
func Configure(globalURL string, noProxy []string, frigateServer string) {
	next := &runtimeState{
		globalURL: strings.TrimSpace(globalURL),
		noProxy:   append([]string(nil), noProxy...),
	}
	if u, err := url.Parse(frigateServer); err == nil && u.Host != "" {
		next.frigateHost = u.Host
	}
	state.Store(next)
}

// ValidateURL checks one proxy address. The address must use one of the
// supported schemes, name a host, carry a port in range when a port is
// present, and contain no path, query or fragment — those are almost
// always a paste error, so they are rejected instead of silently ignored.
// Credentials embedded in the address are allowed.
func ValidateURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("proxy address is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("proxy address cannot be parsed: %s", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return errors.New("proxy address must start with http://, https://, socks5:// or socks5h://")
	}
	if u.Hostname() == "" {
		return errors.New("proxy address must include a host name or IP address")
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return errors.New("proxy port must be a number between 1 and 65535")
		}
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return errors.New("proxy address must not contain a path, query string or fragment")
	}
	return nil
}

// ValidateSetting checks a per-profile proxy setting: empty (follow the
// global default), "direct", or a valid proxy URL.
func ValidateSetting(setting string) error {
	setting = strings.TrimSpace(setting)
	if setting == "" || strings.EqualFold(setting, "direct") {
		return nil
	}
	return ValidateURL(setting)
}

// Effective resolves a per-profile setting against the global default and
// reports the proxy URL to use. direct is true when the connection must
// bypass any proxy. A malformed value resolves to "no proxy": settings
// are validated when the config is saved, so this only defends against a
// hand-edited config file, and dropping to the pre-proxy behaviour is
// safer than failing every notification.
func Effective(profileSetting string) (proxyURL string, direct bool) {
	setting := strings.TrimSpace(profileSetting)
	if strings.EqualFold(setting, "direct") {
		return "", true
	}
	if setting != "" {
		if err := ValidateURL(setting); err == nil {
			return setting, false
		}
		return "", false
	}
	global := state.Load().globalURL
	if global != "" && ValidateURL(global) == nil {
		return global, false
	}
	return "", false
}

// Configured reports whether any proxy applies to the setting: a proxy
// URL of its own, or the global default.
func Configured(profileSetting string) bool {
	proxyURL, _ := Effective(profileSetting)
	return proxyURL != ""
}

// WouldUseProxy reports whether a request to rawurl under the given
// profile setting would actually be sent through a proxy — as opposed
// to no proxy being configured, a forced direct connection, or the
// target sitting on the bypass list.
func WouldUseProxy(profileSetting, rawurl string) bool {
	proxyURL, direct := Effective(profileSetting)
	if direct || proxyURL == "" {
		return false
	}
	target, err := url.Parse(rawurl)
	if err != nil {
		return false
	}
	proxy, err := proxyFunc(proxyURL)(&http.Request{URL: target})
	return err == nil && proxy != nil
}

// bypassList is the effective no-proxy list: the configured entries plus
// the Frigate server host, which is always exempt.
func bypassList() []string {
	s := state.Load()
	list := append([]string(nil), s.noProxy...)
	if s.frigateHost != "" {
		list = append(list, s.frigateHost)
	}
	return list
}

// proxyFunc builds the Transport proxy function for one proxy URL,
// honouring the effective bypass list. Host matching (exact names, domain
// suffixes, CIDRs, localhost) is delegated to the httpproxy package.
func proxyFunc(proxyURL string) func(*http.Request) (*url.URL, error) {
	cfg := httpproxy.Config{
		HTTPProxy:  proxyURL,
		HTTPSProxy: proxyURL,
		NoProxy:    strings.Join(bypassList(), ","),
	}
	proxyForURL := cfg.ProxyFunc()
	return func(req *http.Request) (*url.URL, error) {
		return proxyForURL(req.URL)
	}
}

// Transport returns an http.Transport for one send. It starts from a clone
// of the default transport so timeouts and keep-alive behaviour match the
// rest of the application:
//
//   - no proxy configured: the Proxy field is left untouched, preserving
//     the historical behaviour (the process environment decides);
//   - "direct": Proxy is disabled outright;
//   - a proxy URL: requests go through it unless the target is on the
//     bypass list.
//
// insecure additionally skips TLS certificate verification, matching the
// per-profile "ignore SSL" flags the senders already pass around.
func Transport(profileSetting string, insecure bool) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	proxyURL, direct := Effective(profileSetting)
	switch {
	case direct:
		transport.Proxy = nil
	case proxyURL != "":
		transport.Proxy = proxyFunc(proxyURL)
	}
	if insecure {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		} else {
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		}
		transport.TLSClientConfig.InsecureSkipVerify = true
	}
	return transport
}

// Client returns an http.Client whose transport follows the proxy setting.
func Client(profileSetting string, timeout time.Duration, insecure bool) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: Transport(profileSetting, insecure),
	}
}

// Describe returns the proxy that applies to a setting, safe to show in
// logs and error messages: any password in the address is masked.
func Describe(profileSetting string) string {
	proxyURL, direct := Effective(profileSetting)
	if direct {
		return "direct connection"
	}
	if proxyURL == "" {
		return "no proxy"
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return "invalid proxy address"
	}
	return u.Redacted()
}

// DialContext returns a dial function suitable for connection-oriented
// protocols (SMTP) that establishes connections through the proxy the
// setting resolves to: a CONNECT tunnel for http/https proxies, a SOCKS5
// dialer for socks5/socks5h proxies, and a plain dialer when no proxy
// applies or the setting forces a direct connection.
func DialContext(profileSetting string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	proxyURL, direct := Effective(profileSetting)
	if direct || proxyURL == "" {
		dialer := &net.Dialer{Timeout: dialTimeout}
		return dialer.DialContext
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		dialer := &net.Dialer{Timeout: dialTimeout}
		return dialer.DialContext
	}
	switch strings.ToLower(u.Scheme) {
	case "socks5", "socks5h":
		var auth *proxy.Auth
		if u.User != nil {
			password, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: password}
		}
		forward := &net.Dialer{Timeout: dialTimeout}
		socksDialer, err := proxy.SOCKS5("tcp", u.Host, auth, forward)
		if err != nil {
			return func(context.Context, string, string) (net.Conn, error) { return nil, err }
		}
		if contextDialer, ok := socksDialer.(proxy.ContextDialer); ok {
			return contextDialer.DialContext
		}
		return func(_ context.Context, network, addr string) (net.Conn, error) {
			return socksDialer.Dial(network, addr)
		}
	default: // http, https
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialConnect(ctx, u, addr)
		}
	}
}

// dialConnect opens a TCP connection to addr through an HTTP proxy using
// the CONNECT method. For https proxies the connection to the proxy
// itself is wrapped in TLS first.
func dialConnect(ctx context.Context, proxyURL *url.URL, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", proxyURL.Host)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to proxy %s: %w", proxyURL.Redacted(), err)
	}

	if strings.EqualFold(proxyURL.Scheme, "https") {
		tlsConfig := &tls.Config{ServerName: proxyURL.Hostname()}
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, fmt.Errorf("TLS handshake with proxy %s failed: %w", proxyURL.Redacted(), err)
		}
		conn = tlsConn
	}

	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	} else {
		conn.SetDeadline(time.Now().Add(dialTimeout))
	}

	var request strings.Builder
	fmt.Fprintf(&request, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", addr, addr)
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		credentials := base64.StdEncoding.EncodeToString([]byte(proxyURL.User.Username() + ":" + password))
		fmt.Fprintf(&request, "Proxy-Authorization: Basic %s\r\n", credentials)
	}
	request.WriteString("\r\n")
	if _, err := io.WriteString(conn, request.String()); err != nil {
		conn.Close()
		return nil, fmt.Errorf("cannot send CONNECT to proxy %s: %w", proxyURL.Redacted(), err)
	}

	reader := bufio.NewReader(conn)
	statusLine, err := readConnectResponse(reader)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("proxy %s refused the tunnel: %w", proxyURL.Redacted(), err)
	}
	if !strings.Contains(statusLine, " 200") {
		conn.Close()
		return nil, fmt.Errorf("proxy %s refused the tunnel: %s", proxyURL.Redacted(), statusLine)
	}

	conn.SetDeadline(time.Time{})
	// Bytes the proxy already sent past the response headers (e.g. a
	// server banner that arrived in the same packet) belong to the
	// tunneled stream, so reads must drain the buffer first.
	if reader.Buffered() > 0 {
		return &bufferedConn{Conn: conn, reader: reader}, nil
	}
	return conn, nil
}

// readConnectResponse reads a CONNECT response header block, bounded to
// maxConnectHeaderBytes, and returns its status line.
func readConnectResponse(reader *bufio.Reader) (string, error) {
	var statusLine string
	total := 0
	for {
		line, err := reader.ReadString('\n')
		total += len(line)
		if total > maxConnectHeaderBytes {
			return "", errors.New("CONNECT response headers too large")
		}
		if err != nil {
			return "", err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if statusLine == "" {
			statusLine = trimmed
		}
		if trimmed == "" {
			return statusLine, nil
		}
	}
}

// bufferedConn reads from a bufio.Reader before falling through to the
// underlying connection, so no tunneled bytes are lost.
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	if c.reader.Buffered() > 0 {
		return c.reader.Read(p)
	}
	return c.Conn.Read(p)
}
