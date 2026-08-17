package handler

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

var riskIdentityCloudflarePrefixes = mustRiskIdentityCloudflarePrefixes([]string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22", "2400:cb00::/32",
	"2606:4700::/32", "2803:f800::/32", "2405:b500::/32", "2405:8100::/32",
	"2a06:98c0::/29", "2c0f:f248::/32",
})

var riskIdentityEventSequence atomic.Uint64

const (
	riskIdentityContextKey = "risk_identity_v2_context"
	riskDeviceCookieName   = "risk_device"
	riskDeviceCookieMaxAge = 180 * 24 * 60 * 60
)

type riskRequestIdentity struct {
	EventRoot           string
	ClientIP            string
	IPSource            string
	ProxyChainValid     bool
	CountryCode         string
	Region              string
	City                string
	ASN                 int64
	GeoSource           string
	GeoVerified         bool
	BrowserInstanceID   string
	BrowserCookieStatus string
	BrowserFamily       string
	OSFamily            string
	DeviceClass         string
	LanguageFamily      string
}

func requestRiskIdentity(c *gin.Context, includeBrowser bool) *riskRequestIdentity {
	if c == nil || c.Request == nil {
		return &riskRequestIdentity{EventRoot: randomRiskID()}
	}
	if value, ok := c.Get(riskIdentityContextKey); ok {
		if identity, valid := value.(*riskRequestIdentity); valid && identity != nil {
			return identity
		}
	}
	requestID := newRiskIdentityEventRoot()
	clientIP := strings.TrimSpace(ip.GetTrustedClientIP(c))
	source := "remote_addr"
	proxyValid := true
	remoteIP := remoteAddress(c.Request.RemoteAddr)
	if remoteIP != "" && clientIP != "" && remoteIP != clientIP {
		source = "trusted_xff"
		if strings.TrimSpace(c.GetHeader("X-Real-IP")) == clientIP {
			source = "trusted_real_ip"
		}
	}
	if clientIP == "" {
		proxyValid = false
	}
	identity := &riskRequestIdentity{EventRoot: requestID, ClientIP: clientIP, IPSource: source, ProxyChainValid: proxyValid}
	if includeBrowser {
		identity.BrowserInstanceID, identity.BrowserCookieStatus = ensureSignedRiskDeviceCookie(c)
	}
	identity.BrowserFamily, identity.OSFamily, identity.DeviceClass = coarseBrowserProfile(c.GetHeader("User-Agent"))
	if language := strings.ToLower(strings.TrimSpace(strings.Split(c.GetHeader("Accept-Language"), ",")[0])); len(language) <= 24 {
		identity.LanguageFamily = language
	}
	cfIP := strings.TrimSpace(c.GetHeader("CF-Connecting-IP"))
	if envBoolRiskIdentity("RISK_IDENTITY_TRUST_CLOUDFLARE_HEADERS") && source != "remote_addr" && sameRiskIdentityAddress(cfIP, clientIP) && riskIdentityLastForwardedHopIsCloudflare(c) {
		identity.IPSource = "cf_connecting_ip"
		identity.CountryCode = strings.ToUpper(firstRiskIdentityHeader(c, "X-Risk-CF-Country", "CF-IPCountry"))
		if !validRiskIdentityCountryCode(identity.CountryCode) {
			identity.CountryCode = ""
		}
		identity.Region = boundedHeader(firstRiskIdentityHeader(c, "X-Risk-CF-Region", "CF-Region"), 80)
		identity.City = boundedHeader(firstRiskIdentityHeader(c, "X-Risk-CF-City", "CF-IPCity"), 120)
		if asn, err := strconv.ParseInt(firstRiskIdentityHeader(c, "X-Risk-CF-ASN", "CF-ASN"), 10, 64); err == nil && asn > 0 && asn <= 4294967295 {
			identity.ASN = asn
		}
		if identity.CountryCode != "" || identity.Region != "" || identity.City != "" || identity.ASN > 0 {
			identity.GeoSource = "cloudflare_verified"
			identity.GeoVerified = true
		}
	}
	c.Set(riskIdentityContextKey, identity)
	return identity
}

func mustRiskIdentityCloudflarePrefixes(values []string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			panic("invalid pinned Cloudflare prefix: " + value)
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes
}

func riskIdentityLastForwardedHopIsCloudflare(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	values := c.Request.Header.Values("X-Forwarded-For")
	for valueIndex := len(values) - 1; valueIndex >= 0; valueIndex-- {
		candidates := strings.Split(values[valueIndex], ",")
		for candidateIndex := len(candidates) - 1; candidateIndex >= 0; candidateIndex-- {
			candidate := strings.TrimSpace(candidates[candidateIndex])
			if candidate == "" {
				continue
			}
			address, err := netip.ParseAddr(candidate)
			if err != nil {
				return false
			}
			for _, prefix := range riskIdentityCloudflarePrefixes {
				if prefix.Contains(address) {
					return true
				}
			}
			return false
		}
	}
	return false
}

func firstRiskIdentityHeader(c *gin.Context, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(c.GetHeader(name)); value != "" {
			return value
		}
	}
	return ""
}

func sameRiskIdentityAddress(left, right string) bool {
	leftAddress, leftErr := netip.ParseAddr(strings.TrimSpace(left))
	rightAddress, rightErr := netip.ParseAddr(strings.TrimSpace(right))
	return leftErr == nil && rightErr == nil && leftAddress.Unmap() == rightAddress.Unmap()
}

func validRiskIdentityCountryCode(value string) bool {
	if len(value) != 2 {
		return false
	}
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return value != "XX"
}

func newRiskIdentityEventRoot() string {
	if value, err := secureRandomRiskID(); err == nil {
		return service.HashRiskValue(value)
	}
	fallback := strconv.FormatInt(time.Now().UTC().UnixNano(), 10) + ":" + strconv.FormatUint(riskIdentityEventSequence.Add(1), 10)
	return service.HashRiskValue(fallback)
}

func enqueueRiskIdentity(c *gin.Context, client *service.RiskControlClient, eventType, eventClass, outcome, email string, userID, apiKeyID int64) {
	if client == nil || !client.IdentityEnabled() || c == nil {
		return
	}
	identity := requestRiskIdentity(c, client.IdentityDeviceEnabled() && apiKeyID <= 0)
	report := service.RiskIdentityReport{EventKey: identity.EventRoot + ":identity:" + eventType, EventType: eventType, EventClass: eventClass, Outcome: outcome, OccurredAt: time.Now().UTC(), UserID: userID, Email: strings.TrimSpace(email), ClientIP: identity.ClientIP, IPSource: identity.IPSource, ProxyChainValid: identity.ProxyChainValid, CountryCode: identity.CountryCode, Region: identity.Region, City: identity.City, ASN: identity.ASN, GeoSource: identity.GeoSource, GeoVerified: identity.GeoVerified, BrowserFamily: identity.BrowserFamily, OSFamily: identity.OSFamily, DeviceClass: identity.DeviceClass, LanguageFamily: identity.LanguageFamily, APIKeyID: apiKeyID}
	if !client.IdentityIPEnabled() {
		report.ClientIP, report.IPSource, report.ProxyChainValid = "", "", false
		report.CountryCode, report.Region, report.City, report.ASN, report.GeoSource, report.GeoVerified = "", "", "", 0, "", false
	}
	if !client.IdentityDeviceEnabled() {
		report.BrowserFamily, report.OSFamily, report.DeviceClass, report.LanguageFamily, report.APIKeyID = "", "", "", "", 0
	} else if apiKeyID <= 0 {
		report.BrowserInstanceID = identity.BrowserInstanceID
		report.BrowserCookieStatus = identity.BrowserCookieStatus
	}
	_ = client.EnqueueIdentity(report)
}

func RiskIdentityAuthLifecycleMiddleware(client *service.RiskControlClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		eventType, eventClass, ok := classifyRiskIdentityAuthStage(c.Request.Method, c.Request.URL.Path)
		if !ok || client == nil || !client.IdentityEnabled() {
			c.Next()
			return
		}
		requestRiskIdentity(c, client.IdentityDeviceEnabled())
		c.Next()
		outcome := "success"
		if riskIdentityAuthResponseFailed(c) {
			outcome = "failure"
		}
		enqueueRiskIdentity(c, client, eventType, eventClass, outcome, "", 0, 0)
	}
}

func riskIdentityAuthResponseFailed(c *gin.Context) bool {
	if c == nil || c.Writer.Status() >= http.StatusBadRequest || strings.TrimSpace(c.Query("error")) != "" {
		return true
	}
	if c.Writer.Status() < http.StatusMultipleChoices || c.Writer.Status() >= http.StatusBadRequest {
		return false
	}
	location := strings.TrimSpace(c.Writer.Header().Get("Location"))
	if location == "" {
		return false
	}
	redirect, err := url.Parse(location)
	if err != nil {
		return true
	}
	if strings.TrimSpace(redirect.Query().Get("error")) != "" {
		return true
	}
	fragment, err := url.ParseQuery(redirect.Fragment)
	return err != nil || strings.TrimSpace(fragment.Get("error")) != ""
}

func classifyRiskIdentityAuthStage(method, path string) (string, string, bool) {
	if method == http.MethodPost && (path == "/api/v1/auth/send-verify-code" || path == "/api/v1/auth/oauth/pending/send-verify-code") {
		return "verification_code", map[bool]string{true: "oauth", false: "registration"}[strings.Contains(path, "/oauth/")], true
	}
	if !strings.HasPrefix(path, "/api/v1/auth/oauth/") || strings.Contains(path, "/payment/") || strings.Contains(path, "/bind/") {
		return "", "", false
	}
	switch {
	case strings.HasSuffix(path, "/start") && (method == http.MethodGet || method == http.MethodPost):
		return "oauth_start", "oauth", true
	case strings.HasSuffix(path, "/callback") && method == http.MethodGet:
		return "oauth_callback", "oauth", true
	case (strings.HasSuffix(path, "/complete-registration") || strings.HasSuffix(path, "/create-account") || strings.HasSuffix(path, "/exchange")) && method == http.MethodPost:
		return "oauth_completion", "oauth", true
	default:
		return "", "", false
	}
}

func ensureSignedRiskDeviceCookie(c *gin.Context) (string, string) {
	if c == nil || c.Request == nil {
		return "", "missing"
	}
	key := []byte(strings.TrimSpace(os.Getenv("RISK_DEVICE_COOKIE_SIGNING_KEY")))
	keyID := strings.TrimSpace(os.Getenv("RISK_DEVICE_COOKIE_SIGNING_KEY_ID"))
	if keyID == "" {
		keyID = "current"
	}
	invalidCookie := false
	if len(key) >= 32 {
		if cookie, err := c.Request.Cookie(riskDeviceCookieName); err == nil {
			if value, ok := verifyRiskDeviceCookie(cookie.Value, key, keyID, time.Now().UTC()); ok {
				return value, "valid"
			}
			if previous := []byte(strings.TrimSpace(os.Getenv("RISK_DEVICE_COOKIE_SIGNING_PREVIOUS_KEY"))); len(previous) >= 32 {
				previousID := strings.TrimSpace(os.Getenv("RISK_DEVICE_COOKIE_SIGNING_PREVIOUS_KEY_ID"))
				if value, ok := verifyRiskDeviceCookie(cookie.Value, previous, previousID, time.Now().UTC()); ok {
					setSignedRiskDeviceCookie(c, value, key, keyID)
					return value, "rotated"
				}
			}
			invalidCookie = true
		}
	}
	value, err := secureRandomRiskID()
	if err != nil {
		return "", "unavailable"
	}
	if len(key) < 32 {
		return "", "unavailable"
	}
	setSignedRiskDeviceCookie(c, value, key, keyID)
	if invalidCookie {
		return value, "reset_invalid"
	}
	return value, "issued"
}

func setSignedRiskDeviceCookie(c *gin.Context, value string, key []byte, keyID string) {
	issued := time.Now().UTC().Unix()
	payload := fmtRiskCookiePayload(keyID, issued, value)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("risk-device-cookie-v2\n" + payload))
	cookieValue := payload + "." + hex.EncodeToString(mac.Sum(nil))
	http.SetCookie(c.Writer, &http.Cookie{Name: riskDeviceCookieName, Value: cookieValue, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: riskDeviceCookieMaxAge})
}
func fmtRiskCookiePayload(keyID string, issued int64, value string) string {
	return "v2." + base64.RawURLEncoding.EncodeToString([]byte(keyID)) + "." + strconv.FormatInt(issued, 10) + "." + value
}
func verifyRiskDeviceCookie(raw string, key []byte, expectedKeyID string, now time.Time) (string, bool) {
	if len(raw) > 320 {
		return "", false
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 5 || parts[0] != "v2" {
		return "", false
	}
	keyIDBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || string(keyIDBytes) != expectedKeyID {
		return "", false
	}
	issued, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || issued > now.Unix()+120 || now.Unix()-issued > riskDeviceCookieMaxAge {
		return "", false
	}
	value := parts[3]
	if len(value) != 36 || !isLowerHex(value) {
		return "", false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("risk-device-cookie-v2\n" + strings.Join(parts[:4], ".")))
	if len(parts[4]) != sha256.Size*2 || !isLowerHex(parts[4]) {
		return "", false
	}
	expected, err := hex.DecodeString(parts[4])
	if err != nil || !hmac.Equal(mac.Sum(nil), expected) {
		return "", false
	}
	return value, true
}
func secureRandomRiskID() (string, error) {
	var buf [18]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
func isLowerHex(value string) bool {
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}
func remoteAddress(raw string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(raw))
	if err == nil {
		return strings.TrimSpace(host)
	}
	return strings.TrimSpace(raw)
}
func boundedHeader(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) > max {
		return ""
	}
	return value
}
func envBoolRiskIdentity(key string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}
func coarseBrowserProfile(userAgent string) (string, string, string) {
	ua := strings.ToLower(userAgent)
	browser := "other"
	switch {
	case strings.Contains(ua, "edg/"):
		browser = "edge"
	case strings.Contains(ua, "chrome/"):
		browser = "chrome"
	case strings.Contains(ua, "firefox/"):
		browser = "firefox"
	case strings.Contains(ua, "safari/"):
		browser = "safari"
	}
	osFamily := "other"
	switch {
	case strings.Contains(ua, "windows"):
		osFamily = "windows"
	case strings.Contains(ua, "android"):
		osFamily = "android"
	case strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad"):
		osFamily = "ios"
	case strings.Contains(ua, "mac os"):
		osFamily = "macos"
	case strings.Contains(ua, "linux"):
		osFamily = "linux"
	}
	device := "desktop"
	if strings.Contains(ua, "mobile") {
		device = "mobile"
	}
	return browser, osFamily, device
}
