package config

import (
	"encoding/hex"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Decryption key (from JS: TheOldLLm-Secure-2025-v9)
var decryptKey = []byte{84, 104, 101, 79, 108, 100, 76, 76, 109, 45, 83, 101, 99, 117, 114, 101, 45, 50, 48, 50, 53, 45, 118, 57}

// Encrypted API key (from JS source)
var encryptedAPIKey = "3b073c3e0d0f3329217b6f5b6250520f2e061e58531e151a67595e384c1281821a10605804194e5c23085a0d0a1f648c463d212806201107296b07363f112c485c6f6876071f051106285c693535473567261520193b48152047652514743b0b102e41874d3834215f5a186126192018226b1188845821770e302a395a342642676c24123a544d1c664773075e6c285a75342d1b691715351677034a2d1f0f146a5c61815a520f8d706a35183c150e391a21235d3432533c731a705c59621c1167654331102f413662657a2301282135186a6b6a5e6528861d321e4613031d2c1f701f2f591f4d5f761654834f6d416f19030f132e0c3d144577172912542b0d5f087e23587b2b4846445d0c274a2d35041e682428325144810e85518b68221a70453e1836360e27414c743437362a"

// Cached API key
var (
	cachedAPIKey     string
	cachedAPIKeyOnce sync.Once
)

// decryptAPIKey decrypts the API key using the same algorithm as the JS frontend
func decryptAPIKey() string {
	result := make([]byte, 0, len(encryptedAPIKey)/2)
	keyLen := len(decryptKey)

	for i := 0; i < len(encryptedAPIKey); i += 2 {
		if i+2 > len(encryptedAPIKey) {
			break
		}
		hexByte := encryptedAPIKey[i : i+2]
		b, err := hex.DecodeString(hexByte)
		if err != nil || len(b) == 0 {
			continue
		}

		pos := i / 2
		// Subtract (pos % 17) and mask with 0xFF
		val := (int(b[0]) - (pos % 17)) & 0xFF
		// XOR with key
		decrypted := byte(val) ^ decryptKey[pos%keyLen]
		result = append(result, decrypted)
	}

	return string(result)
}

// GetUpstreamAPIKey returns the dynamically generated API key
func GetUpstreamAPIKey() string {
	// Allow override via environment variable
	if envKey := os.Getenv("UPSTREAM_API_KEY"); envKey != "" {
		cachedAPIKeyOnce.Do(func() {
			log.Printf("[Config] Using UPSTREAM_API_KEY from environment")
			log.Printf("[Config] API Key: %s", envKey)
		})
		return envKey
	}

	cachedAPIKeyOnce.Do(func() {
		cachedAPIKey = decryptAPIKey()
		log.Printf("[Config] Decrypted API Key: %s", cachedAPIKey)
	})
	return cachedAPIKey
}

type ProxyConfig struct {
	URL      string
	Username string
	Password string
}

type Config struct {
	Port            string
	UpstreamURL     string
	MaxIdleConns    int
	MaxConnsPerHost int
	IdleConnTimeout time.Duration
	RequestTimeout  time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration

	// Proxy pool settings
	ProxyEnabled     bool
	Proxies          []ProxyConfig
	ProxyHealthCheck time.Duration
	ProxyRetryCount  int
}

func Load() *Config {
	cfg := &Config{
		Port:             getEnv("PORT", "8080"),
		UpstreamURL:      getEnv("UPSTREAM_URL", "https://theoldllm.vercel.app/api/proxy?provider=p5"),
		MaxIdleConns:     getEnvInt("MAX_IDLE_CONNS", 100),
		MaxConnsPerHost:  getEnvInt("MAX_CONNS_PER_HOST", 100),
		IdleConnTimeout:  getEnvDuration("IDLE_CONN_TIMEOUT", 90*time.Second),
		RequestTimeout:   getEnvDuration("REQUEST_TIMEOUT", 300*time.Second),
		ReadTimeout:      getEnvDuration("READ_TIMEOUT", 30*time.Second),
		WriteTimeout:     getEnvDuration("WRITE_TIMEOUT", 300*time.Second),
		ProxyEnabled:     getEnvBool("PROXY_ENABLED", true),
		ProxyHealthCheck: getEnvDuration("PROXY_HEALTH_CHECK", 30*time.Second),
		ProxyRetryCount:  getEnvInt("PROXY_RETRY_COUNT", 3),
	}

	// Parse proxy pool from environment
	// Format: PROXY_URLS="url1,url2,url3"
	// Format: PROXY_USERNAMES="user1,user2,user3"
	// Format: PROXY_PASSWORDS="pass1,pass2,pass3"
	if cfg.ProxyEnabled {
		urls := strings.Split(getEnv("PROXY_URLS", ""), ",")
		usernames := strings.Split(getEnv("PROXY_USERNAMES", ""), ",")
		passwords := strings.Split(getEnv("PROXY_PASSWORDS", ""), ",")

		for i, u := range urls {
			u = strings.TrimSpace(u)
			if u == "" {
				continue
			}

			proxy := ProxyConfig{URL: u}
			if i < len(usernames) {
				proxy.Username = strings.TrimSpace(usernames[i])
			}
			if i < len(passwords) {
				proxy.Password = strings.TrimSpace(passwords[i])
			}
			cfg.Proxies = append(cfg.Proxies, proxy)
		}
	}

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}
