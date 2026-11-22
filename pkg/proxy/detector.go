package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// ProxyEndpoint holds discovered proxy information
type ProxyEndpoint struct {
	Type     string // "socks5", "http", "mixed", "unknown"
	Address  string // "127.0.0.1:10808"
	URL      string // "http://127.0.0.1:10808" or "socks5h://127.0.0.1:10808"
	Provider string // detected provider description
	HasHttp  bool   // true if HTTP CONNECT tunnel handshake succeeded
	HasSocks bool   // true if SOCKS5 RFC 1928 greeting handshake succeeded
}

// NetHttpURL returns the proxy URL formatted for Go net/http.Transport (socks5h is converted to socks5)
func (p *ProxyEndpoint) NetHttpURL() string {
	return SafeNetHttpProxyURL(p.URL)
}

// SafeNetHttpProxyURL sanitizes proxy URLs for Go net/http (converts socks5h:// to socks5://)
func SafeNetHttpProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "socks5h://") {
		return "socks5://" + strings.TrimPrefix(raw, "socks5h://")
	}
	return raw
}

// CleanHostPort strips protocol prefixes and trailing slashes for concise display
func CleanHostPort(rawURL string) string {
	s := strings.TrimSpace(rawURL)
	s = strings.TrimPrefix(s, "socks5h://")
	s = strings.TrimPrefix(s, "socks5://")
	s = strings.TrimPrefix(s, "socks://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimSuffix(s, "/")
	return s
}

// IsPortOpen tests if a TCP port is responding on 127.0.0.1
func IsPortOpen(port int, timeout time.Duration) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ProbeHttpConnect performs an actual HTTP CONNECT handshake to verify HTTPS tunneling support
func ProbeHttpConnect(host string, port int, targetHostPort string, timeout time.Duration) (bool, bool, string) {
	if targetHostPort == "" {
		targetHostPort = "cloudcode-pa.googleapis.com:443"
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false, false, fmt.Sprintf("dial err: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: AntigravityCleaner/5.5\r\nProxy-Connection: Keep-Alive\r\n\r\n", targetHostPort, targetHostPort)
	if _, err := conn.Write([]byte(req)); err != nil {
		return false, false, fmt.Sprintf("write err: %v", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: "CONNECT"})
	if err != nil {
		return false, false, fmt.Sprintf("read err: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return true, false, "200 Connection established"
	}
	if resp.StatusCode == 407 {
		return false, true, "407 Proxy Authentication Required"
	}
	return false, false, fmt.Sprintf("HTTP status %d", resp.StatusCode)
}

// ProbeSocks5Handshake performs an RFC 1928 SOCKS5 greeting handshake to verify SOCKS5 protocol support
func ProbeSocks5Handshake(host string, port int, timeout time.Duration) (bool, bool, string) {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false, false, fmt.Sprintf("dial err: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	// RFC 1928 greeting: VER=5, NMETHODS=1, METHOD=0x00 (NO AUTH)
	greeting := []byte{0x05, 0x01, 0x00}
	if _, err := conn.Write(greeting); err != nil {
		return false, false, fmt.Sprintf("write err: %v", err)
	}

	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return false, false, fmt.Sprintf("read err: %v", err)
	}

	if resp[0] != 0x05 {
		return false, false, fmt.Sprintf("invalid SOCKS ver 0x%02x", resp[0])
	}

	if resp[1] == 0x00 {
		return true, false, "SOCKS5 OK (No Auth)"
	}
	if resp[1] == 0x02 {
		return true, true, "SOCKS5 Auth Required (User/Pass)"
	}
	if resp[1] == 0xFF {
		return false, true, "SOCKS5 No Acceptable Methods"
	}
	return false, false, fmt.Sprintf("SOCKS5 method 0x%02x", resp[1])
}

// ProbeProxyPort inspects a loopback port with both HTTP CONNECT and SOCKS5 RFC handshakes
func ProbeProxyPort(host string, port int, timeout time.Duration) (ProxyEndpoint, bool) {
	if host == "" {
		host = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	httpOk, httpAuth, _ := ProbeHttpConnect(host, port, "", timeout)
	socksOk, socksAuth, _ := ProbeSocks5Handshake(host, port, timeout)

	if httpOk && socksOk {
		return ProxyEndpoint{
			Type:     "mixed",
			Address:  addr,
			URL:      fmt.Sprintf("http://%s", addr), // HTTP URL enables Go net/http & Node.js CONNECT while SOCKS5 is also valid
			Provider: "Active Mixed Proxy (HTTP CONNECT + SOCKS5)",
			HasHttp:  true,
			HasSocks: true,
		}, true
	}

	if httpOk {
		return ProxyEndpoint{
			Type:     "http",
			Address:  addr,
			URL:      fmt.Sprintf("http://%s", addr),
			Provider: "Active HTTP Tunnel Proxy",
			HasHttp:  true,
		}, true
	}

	if socksOk {
		return ProxyEndpoint{
			Type:     "socks5",
			Address:  addr,
			URL:      fmt.Sprintf("socks5h://%s", addr),
			Provider: "Active SOCKS5 Proxy",
			HasSocks: true,
		}, true
	}

	if httpAuth || socksAuth {
		return ProxyEndpoint{
			Type:     "auth_required",
			Address:  addr,
			URL:      fmt.Sprintf("http://%s", addr),
			Provider: "Proxy Requires Authentication (407/UserPass)",
		}, false
	}

	return ProxyEndpoint{
		Type:    "unknown",
		Address: addr,
	}, false
}

// Candidate loopback ports commonly scanned for active clients
var probeCandidatePorts = []int{10808, 7890, 2080, 10809, 2081, 1080, 8888, 8080}

// DetectActiveProxies scans loopback ports and environment variables using active RFC handshakes
func DetectActiveProxies() []ProxyEndpoint {
	var found []ProxyEndpoint

	// 1. Actively probe loopback ports in parallel with RFC handshakes
	type probeResult struct {
		endpoint ProxyEndpoint
		valid    bool
	}

	ch := make(chan probeResult, len(probeCandidatePorts))
	for _, port := range probeCandidatePorts {
		go func(p int) {
			if !IsPortOpen(p, 150*time.Millisecond) {
				ch <- probeResult{valid: false}
				return
			}
			ep, valid := ProbeProxyPort("127.0.0.1", p, 600*time.Millisecond)
			ch <- probeResult{endpoint: ep, valid: valid}
		}(port)
	}

	for i := 0; i < len(probeCandidatePorts); i++ {
		res := <-ch
		if res.valid {
			found = append(found, res.endpoint)
		}
	}

	// 2. Fallback to check environment variables if no local port validated
	if len(found) == 0 {
		for _, envVar := range []string{"ALL_PROXY", "all_proxy", "HTTPS_PROXY", "https_proxy"} {
			val := os.Getenv(envVar)
			if val != "" {
				val = strings.TrimSpace(val)
				typ := "http"
				if strings.HasPrefix(val, "socks") {
					typ = "socks5"
				}
				cleanAddr := CleanHostPort(val)
				found = append(found, ProxyEndpoint{
					Type:     typ,
					Address:  cleanAddr,
					URL:      val,
					Provider: "System Proxy (" + envVar + ")",
					HasHttp:  typ == "http",
					HasSocks: typ == "socks5",
				})
				break
			}
		}
	}

	return found
}

// GetBestProxy returns the optimal verified proxy (preferring Mixed / HTTP for full Go & Electron compatibility)
func GetBestProxy() (*ProxyEndpoint, error) {
	active := DetectActiveProxies()
	if len(active) == 0 {
		return nil, fmt.Errorf("no verified active proxy detected on 127.0.0.1 (please start your proxy client)")
	}

	// 1. Prefer Mixed (works seamlessly with Go net/http, Node.js, and Chromium)
	for _, p := range active {
		if p.Type == "mixed" {
			return &p, nil
		}
	}

	// 2. Prefer HTTP (supports CONNECT tunnel for Go & Node.js)
	for _, p := range active {
		if p.Type == "http" {
			return &p, nil
		}
	}

	// 3. Fallback to SOCKS5
	return &active[0], nil
}
