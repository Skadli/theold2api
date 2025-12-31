package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"theold2api/config"
)

type ProxyStatus struct {
	Config      config.ProxyConfig
	Healthy     bool
	FailCount   int32
	LastChecked time.Time
	LastUsed    time.Time
}

type Client struct {
	directClient *http.Client
	upstreamURL  string
	cfg          *config.Config

	// Proxy pool
	proxies      []*ProxyStatus
	proxyClients map[string]*http.Client
	proxyMu      sync.RWMutex
	currentIdx   uint32

	// Health check
	stopCh chan struct{}
}

func NewClient(cfg *config.Config) *Client {
	c := &Client{
		upstreamURL:  cfg.UpstreamURL,
		cfg:          cfg,
		proxyClients: make(map[string]*http.Client),
		stopCh:       make(chan struct{}),
	}

	// Create direct client (no proxy)
	c.directClient = createHTTPClient(cfg, nil)

	// Initialize proxy pool
	if cfg.ProxyEnabled && len(cfg.Proxies) > 0 {
		for _, p := range cfg.Proxies {
			c.proxies = append(c.proxies, &ProxyStatus{
				Config:  p,
				Healthy: true,
			})
			c.proxyClients[p.URL] = createHTTPClient(cfg, &p)
		}
		log.Printf("[Proxy] Initialized proxy pool with %d proxies", len(c.proxies))

		// Start health check goroutine
		go c.healthCheckLoop()
	}

	return c
}

func createHTTPClient(cfg *config.Config, proxy *config.ProxyConfig) *http.Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxConnsPerHost,
		MaxConnsPerHost:       cfg.MaxConnsPerHost,
		IdleConnTimeout:       cfg.IdleConnTimeout,
		TLSHandshakeTimeout:   10 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:     true,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if proxy != nil {
		proxyURL, err := url.Parse(proxy.URL)
		if err == nil {
			if proxy.Username != "" && proxy.Password != "" {
				proxyURL.User = url.UserPassword(proxy.Username, proxy.Password)
			}
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   cfg.RequestTimeout,
	}
}

func (c *Client) healthCheckLoop() {
	ticker := time.NewTicker(c.cfg.ProxyHealthCheck)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.checkAllProxies()
		}
	}
}

func (c *Client) checkAllProxies() {
	c.proxyMu.RLock()
	proxies := c.proxies
	c.proxyMu.RUnlock()

	for _, p := range proxies {
		go c.checkProxy(p)
	}
}

func (c *Client) checkProxy(p *ProxyStatus) {
	client := c.proxyClients[p.Config.URL]
	if client == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodHead, "https://www.google.com", nil)
	req.Header.Set("User-Agent", RandomUserAgent())

	resp, err := client.Do(req)
	if err != nil {
		atomic.AddInt32(&p.FailCount, 1)
		if atomic.LoadInt32(&p.FailCount) >= 3 {
			c.proxyMu.Lock()
			p.Healthy = false
			c.proxyMu.Unlock()
			log.Printf("[Proxy] Marked unhealthy: %s (fail count: %d)", p.Config.URL, p.FailCount)
		}
		return
	}
	resp.Body.Close()

	// Reset on success
	c.proxyMu.Lock()
	p.Healthy = true
	p.FailCount = 0
	p.LastChecked = time.Now()
	c.proxyMu.Unlock()
}

func (c *Client) getNextProxy() (*ProxyStatus, *http.Client) {
	if len(c.proxies) == 0 {
		return nil, c.directClient
	}

	c.proxyMu.RLock()
	defer c.proxyMu.RUnlock()

	// Round-robin with health check
	startIdx := atomic.AddUint32(&c.currentIdx, 1)
	for i := 0; i < len(c.proxies); i++ {
		idx := (int(startIdx) + i) % len(c.proxies)
		p := c.proxies[idx]
		if p.Healthy {
			p.LastUsed = time.Now()
			return p, c.proxyClients[p.Config.URL]
		}
	}

	// All proxies unhealthy, try first one anyway
	p := c.proxies[0]
	return p, c.proxyClients[p.Config.URL]
}

func (c *Client) markProxyFailed(p *ProxyStatus) {
	if p == nil {
		return
	}
	atomic.AddInt32(&p.FailCount, 1)
	if atomic.LoadInt32(&p.FailCount) >= int32(c.cfg.ProxyRetryCount) {
		c.proxyMu.Lock()
		p.Healthy = false
		c.proxyMu.Unlock()
		log.Printf("[Proxy] Marked unhealthy after failures: %s", p.Config.URL)
	}
}

func (c *Client) Do(req *http.Request) (*http.Response, string, error) {
	if !c.cfg.ProxyEnabled || len(c.proxies) == 0 {
		resp, err := c.directClient.Do(req)
		return resp, "direct", err
	}

	for i := 0; i < c.cfg.ProxyRetryCount; i++ {
		proxy, client := c.getNextProxy()
		proxyURL := "unknown"
		if proxy != nil {
			proxyURL = proxy.Config.URL
		}

		resp, err := client.Do(req)
		if err == nil {
			return resp, proxyURL, nil
		}

		c.markProxyFailed(proxy)
		log.Printf("[Proxy] Failed via %s (attempt %d/%d): %v", proxyURL, i+1, c.cfg.ProxyRetryCount, err)
	}

	// Fallback to direct connection
	log.Printf("[Proxy] All proxies failed, fallback to direct")
	resp, err := c.directClient.Do(req)
	return resp, "direct(fallback)", err
}

func (c *Client) UpstreamURL() string {
	return c.upstreamURL
}

func (c *Client) HTTPClient() *http.Client {
	return c.directClient
}

func (c *Client) Close() {
	close(c.stopCh)
}

// ==================== Random Request Parameters ====================

var (
	chromeVersions  = []int{120, 121, 122, 123, 124, 125, 126, 127, 128, 129, 130, 131, 132, 133, 134, 135, 136, 137, 138, 139, 140, 141, 142, 143}
	firefoxVersions = []int{120, 121, 122, 123, 124, 125, 126, 127, 128, 129, 130, 131, 132, 133, 134, 135}
	safariVersions  = []string{"17.0", "17.1", "17.2", "17.3", "17.4", "17.5", "18.0", "18.1", "18.2"}
	edgeVersions    = []int{120, 121, 122, 123, 124, 125, 126, 127, 128, 129, 130, 131, 132, 133, 134, 135}

	windowsVersions = []string{"10.0", "11.0"}
	macVersions     = []string{"10_15_7", "11_0", "12_0", "13_0", "14_0", "15_0"}

	platforms = []string{"Windows", "macOS", "Linux"}
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func RandomUserAgent() string {
	browser := rand.Intn(4)
	switch browser {
	case 0: // Chrome
		return randomChromeUA()
	case 1: // Firefox
		return randomFirefoxUA()
	case 2: // Safari
		return randomSafariUA()
	default: // Edge
		return randomEdgeUA()
	}
}

func randomChromeUA() string {
	version := chromeVersions[rand.Intn(len(chromeVersions))]
	build := rand.Intn(9999)

	os := rand.Intn(3)
	switch os {
	case 0: // Windows
		winVer := windowsVersions[rand.Intn(len(windowsVersions))]
		return fmt.Sprintf("Mozilla/5.0 (Windows NT %s; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.%d.0 Safari/537.36", winVer, version, build)
	case 1: // macOS
		macVer := macVersions[rand.Intn(len(macVersions))]
		return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.%d.0 Safari/537.36", macVer, version, build)
	default: // Linux
		return fmt.Sprintf("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.%d.0 Safari/537.36", version, build)
	}
}

func randomFirefoxUA() string {
	version := firefoxVersions[rand.Intn(len(firefoxVersions))]

	os := rand.Intn(3)
	switch os {
	case 0: // Windows
		winVer := windowsVersions[rand.Intn(len(windowsVersions))]
		return fmt.Sprintf("Mozilla/5.0 (Windows NT %s; Win64; x64; rv:%d.0) Gecko/20100101 Firefox/%d.0", winVer, version, version)
	case 1: // macOS
		macVer := macVersions[rand.Intn(len(macVersions))]
		return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X %s; rv:%d.0) Gecko/20100101 Firefox/%d.0", macVer, version, version)
	default: // Linux
		return fmt.Sprintf("Mozilla/5.0 (X11; Linux x86_64; rv:%d.0) Gecko/20100101 Firefox/%d.0", version, version)
	}
}

func randomSafariUA() string {
	version := safariVersions[rand.Intn(len(safariVersions))]
	macVer := macVersions[rand.Intn(len(macVersions))]
	webkitBuild := 600 + rand.Intn(100)
	return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/%d.1.15 (KHTML, like Gecko) Version/%s Safari/%d.1.15", macVer, webkitBuild, version, webkitBuild)
}

func randomEdgeUA() string {
	version := edgeVersions[rand.Intn(len(edgeVersions))]
	chromeVer := chromeVersions[rand.Intn(len(chromeVersions))]
	build := rand.Intn(9999)

	winVer := windowsVersions[rand.Intn(len(windowsVersions))]
	return fmt.Sprintf("Mozilla/5.0 (Windows NT %s; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.%d.0 Safari/537.36 Edg/%d.0.%d.0", winVer, chromeVer, build, version, build)
}

func RandomSecChUa() string {
	version := chromeVersions[rand.Intn(len(chromeVersions))]
	brands := []string{
		fmt.Sprintf(`"Google Chrome";v="%d", "Chromium";v="%d", "Not_A Brand";v="24"`, version, version),
		fmt.Sprintf(`"Chromium";v="%d", "Google Chrome";v="%d", "Not-A.Brand";v="99"`, version, version),
		fmt.Sprintf(`"Google Chrome";v="%d", "Not;A=Brand";v="8", "Chromium";v="%d"`, version, version),
		fmt.Sprintf(`"Not A(Brand";v="99", "Google Chrome";v="%d", "Chromium";v="%d"`, version, version),
	}
	return brands[rand.Intn(len(brands))]
}

func RandomSecChUaPlatform() string {
	return fmt.Sprintf(`"%s"`, platforms[rand.Intn(len(platforms))])
}

func RandomSecChUaMobile() string {
	if rand.Intn(10) < 1 { // 10% mobile
		return "?1"
	}
	return "?0"
}

func RandomAcceptLanguage() string {
	langs := []string{
		"en-US,en;q=0.9",
		"en-GB,en;q=0.9,en-US;q=0.8",
		"zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7",
		"zh-TW,zh;q=0.9,en-US;q=0.8,en;q=0.7",
		"ja-JP,ja;q=0.9,en-US;q=0.8,en;q=0.7",
		"ko-KR,ko;q=0.9,en-US;q=0.8,en;q=0.7",
		"de-DE,de;q=0.9,en-US;q=0.8,en;q=0.7",
		"fr-FR,fr;q=0.9,en-US;q=0.8,en;q=0.7",
		"es-ES,es;q=0.9,en-US;q=0.8,en;q=0.7",
	}
	return langs[rand.Intn(len(langs))]
}

func RandomPriority() string {
	priorities := []string{
		"u=1, i",
		"u=0, i",
		"u=1",
		"u=0",
	}
	return priorities[rand.Intn(len(priorities))]
}
