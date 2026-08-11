package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateOutboundURLRejectsPrivateHosts(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "false")
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "")
	for _, rawURL := range []string{"http://127.0.0.1:8080", "http://localhost:8080", "http://169.254.169.254/latest/meta-data"} {
		if _, err := ValidateOutboundURL(rawURL); err == nil {
			t.Fatalf("ValidateOutboundURL(%q) should fail", rawURL)
		}
	}
}

func TestValidateOutboundURLAllowsExplicitPrivateUpstreamOverride(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "")
	if _, err := ValidateOutboundURL("http://127.0.0.1:8080"); err != nil {
		t.Fatalf("ValidateOutboundURL() error = %v", err)
	}
}

func TestValidateOutboundURLAllowsOnlyNamedPrivateUpstream(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "false")
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	if _, err := ValidateOutboundURL("http://127.0.0.1:8080"); err != nil {
		t.Fatalf("ValidateOutboundURL() error = %v", err)
	}
	if _, err := ValidateOutboundURL("http://127.0.0.2:8080"); err == nil {
		t.Fatal("ValidateOutboundURL() should reject an unlisted private host")
	}
}

func TestAllowedPrivateUpstreamHostUsesExactCaseInsensitiveMatch(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", " API.EXAMPLE.COM.,trusted.internal ")
	if !allowedPrivateUpstreamHost("api.example.com") {
		t.Fatal("allowedPrivateUpstreamHost() should allow exact normalized hostname")
	}
	if allowedPrivateUpstreamHost("api.example.com.evil.test") {
		t.Fatal("allowedPrivateUpstreamHost() should reject hostname suffix confusion")
	}
}

func TestValidateCustomRelayURLUsesHTTPSAndExactPrivateAllowlist(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "")
	if _, err := ValidateCustomRelayURL("http://127.0.0.1:8080/v1/models"); err == nil {
		t.Fatal("ValidateCustomRelayURL() should reject HTTP")
	}
	if _, err := ValidateCustomRelayURL("https://127.0.0.1:8080/v1/models"); err == nil {
		t.Fatal("ValidateCustomRelayURL() should ignore the global private upstream override")
	}
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	if _, err := ValidateCustomRelayURL("https://127.0.0.1:8080/v1/models"); err != nil {
		t.Fatalf("ValidateCustomRelayURL() error = %v", err)
	}
}

func TestValidateCustomRelayURLRejectsCredentialsAndFragment(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	for _, rawURL := range []string{
		"https://user:pass@127.0.0.1/v1/models",
		"https://127.0.0.1/v1/models#secret",
	} {
		if _, err := ValidateCustomRelayURL(rawURL); err == nil {
			t.Fatalf("ValidateCustomRelayURL(%q) should fail", rawURL)
		}
	}
}

func TestBlockedCustomRelayIPRejectsCarrierGradeNATAndReservedRanges(t *testing.T) {
	for _, value := range []string{"100.100.100.200", "192.0.2.10", "198.18.0.1", "2001:db8::1"} {
		if !blockedCustomRelayIP(net.ParseIP(value)) {
			t.Fatalf("blockedCustomRelayIP(%q) = false", value)
		}
	}
	if blockedCustomRelayIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("blockedCustomRelayIP() rejected a public address")
	}
}

func TestCustomRelayHTTPClientDoesNotFollowRedirects(t *testing.T) {
	redirected := false
	destination := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected = true
	}))
	defer destination.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", destination.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer source.Close()

	client := CustomRelayHTTPClient(time.Second)
	client.Transport = source.Client().Transport
	if _, err := client.Get(source.URL); err == nil {
		t.Fatal("CustomRelayHTTPClient() should reject redirects")
	}
	if redirected {
		t.Fatal("redirect destination should not receive the request")
	}
}

func TestKuaiziEndpointProductionAcceptsOnlyPublicHTTPSOrigin(t *testing.T) {
	publicResolver := func(_ context.Context, _ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	if parsed, err := validateKuaiziBaseURLWithResolver(context.Background(), "https://api.example.com:8443", "production", publicResolver); err != nil || parsed.String() != "https://api.example.com:8443" {
		t.Fatalf("public production origin = %v, %v", parsed, err)
	}
	for _, value := range []string{
		"http://api.example.com", "https://user:pass@api.example.com", "https://api.example.com/", "https://api.example.com/path",
		"https://api.example.com?query=1", "https://api.example.com#fragment", "https://127.0.0.1", "https://169.254.169.254",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := validateKuaiziBaseURLWithResolver(context.Background(), value, "production", publicResolver); err == nil {
				t.Fatalf("unsafe production endpoint %q was accepted", value)
			}
		})
	}
}

func TestKuaiziEndpointDevelopmentAllowsOnlyLoopbackHTTP(t *testing.T) {
	resolver := func(_ context.Context, host string) ([]net.IP, error) {
		if host == "localhost" {
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		}
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	if _, err := validateKuaiziBaseURLWithResolver(context.Background(), "http://localhost:8080", "development", resolver); err != nil {
		t.Fatalf("development loopback HTTP rejected: %v", err)
	}
	if _, err := validateKuaiziBaseURLWithResolver(context.Background(), "http://api.example.com", "development", resolver); err == nil {
		t.Fatal("development public HTTP endpoint was accepted")
	}
}

func TestKuaiziEndpointRejectsPrivateOrRebindingDNSFacts(t *testing.T) {
	for _, addresses := range [][]net.IP{
		{net.ParseIP("10.0.0.2")},
		{net.ParseIP("93.184.216.34"), net.ParseIP("127.0.0.1")},
		{net.ParseIP("fe80::1")},
	} {
		resolver := func(_ context.Context, _ string) ([]net.IP, error) { return addresses, nil }
		if _, err := validateKuaiziBaseURLWithResolver(context.Background(), "https://api.example.com", "production", resolver); err == nil {
			t.Fatalf("unsafe DNS facts %#v were accepted", addresses)
		}
	}

	transport := newKuaiziTransport("production", func(_ context.Context, _ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	})
	request, err := http.NewRequest(http.MethodGet, "https://api.example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(request); err == nil {
		t.Fatal("DNS rebinding target reached transport without rejection")
	}
}

func TestKuaiziEndpointRejectsSpecialUseDirectIPs(t *testing.T) {
	resolver := func(_ context.Context, _ string) ([]net.IP, error) {
		t.Fatal("literal IP must not use DNS resolver")
		return nil, nil
	}
	for _, address := range []string{"100.64.0.1", "198.18.0.1", "192.0.0.10", "240.0.0.1"} {
		if _, err := validateKuaiziBaseURLWithResolver(context.Background(), "https://"+address, "production", resolver); err == nil {
			t.Fatalf("special-use direct IP %s was accepted", address)
		}
	}
}

func TestKuaiziEndpointRejectsSpecialUseInitialDNSFacts(t *testing.T) {
	for _, address := range []string{"100.64.0.1", "198.18.0.1", "192.0.0.10", "240.0.0.1"} {
		resolver := func(_ context.Context, _ string) ([]net.IP, error) {
			return []net.IP{net.ParseIP(address)}, nil
		}
		if _, err := validateKuaiziBaseURLWithResolver(context.Background(), "https://api.example.com", "production", resolver); err == nil {
			t.Fatalf("special-use initial DNS IP %s was accepted", address)
		}
	}
}

func TestKuaiziTransportRejectsSpecialUseDNSRebindingBeforeDial(t *testing.T) {
	for _, address := range []string{"100.64.0.1", "198.18.0.1", "192.0.0.10", "240.0.0.1"} {
		dialed := false
		transport := newKuaiziTransportWithDialer(
			"production",
			func(_ context.Context, _ string) ([]net.IP, error) { return []net.IP{net.ParseIP(address)}, nil },
			func(_ context.Context, _, _ string) (net.Conn, error) {
				dialed = true
				return nil, errors.New("unexpected dial")
			},
		)
		request, err := http.NewRequest(http.MethodGet, "https://api.example.com", nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = transport.RoundTrip(request)
		if err == nil || !strings.Contains(err.Error(), "不允许") {
			t.Fatalf("special-use rebind %s error = %v", address, err)
		}
		if dialed {
			t.Fatalf("special-use rebind %s reached network dial", address)
		}
	}
}

func TestExternalBinaryTransportRejectsSpecialUseAddressBeforeEveryDial(t *testing.T) {
	for _, address := range []string{
		"100.64.0.1",
		"192.0.0.10",
		"192.0.2.10",
		"198.18.0.1",
		"198.51.100.10",
		"203.0.113.10",
		"240.0.0.1",
		"100::1",
		"2001:db8::1",
	} {
		t.Run(address, func(t *testing.T) {
			dialed := false
			client := newExternalBinaryHTTPClientWithDialer(
				time.Second,
				func(_ context.Context, _ string) ([]net.IP, error) {
					return []net.IP{net.ParseIP(address)}, nil
				},
				func(_ context.Context, _, _ string) (net.Conn, error) {
					dialed = true
					return nil, errors.New("unexpected dial")
				},
			)
			transport := client.Transport.(*externalBinaryRoundTripper).transport
			if _, err := transport.DialContext(context.Background(), "tcp", "cdn.example.com:443"); err == nil {
				t.Fatal("special-use address reached external binary dial")
			}
			if dialed {
				t.Fatal("special-use address reached network dial")
			}
		})
	}

	t.Run("mixed public and special-use answers", func(t *testing.T) {
		dialed := false
		client := newExternalBinaryHTTPClientWithDialer(
			time.Second,
			func(_ context.Context, _ string) ([]net.IP, error) {
				return []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("198.18.0.1")}, nil
			},
			func(_ context.Context, _, _ string) (net.Conn, error) {
				dialed = true
				return nil, errors.New("unexpected dial")
			},
		)
		transport := client.Transport.(*externalBinaryRoundTripper).transport
		if _, err := transport.DialContext(context.Background(), "tcp", "cdn.example.com:443"); err == nil {
			t.Fatal("mixed DNS facts reached external binary dial")
		}
		if dialed {
			t.Fatal("mixed DNS facts reached network dial")
		}
	})
}

func TestExternalBinaryRedirectRejectsSpecialUseDNSRebindingBeforeDial(t *testing.T) {
	resolveCount := 0
	dialed := false
	client := newExternalBinaryHTTPClientWithDialer(
		time.Second,
		func(_ context.Context, host string) ([]net.IP, error) {
			if host != "redirect.example.com" {
				t.Fatalf("resolver host = %q", host)
			}
			resolveCount++
			if resolveCount == 1 {
				return []net.IP{net.ParseIP("8.8.8.8")}, nil
			}
			return []net.IP{net.ParseIP("198.18.0.1")}, nil
		},
		func(_ context.Context, _, _ string) (net.Conn, error) {
			dialed = true
			return nil, errors.New("unexpected dial")
		},
	)
	redirect, err := http.NewRequest(http.MethodGet, "https://redirect.example.com/result.mp4", nil)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := http.NewRequest(http.MethodGet, "https://origin.example.com/task", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(redirect, []*http.Request{previous}); err != nil {
		t.Fatalf("public redirect validation error = %v", err)
	}
	transport := client.Transport.(*externalBinaryRoundTripper).transport
	if _, err := transport.DialContext(context.Background(), "tcp", "redirect.example.com:443"); err == nil {
		t.Fatal("DNS-rebound redirect reached external binary dial")
	}
	if resolveCount != 2 {
		t.Fatalf("resolve count = %d, want redirect validation plus per-dial resolution", resolveCount)
	}
	if dialed {
		t.Fatal("DNS-rebound redirect reached network dial")
	}
}

func TestExternalBinaryRedirectRejectsSpecialUseAddressDuringRedirectValidation(t *testing.T) {
	client := newExternalBinaryHTTPClientWithDialer(
		time.Second,
		func(_ context.Context, _ string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("203.0.113.10")}, nil
		},
		func(_ context.Context, _, _ string) (net.Conn, error) {
			t.Fatal("redirect validation must not dial")
			return nil, nil
		},
	)
	redirect, err := http.NewRequest(http.MethodGet, "https://redirect.example.com/result.mp4", nil)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := http.NewRequest(http.MethodGet, "https://origin.example.com/task", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(redirect, []*http.Request{previous}); err == nil {
		t.Fatal("special-use redirect target passed redirect validation")
	}
}

func TestExternalBinaryClientRejectsInitialHTTPBeforeDial(t *testing.T) {
	dialed := false
	client := newExternalBinaryHTTPClientWithDialer(
		time.Second,
		func(_ context.Context, _ string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("8.8.8.8")}, nil
		},
		func(_ context.Context, _, _ string) (net.Conn, error) {
			dialed = true
			return nil, errors.New("unexpected dial")
		},
	)
	if _, err := client.Get("http://public.example.com/result.mp4"); err == nil {
		t.Fatal("initial HTTP external binary URL was accepted")
	}
	if dialed {
		t.Fatal("initial HTTP external binary URL reached network dial")
	}
}

func TestExternalBinaryClientRejectsHTTPSRedirectToHTTPBeforeSecondDial(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", "http://redirect.example.com/result.mp4")
		writer.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	serverAddress := strings.TrimPrefix(server.URL, "https://")
	dialCount := 0
	dialer := &net.Dialer{}
	client := newExternalBinaryHTTPClientWithDialer(
		time.Second,
		func(_ context.Context, _ string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("8.8.8.8")}, nil
		},
		func(ctx context.Context, network string, _ string) (net.Conn, error) {
			dialCount++
			if dialCount > 1 {
				return nil, errors.New("second hop dial attempted")
			}
			return dialer.DialContext(ctx, network, serverAddress)
		},
	)
	transport := client.Transport.(*externalBinaryRoundTripper).transport
	transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	transport.TLSClientConfig.InsecureSkipVerify = true // 仅测试注入域名连接本地 TLS server。
	if _, err := client.Get("https://origin.example.com/result.mp4"); err == nil {
		t.Fatal("HTTPS to HTTP redirect was accepted")
	}
	if dialCount != 1 {
		t.Fatalf("dial count = %d, want only the initial HTTPS dial", dialCount)
	}
}
