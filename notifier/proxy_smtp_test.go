package notifier

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wneessen/go-mail"

	"github.com/izerok/frigate-notify-plus/util/proxyx"
)

// fakeSMTPServer speaks just enough SMTP for a go-mail DialAndSend:
// greeting, EHLO, MAIL FROM, RCPT TO, DATA and QUIT, recording the
// message data it receives.
type fakeSMTPServer struct {
	listener net.Listener

	mu   sync.Mutex
	data string
}

func startFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTPServer{listener: listener}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go s.handle(conn)
		}
	}()
	t.Cleanup(func() { listener.Close() })
	return s
}

func (s *fakeSMTPServer) handle(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	fmt.Fprint(conn, "220 fake.smtp ESMTP\r\n")
	inData := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		if inData {
			if strings.TrimRight(line, "\r\n") == "." {
				inData = false
				fmt.Fprint(conn, "250 OK queued\r\n")
				continue
			}
			s.mu.Lock()
			s.data += line
			s.mu.Unlock()
			continue
		}
		switch {
		case strings.HasPrefix(line, "EHLO") || strings.HasPrefix(line, "HELO"):
			fmt.Fprint(conn, "250-fake.smtp greets\r\n250 OK\r\n")
		case strings.HasPrefix(line, "MAIL FROM"):
			fmt.Fprint(conn, "250 OK\r\n")
		case strings.HasPrefix(line, "RCPT TO"):
			fmt.Fprint(conn, "250 OK\r\n")
		case strings.HasPrefix(line, "DATA"):
			inData = true
			fmt.Fprint(conn, "354 End data with <CR><LF>.<CR><LF>\r\n")
		case strings.HasPrefix(line, "QUIT"):
			fmt.Fprint(conn, "221 Bye\r\n")
			return
		default:
			fmt.Fprint(conn, "250 OK\r\n")
		}
	}
}

func (s *fakeSMTPServer) received() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data
}

// connectProxyForSMTP accepts one CONNECT, records the requested
// target, answers 200 and then pipes the tunnel to the fake SMTP
// server regardless of the requested host name.
type connectProxyForSMTP struct {
	listener net.Listener
	target   string

	mu      sync.Mutex
	connect string
}

func startConnectProxyForSMTP(t *testing.T, smtpAddr string) *connectProxyForSMTP {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &connectProxyForSMTP{listener: listener, target: smtpAddr}
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
			if strings.HasPrefix(trimmed, "CONNECT ") {
				p.mu.Lock()
				p.connect = strings.Fields(trimmed)[1]
				p.mu.Unlock()
			}
			if trimmed == "" {
				break
			}
		}
		upstream, err := net.Dial("tcp", p.target)
		if err != nil {
			return
		}
		defer upstream.Close()
		fmt.Fprint(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go io.Copy(upstream, reader)
		io.Copy(conn, upstream)
	}()
	t.Cleanup(func() { listener.Close() })
	return p
}

// TestSMTPThroughConnectProxy sends a real mail through a go-mail
// client configured exactly like the SMTP sender configures it when a
// proxy applies (WithDialContextFunc over proxyx.DialContext), proving
// the SMTP conversation travels through the CONNECT tunnel.
func TestSMTPThroughConnectProxy(t *testing.T) {
	smtpServer := startFakeSMTPServer(t)
	proxy := startConnectProxyForSMTP(t, smtpServer.listener.Addr().String())

	proxyx.Configure("", nil, "")
	setting := "http://" + proxy.listener.Addr().String()

	client, err := mail.NewClient("smtp.test",
		mail.WithPort(25),
		mail.WithTLSPolicy(mail.NoTLS),
		mail.WithTimeout(10*time.Second),
		mail.WithDialContextFunc(proxyx.DialContext(setting)),
	)
	if err != nil {
		t.Fatalf("mail.NewClient: %v", err)
	}

	msg := mail.NewMsg()
	if err := msg.From("alerts@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := msg.To("user@example.com"); err != nil {
		t.Fatal(err)
	}
	msg.Subject("Proxy SMTP Test")
	msg.SetBodyString(mail.TypeTextPlain, "sent through the tunnel")

	if err := client.DialAndSend(msg); err != nil {
		t.Fatalf("DialAndSend through CONNECT proxy: %v", err)
	}

	proxy.mu.Lock()
	connectTarget := proxy.connect
	proxy.mu.Unlock()
	if connectTarget != "smtp.test:25" {
		t.Errorf("proxy saw CONNECT %q, want smtp.test:25", connectTarget)
	}
	if received := smtpServer.received(); !strings.Contains(received, "sent through the tunnel") {
		t.Errorf("SMTP server received %q, want the message body", received)
	}
}
