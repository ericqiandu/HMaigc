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

func TestKuaiziHTTPClientRejectsRedirectBeforeForwardingAPIKey(t *testing.T) {
	redirected := false
	destination := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		redirected = true
		if request.Header.Get("ApiKey") != "" {
			t.Fatal("ApiKey reached redirect destination")
		}
	}))
	defer destination.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", destination.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	client := KuaiziHTTPClient("production", time.Second)
	client.Transport = source.Client().Transport
	request, err := http.NewRequest(http.MethodPost, source.URL, strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("ApiKey", "secret")
	if _, err := client.Do(request); err == nil {
		t.Fatal("KuaiziHTTPClient followed redirect")
	}
	if redirected {
		t.Fatal("redirect destination received request")
	}
}
