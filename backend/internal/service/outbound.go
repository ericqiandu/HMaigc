package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

const maxOutboundRedirects = 5

var (
	outboundTransport         = newOutboundTransport(resolveOutboundHost)
	customRelayTransport      = newOutboundTransport(resolveCustomRelayHost)
	blockedSpecialUsePrefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
		netip.MustParsePrefix("100::/64"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
)

func ValidateOutboundURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return nil, BadAuthRequest("外部服务地址无效")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, BadAuthRequest("外部服务地址只支持 http/https")
	}
	if parsed.User != nil {
		return nil, BadAuthRequest("外部服务地址不允许包含认证信息")
	}
	if err := validateOutboundHost(parsed.Hostname()); err != nil {
		return nil, err
	}
	return parsed, nil
}

// 用户自定义渠道必须使用更严格的出口策略：只允许 HTTPS，不接受 URL 凭据，
// 仅部署者精确配置的主机可以路由到私网。
func ValidateCustomRelayURL(rawURL string) (*url.URL, error) {
	if len(strings.TrimSpace(rawURL)) > 4096 {
		return nil, BadAuthRequest("自定义渠道地址过长")
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" || !parsed.IsAbs() {
		return nil, BadAuthRequest("自定义渠道地址无效")
	}
	if parsed.Scheme != "https" {
		return nil, BadAuthRequest("自定义渠道中转只支持 HTTPS")
	}
	if parsed.User != nil {
		return nil, BadAuthRequest("自定义渠道地址不允许包含认证信息")
	}
	if parsed.Fragment != "" {
		return nil, BadAuthRequest("自定义渠道地址不允许包含片段")
	}
	if err := validateCustomRelayHost(parsed.Hostname()); err != nil {
		return nil, err
	}
	return parsed, nil
}

func OutboundHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: outboundTransport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxOutboundRedirects {
				return errors.New("外部服务重定向次数过多")
			}
			_, err := ValidateOutboundURL(req.URL.String())
			return err
		},
	}
}

func CustomRelayHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: customRelayTransport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("自定义渠道中转不允许重定向")
		},
	}
}

type outboundHostResolver func(context.Context, string) ([]net.IP, error)
type outboundDialContext func(context.Context, string, string) (net.Conn, error)

// ValidateKuaiziBaseURL 只接受不携带路径或能力信息的 origin，并在保存时验证全部 DNS 事实。
func ValidateKuaiziBaseURL(ctx context.Context, rawURL string, environment string) (*url.URL, error) {
	return validateKuaiziBaseURLWithResolver(ctx, rawURL, environment, defaultOutboundHostResolver)
}

func validateKuaiziBaseURLWithResolver(ctx context.Context, rawURL string, environment string, resolver outboundHostResolver) (*url.URL, error) {
	if len(strings.TrimSpace(rawURL)) > 2048 {
		return nil, BadAuthRequest("筷子服务地址过长")
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" {
		return nil, BadAuthRequest("筷子服务地址无效")
	}
	if parsed.User != nil || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, BadAuthRequest("筷子服务地址必须是无路径、查询或片段的 origin")
	}
	environment = strings.TrimSpace(environment)
	if environment != "production" && environment != "development" {
		return nil, BadAuthRequest("运行环境未配置，拒绝保存筷子服务地址")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, BadAuthRequest("筷子服务地址只支持 HTTPS")
	}
	addresses, err := resolveKuaiziHost(ctx, parsed.Hostname(), resolver)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "http" {
		if environment != "development" || !allLoopbackIPs(addresses) {
			return nil, BadAuthRequest("HTTP 筷子服务地址仅允许开发环境 loopback origin")
		}
		return parsed, nil
	}
	for _, address := range addresses {
		if blockedSpecialUseIP(address) {
			return nil, BadAuthRequest("筷子服务地址不允许指向本机、内网或链路本地地址")
		}
	}
	return parsed, nil
}

func KuaiziHTTPClient(environment string, timeout time.Duration) *http.Client {
	transport, err := newKuaiziHTTPTransport(environment, defaultOutboundHostResolver, os.Getenv("CANVAS_KUAIZI_PROXY_URL"))
	if err != nil {
		transport = rejectingRoundTripper{err: err}
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("筷子服务请求不允许重定向")
		},
	}
}

type rejectingRoundTripper struct {
	err error
}

func (transport rejectingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, transport.err
}

type validatingKuaiziProxyTransport struct {
	transport   *http.Transport
	environment string
	resolver    outboundHostResolver
}

func (transport *validatingKuaiziProxyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, BadAuthRequest("筷子服务请求地址无效")
	}
	origin := &url.URL{Scheme: request.URL.Scheme, Host: request.URL.Host, User: request.URL.User}
	if _, err := validateKuaiziBaseURLWithResolver(request.Context(), origin.String(), transport.environment, transport.resolver); err != nil {
		return nil, err
	}
	return transport.transport.RoundTrip(request)
}

func (transport *validatingKuaiziProxyTransport) CloseIdleConnections() {
	transport.transport.CloseIdleConnections()
}

func newKuaiziHTTPTransport(environment string, resolver outboundHostResolver, rawProxyURL string) (http.RoundTripper, error) {
	if strings.TrimSpace(rawProxyURL) == "" {
		return newKuaiziTransport(environment, resolver), nil
	}
	proxyURL, err := validateKuaiziProxyURL(rawProxyURL, environment)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	base := newBaseHTTPTransport(dialer.DialContext)
	base.Proxy = http.ProxyURL(proxyURL)
	return &validatingKuaiziProxyTransport{transport: base, environment: environment, resolver: resolver}, nil
}

func validateKuaiziProxyURL(rawProxyURL string, environment string) (*url.URL, error) {
	if strings.TrimSpace(environment) != "development" {
		return nil, BadAuthRequest("筷子出站代理仅允许开发环境使用")
	}
	parsed, err := url.Parse(strings.TrimSpace(rawProxyURL))
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "http" || parsed.Hostname() == "" || parsed.Port() == "" {
		return nil, BadAuthRequest("筷子出站代理地址无效")
	}
	if parsed.User != nil || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, BadAuthRequest("筷子出站代理必须是不含凭据、路径、查询或片段的 HTTP origin")
	}
	host := normalizeOutboundHost(parsed.Hostname())
	address := net.ParseIP(host)
	if host != "host.docker.internal" && host != "localhost" && (address == nil || !address.IsLoopback()) {
		return nil, BadAuthRequest("筷子出站代理必须指向本机开发代理")
	}
	return parsed, nil
}

func defaultOutboundHostResolver(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

func newKuaiziTransport(environment string, resolver outboundHostResolver) *http.Transport {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	return newKuaiziTransportWithDialer(environment, resolver, dialer.DialContext)
}

func newKuaiziTransportWithDialer(environment string, resolver outboundHostResolver, dial outboundDialContext) *http.Transport {
	transport := newBaseHTTPTransport(dial)
	transport.DialContext = func(ctx context.Context, network string, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := resolveKuaiziHost(ctx, host, resolver)
		if err != nil {
			return nil, err
		}
		if environment == "development" && allLoopbackIPs(addresses) {
			return dial(ctx, network, net.JoinHostPort(addresses[0].String(), port))
		}
		for _, candidate := range addresses {
			if blockedSpecialUseIP(candidate) {
				return nil, BadAuthRequest("筷子服务连接不允许解析到本机、内网或特殊用途地址")
			}
		}
		return dial(ctx, network, net.JoinHostPort(addresses[0].String(), port))
	}
	return transport
}

func newBaseHTTPTransport(dial outboundDialContext) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			return dial(ctx, network, address)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

func resolveKuaiziHost(ctx context.Context, host string, resolver outboundHostResolver) ([]net.IP, error) {
	host = normalizeOutboundHost(host)
	if host == "" {
		return nil, BadAuthRequest("筷子服务域名无效")
	}
	if address := net.ParseIP(host); address != nil {
		return []net.IP{address}, nil
	}
	addresses, err := resolver(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, BadAuthRequest("筷子服务域名解析失败")
	}
	return addresses, nil
}

func allLoopbackIPs(addresses []net.IP) bool {
	if len(addresses) == 0 {
		return false
	}
	for _, address := range addresses {
		if address == nil || !address.IsLoopback() {
			return false
		}
	}
	return true
}

func newOutboundTransport(resolveHost func(context.Context, string) ([]net.IP, error)) *http.Transport {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := resolveHost(ctx, host)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

func validateOutboundHost(host string) error {
	_, err := resolveOutboundHost(context.Background(), host)
	return err
}

func validateCustomRelayHost(host string) error {
	_, err := resolveCustomRelayHost(context.Background(), host)
	return err
}

func resolveOutboundHost(ctx context.Context, host string) ([]net.IP, error) {
	host = normalizeOutboundHost(host)
	return resolveOutboundHostWithPolicy(ctx, host, allowPrivateUpstreams() || allowedPrivateUpstreamHost(host))
}

func resolveCustomRelayHost(ctx context.Context, host string) ([]net.IP, error) {
	host = normalizeOutboundHost(host)
	allowPrivateHost := allowedPrivateUpstreamHost(host)
	addresses, err := resolveOutboundHostWithPolicy(ctx, host, allowPrivateHost)
	if err != nil {
		return nil, err
	}
	if !allowPrivateHost {
		for _, ip := range addresses {
			if blockedCustomRelayIP(ip) {
				return nil, BadAuthRequest("不允许访问保留地址或特殊用途地址")
			}
		}
	}
	return addresses, nil
}

func resolveOutboundHostWithPolicy(ctx context.Context, host string, allowPrivateHost bool) ([]net.IP, error) {
	if host == "" {
		return nil, BadAuthRequest("外部服务域名无效")
	}
	if !allowPrivateHost && (host == "localhost" || strings.HasSuffix(host, ".localhost")) {
		return nil, BadAuthRequest("不允许访问本机或内网地址")
	}
	addresses, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, BadAuthRequest("外部服务域名解析失败")
	}
	if len(addresses) == 0 {
		return nil, BadAuthRequest("外部服务域名没有可用地址")
	}
	if !allowPrivateHost {
		for _, ip := range addresses {
			if blockedOutboundIP(ip) {
				return nil, BadAuthRequest("不允许访问本机、内网或链路本地地址")
			}
		}
	}
	return addresses, nil
}

func blockedOutboundIP(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()
}

func blockedCustomRelayIP(ip net.IP) bool {
	return blockedSpecialUseIP(ip)
}

// blockedSpecialUseIP 是外部凭据和自定义中转共用的生产出口边界，禁止维护两套 CIDR 列表。
func blockedSpecialUseIP(ip net.IP) bool {
	if blockedOutboundIP(ip) {
		return true
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	address = address.Unmap()
	for _, prefix := range blockedSpecialUsePrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func allowPrivateUpstreams() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS")))
	return value == "1" || value == "true" || value == "yes"
}

// allowedPrivateUpstreamHost lets operators pin only explicitly trusted upstream
// hostnames to an internal route without disabling SSRF protection for every URL.
func allowedPrivateUpstreamHost(host string) bool {
	host = normalizeOutboundHost(host)
	if host == "" {
		return false
	}
	for _, configured := range strings.Split(os.Getenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS"), ",") {
		if normalizeOutboundHost(configured) == host {
			return true
		}
	}
	return false
}

func normalizeOutboundHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}
