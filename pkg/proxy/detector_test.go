package proxy

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestCleanHostPort(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"socks5://127.0.0.1:10808", "127.0.0.1:10808"},
		{"http://127.0.0.1:7890/", "127.0.0.1:7890"},
		{"socks5h://127.0.0.1:2080", "127.0.0.1:2080"},
		{"127.0.0.1:10808", "127.0.0.1:10808"},
	}

	for _, c := range cases {
		res := CleanHostPort(c.input)
		if res != c.expected {
			t.Errorf("CleanHostPort(%s) = %s; want %s", c.input, res, c.expected)
		}
	}
}

func TestBuildProxyEnv(t *testing.T) {
	// 1. Pure SOCKS proxy (non-HTTP port): ALL_PROXY should be set to socks5h, HTTPS_PROXY must not be set to socks
	pureSocksEnv := BuildProxyEnv("socks5://127.0.0.1:59999")
	var foundAllProxy bool
	for _, e := range pureSocksEnv {
		if strings.HasPrefix(e, "ALL_PROXY=") {
			foundAllProxy = true
			if !strings.Contains(e, "127.0.0.1:59999") {
				t.Errorf("ALL_PROXY does not contain target proxy: %s", e)
			}
		}
		if strings.HasPrefix(strings.ToUpper(e), "HTTPS_PROXY=") || strings.HasPrefix(strings.ToUpper(e), "HTTP_PROXY=") {
			t.Errorf("HTTPS_PROXY / HTTP_PROXY should not be set for pure unverified socks proxies, got: %s", e)
		}
	}
	if !foundAllProxy {
		t.Errorf("BuildProxyEnv did not produce ALL_PROXY for pure socks")
	}

	// 2. HTTP proxy: ALL_PROXY, HTTPS_PROXY, and HTTP_PROXY should be set
	httpEnv := BuildProxyEnv("http://127.0.0.1:7890")
	var foundHttpsProxy bool
	for _, e := range httpEnv {
		if strings.HasPrefix(e, "HTTPS_PROXY=") {
			foundHttpsProxy = true
			if !strings.Contains(e, "127.0.0.1:7890") {
				t.Errorf("HTTPS_PROXY does not contain target proxy: %s", e)
			}
		}
	}
	if !foundHttpsProxy {
		t.Errorf("BuildProxyEnv did not produce HTTPS_PROXY for http")
	}
}

func TestProbeSocks5HandshakeMock(t *testing.T) {
	// Start mock SOCKS5 listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on random port: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 3)
		_, _ = conn.Read(buf)
		// Send RFC 1928 response: VER=5, METHOD=0x00
		_, _ = conn.Write([]byte{0x05, 0x00})
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	ok, auth, desc := ProbeSocks5Handshake("127.0.0.1", port, 500*time.Millisecond)
	if !ok || auth {
		t.Fatalf("expected socks5 ok=true, auth=false; got ok=%v, auth=%v, desc=%s", ok, auth, desc)
	}
}

func TestProbeHttpConnectMock(t *testing.T) {
	// Start mock HTTP CONNECT listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on random port: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 256)
		_, _ = conn.Read(buf)
		// Respond with 200 Connection established
		resp := "HTTP/1.1 200 Connection established\r\n\r\n"
		_, _ = conn.Write([]byte(resp))
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	ok, auth, desc := ProbeHttpConnect("127.0.0.1", port, "cloudcode-pa.googleapis.com:443", 500*time.Millisecond)
	if !ok || auth {
		t.Fatalf("expected http connect ok=true, auth=false; got ok=%v, auth=%v, desc=%s", ok, auth, desc)
	}
}

func BenchmarkCleanHostPort(b *testing.B) {
	input := "socks5h://127.0.0.1:10808"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res := CleanHostPort(input)
		if res == "" {
			b.Fatal("empty host port")
		}
	}
}

func BenchmarkBuildProxyEnv(b *testing.B) {
	input := "socks5h://127.0.0.1:10808"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		env := BuildProxyEnv(input)
		if len(env) == 0 {
			b.Fatal("empty proxy env")
		}
	}
}
