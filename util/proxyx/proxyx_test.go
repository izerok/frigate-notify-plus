package proxyx

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestValidateURL(t *testing.T) {
	valid := []string{
		"http://127.0.0.1:7890",
		"https://proxy.example.com:8443",
		"socks5://10.0.0.1:1080",
		"socks5h://proxy.lan",
		"http://user:pass@192.168.1.10:7890",
		"http://proxy.example.com",  // no port is fine
		"http://proxy.example.com/", // bare slash is fine
		"HTTP://proxy.example.com:8080",
	}
	for _, raw := range valid {
		if err := ValidateURL(raw); err != nil {
			t.Errorf("ValidateURL(%q) = %v, want nil", raw, err)
		}
	}

	invalid := []string{
		"",                               // empty
		"proxy.example.com:7890",         // no scheme
		"ftp://proxy.example.com:21",     // scheme not supported
		"http://",                        // no host
		"http://:7890",                   // no host, only a port
		"http://proxy.example.com:0",     // port too small
		"http://proxy.example.com:65536", // port too large
		"http://proxy.example.com:abc",   // non-numeric port
		"http://proxy.example.com/path",  // path not allowed
		"http://proxy.example.com/?x=1",  // query not allowed
		"http://proxy.example.com/#frag",
	}
	for _, raw := range invalid {
		if err := ValidateURL(raw); err == nil {
			t.Errorf("ValidateURL(%q) = nil, want error", raw)
		}
	}
}

func TestValidateSetting(t *testing.T) {
	for _, setting := range []string{"", "direct", "DIRECT", "Direct", " http://127.0.0.1:7890 "} {
		if err := ValidateSetting(setting); err != nil {
			t.Errorf("ValidateSetting(%q) = %v, want nil", setting, err)
		}
	}
	for _, setting := range []string{"drect", "ftp://x:21", "http://x/path"} {
		if err := ValidateSetting(setting); err == nil {
			t.Errorf("ValidateSetting(%q) = nil, want error", setting)
		}
	}
}

func TestEffective(t *testing.T) {
	Configure("http://global.example.com:7890", nil, "http://frigate.lan:5000")
	t.Cleanup(func() { Configure("", nil, "") })

	tests := []struct {
		setting    string
		wantURL    string
		wantDirect bool
	}{
		{"", "http://global.example.com:7890", false}, // follow global
		{"direct", "", true},                          // force direct
		{"DIRECT", "", true},                          // case-insensitive
		{"http://own.example.com:1080", "http://own.example.com:1080", false}, // own URL wins
		{"not a url", "", false}, // malformed falls back to unset
	}
	for _, tc := range tests {
		gotURL, gotDirect := Effective(tc.setting)
		if gotURL != tc.wantURL || gotDirect != tc.wantDirect {
			t.Errorf("Effective(%q) = (%q, %v), want (%q, %v)", tc.setting, gotURL, gotDirect, tc.wantURL, tc.wantDirect)
		}
	}

	// Profile URL also wins when no global is configured.
	Configure("", nil, "")
	if gotURL, _ := Effective("socks5://own.example.com:1080"); gotURL != "socks5://own.example.com:1080" {
		t.Errorf("Effective with empty global = %q, want profile URL", gotURL)
	}
	if gotURL, gotDirect := Effective(""); gotURL != "" || gotDirect {
		t.Errorf("Effective(\"\") with no global = (%q, %v), want (\"\", false)", gotURL, gotDirect)
	}
	if Configured("") {
		t.Error("Configured(\"\") = true with nothing configured")
	}
	if !Configured("socks5://own.example.com:1080") {
		t.Error("Configured(profile URL) = false")
	}
}

func proxyFor(t *testing.T, transport *http.Transport, rawurl string) *url.URL {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawurl, nil)
	if err != nil {
		t.Fatal(err)
	}
	u, err := transport.Proxy(req)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestTransportProxyDecisions(t *testing.T) {
	Configure("http://proxy.example.com:7890",
		[]string{"gotify.lan", ".internal.example.com", "10.0.0.0/8"},
		"http://frigate.lan:5000")
	t.Cleanup(func() { Configure("", nil, "") })

	transport := Transport("", false)

	// Ordinary internet hosts go through the proxy.
	for _, target := range []string{"http://api.telegram.org", "https://api.telegram.org"} {
		u := proxyFor(t, transport, target)
		if u == nil || u.Host != "proxy.example.com:7890" {
			t.Errorf("proxy for %s = %v, want proxy.example.com:7890", target, u)
		}
	}

	// Bypass list: exact host, domain suffix, CIDR, localhost, loopback,
	// and the Frigate server (auto-exempt) all connect directly.
	for _, target := range []string{
		"http://gotify.lan:8080",
		"http://nas.internal.example.com",
		"http://10.1.2.3:9000",
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"http://frigate.lan:5000",
		"https://frigate.lan:5000",
	} {
		if u := proxyFor(t, transport, target); u != nil {
			t.Errorf("proxy for %s = %v, want nil (direct)", target, u)
		}
	}

	// A different port on the Frigate host is NOT exempt.
	if u := proxyFor(t, transport, "http://frigate.lan:9999"); u == nil {
		t.Error("proxy for frigate.lan:9999 = nil, want the proxy (only the Frigate port is exempt)")
	}

	// "direct" disables the proxy function entirely.
	directTransport := Transport("direct", false)
	if directTransport.Proxy != nil {
		t.Error("direct transport still has a Proxy function")
	}

	// A profile URL replaces the global proxy.
	ownTransport := Transport("socks5://own.example.com:1080", false)
	if u := proxyFor(t, ownTransport, "http://api.telegram.org"); u == nil || u.Host != "own.example.com:1080" {
		t.Errorf("proxy for profile URL = %v, want own.example.com:1080", u)
	}
}

func TestDescribeRedactsPassword(t *testing.T) {
	Configure("", nil, "")
	if got := Describe("http://user:secretpw@192.168.1.10:7890"); got != "http://user:xxxxx@192.168.1.10:7890" {
		t.Errorf("Describe = %q, want the password masked", got)
	}
	if strings.Contains(Describe("http://user:secretpw@192.168.1.10:7890"), "secretpw") {
		t.Error("Describe leaked the proxy password")
	}
	if got := Describe("direct"); got != "direct connection" {
		t.Errorf("Describe(direct) = %q", got)
	}
	if got := Describe(""); got != "no proxy" {
		t.Errorf("Describe(\"\") = %q", got)
	}
}

// fakeConnectProxy is a minimal HTTP CONNECT proxy: it records the
// request line and the Proxy-Authorization header, answers 200, then
// echoes whatever arrives through the tunnel.
type fakeConnectProxy struct {
	listener    net.Listener
	connectLine string
	authHeader  string
}

func startFakeConnectProxy(t *testing.T) *fakeConnectProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeConnectProxy{listener: listener}
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			trimmed := strings.TrimRight(line, "\r\n")
			if p.connectLine == "" {
				p.connectLine = trimmed
			}
			if strings.HasPrefix(strings.ToLower(trimmed), "proxy-authorization:") {
				p.authHeader = strings.TrimSpace(strings.SplitN(trimmed, ":", 2)[1])
			}
			if trimmed == "" {
				break
			}
		}
		fmt.Fprint(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		io.Copy(conn, reader) // echo the tunnel back to the sender
	}()
	t.Cleanup(func() { listener.Close() })
	return p
}

func TestDialContextConnectTunnel(t *testing.T) {
	proxy := startFakeConnectProxy(t)
	proxyAddr := proxy.listener.Addr().String()

	Configure("", nil, "")
	setting := "http://user:pass@" + proxyAddr
	dial := DialContext(setting)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := dial(ctx, "tcp", "smtp.example.com:25")
	if err != nil {
		t.Fatalf("DialContext through CONNECT proxy: %v", err)
	}
	defer conn.Close()

	if proxy.connectLine != "CONNECT smtp.example.com:25 HTTP/1.1" {
		t.Errorf("proxy saw connect line %q", proxy.connectLine)
	}
	if proxy.authHeader == "" {
		t.Error("proxy saw no Proxy-Authorization header")
	}

	if _, err := io.WriteString(conn, "hello tunnel"); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("hello tunnel"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "hello tunnel" {
		t.Errorf("echo through tunnel = %q", buf)
	}
}

// fakeSocks5Server is a minimal no-auth SOCKS5 server that connects to
// the requested address and relays bytes both ways.
func startFakeSocks5Server(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleSocks5(conn)
		}
	}()
	t.Cleanup(func() { listener.Close() })
	return listener
}

func handleSocks5(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 512)
	// Greeting: version, nmethods, methods.
	if _, err := io.ReadFull(conn, buf[:2]); err != nil || buf[0] != 5 {
		return
	}
	if _, err := io.ReadFull(conn, buf[:int(buf[1])]); err != nil {
		return
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return
	}
	// Request: version, command, reserved, address type.
	if _, err := io.ReadFull(conn, buf[:4]); err != nil || buf[1] != 1 {
		return
	}
	var host string
	switch buf[3] {
	case 1:
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return
		}
		host = net.IP(buf[:4]).String()
	case 3:
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return
		}
		n := int(buf[0])
		if _, err := io.ReadFull(conn, buf[:n]); err != nil {
			return
		}
		host = string(buf[:n])
	default:
		return
	}
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	port := uint16(buf[0])<<8 | uint16(buf[1])
	target, err := net.Dial("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer target.Close()
	if _, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	go io.Copy(target, conn)
	io.Copy(conn, target)
}

func startEchoServer(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()
	t.Cleanup(func() { listener.Close() })
	return listener
}

func TestDialContextSocks5(t *testing.T) {
	socks := startFakeSocks5Server(t)
	echo := startEchoServer(t)

	Configure("", nil, "")
	dial := DialContext("socks5://" + socks.Addr().String())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := dial(ctx, "tcp", echo.Addr().String())
	if err != nil {
		t.Fatalf("DialContext through SOCKS5 proxy: %v", err)
	}
	defer conn.Close()

	if _, err := io.WriteString(conn, "hello socks"); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("hello socks"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "hello socks" {
		t.Errorf("echo through SOCKS5 = %q", buf)
	}
}

func TestDialContextDirect(t *testing.T) {
	echo := startEchoServer(t)
	Configure("http://proxy.example.com:7890", nil, "")
	dial := DialContext("direct")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := dial(ctx, "tcp", echo.Addr().String())
	if err != nil {
		t.Fatalf("DialContext direct: %v", err)
	}
	conn.Close()
}

// TestDialContextHonoursNoProxy proves the SMTP dialer skips the proxy for
// targets on the bypass list. The host does not resolve, so a direct dial
// fails; any CONNECT at the fake proxy means the bypass was ignored.
func TestDialContextHonoursNoProxy(t *testing.T) {
	proxy := startFakeConnectProxy(t)
	Configure("http://"+proxy.listener.Addr().String(), []string{"smtp.lan"}, "http://frigate.lan:5000")
	t.Cleanup(func() { Configure("", nil, "") })

	dial := DialContext("")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := dial(ctx, "tcp", "smtp.lan:25"); err == nil {
		t.Fatal("dial to the bypassed host unexpectedly succeeded")
	}
	time.Sleep(100 * time.Millisecond)
	if proxy.connectLine != "" {
		t.Fatalf("bypassed host was tunneled through the proxy: %q", proxy.connectLine)
	}
}

// A target that is not on the bypass list still goes through the proxy.
func TestDialContextProxiesNonBypassedTargets(t *testing.T) {
	proxy := startFakeConnectProxy(t)
	Configure("http://"+proxy.listener.Addr().String(), []string{"smtp.lan"}, "")
	t.Cleanup(func() { Configure("", nil, "") })

	dial := DialContext("")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := dial(ctx, "tcp", "smtp.example.com:25")
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	conn.Close()
	if proxy.connectLine != "CONNECT smtp.example.com:25 HTTP/1.1" {
		t.Errorf("proxy saw %q, want the CONNECT tunnel", proxy.connectLine)
	}
}

func TestProxyDialAddressDefaultPorts(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"http://proxy.lan", "proxy.lan:80"},
		{"https://proxy.lan", "proxy.lan:443"},
		{"socks5://proxy.lan", "proxy.lan:1080"},
		{"socks5h://proxy.lan", "proxy.lan:1080"},
		{"http://proxy.lan:7890", "proxy.lan:7890"},
		{"socks5://[::1]", "[::1]:1080"},
	}
	for _, tc := range cases {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.raw, err)
		}
		got, err := proxyDialAddress(u)
		if err != nil {
			t.Errorf("proxyDialAddress(%q) error: %v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("proxyDialAddress(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// A parse failure must not echo the address: it carries credentials and
// ends up in the API's 422 body and the logs.
func TestValidateURLErrorDoesNotLeakCredentials(t *testing.T) {
	err := ValidateURL("http://alice:s3cr3t@proxy.lan:http")
	if err == nil {
		t.Fatal("ValidateURL accepted an invalid port")
	}
	for _, secret := range []string{"s3cr3t", "alice"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("validation error leaked %q: %s", secret, err)
		}
	}
}
