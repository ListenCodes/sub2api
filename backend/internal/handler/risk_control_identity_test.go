package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRiskDeviceCookieRejectsTampering(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest("GET", "https://example.test", nil)
	setSignedRiskDeviceCookie(c, "0123456789abcdef0123456789abcdef0123", key, "current")
	cookie := writer.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != riskDeviceCookieMaxAge {
		t.Fatalf("signed cookie security attributes are incomplete: %#v", cookie)
	}
	value, ok := verifyRiskDeviceCookie(cookie.Value, key, "current", time.Now().UTC())
	if !ok || value == "" {
		t.Fatal("valid signed cookie rejected")
	}
	replacement := "0"
	if cookie.Value[len(cookie.Value)-1] == '0' {
		replacement = "1"
	}
	cookie.Value = cookie.Value[:len(cookie.Value)-1] + replacement
	if _, ok := verifyRiskDeviceCookie(cookie.Value, key, "current", time.Now().UTC()); ok {
		t.Fatal("tampered signed cookie accepted")
	}
}

func TestRiskDeviceCookieRejectsNonCanonicalSignature(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest("GET", "https://example.test", nil)
	setSignedRiskDeviceCookie(c, "0123456789abcdef0123456789abcdef0123", key, "current")
	cookie := writer.Result().Cookies()[0]
	parts := strings.Split(cookie.Value, ".")
	if index := strings.IndexAny(parts[4], "abcdef"); index >= 0 {
		parts[4] = parts[4][:index] + strings.ToUpper(parts[4][index:index+1]) + parts[4][index+1:]
	} else {
		parts[4] += "00"
	}
	cookie.Value = strings.Join(parts, ".")
	if _, ok := verifyRiskDeviceCookie(cookie.Value, key, "current", time.Now().UTC()); ok {
		t.Fatal("non-canonical signed cookie accepted")
	}
}

func TestRiskDeviceCookieResetsInvalidValueWithoutTrustingIt(t *testing.T) {
	t.Setenv("RISK_DEVICE_COOKIE_SIGNING_KEY", "01234567890123456789012345678901")
	t.Setenv("RISK_DEVICE_COOKIE_SIGNING_KEY_ID", "current")
	t.Setenv("RISK_DEVICE_COOKIE_SIGNING_PREVIOUS_KEY", "")
	t.Setenv("RISK_DEVICE_COOKIE_SIGNING_PREVIOUS_KEY_ID", "")

	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "https://example.test/api/v1/auth/register", nil)
	c.Request.AddCookie(&http.Cookie{Name: riskDeviceCookieName, Value: "attacker-controlled"})

	value, status := ensureSignedRiskDeviceCookie(c)
	if value == "" || status != "reset_invalid" {
		t.Fatalf("invalid cookie reset = %q/%q", value, status)
	}
	if len(writer.Result().Cookies()) != 1 {
		t.Fatalf("replacement cookie count = %d", len(writer.Result().Cookies()))
	}
}

func TestRiskIdentityEventRootIgnoresRepeatedClientRequestID(t *testing.T) {
	first, _ := gin.CreateTestContext(httptest.NewRecorder())
	first.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", nil)
	first.Request.Header.Set("X-Request-ID", "attacker-controlled")
	second, _ := gin.CreateTestContext(httptest.NewRecorder())
	second.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", nil)
	second.Request.Header.Set("X-Request-ID", "attacker-controlled")

	firstRoot := requestRiskIdentity(first, false).EventRoot
	secondRoot := requestRiskIdentity(second, false).EventRoot
	if firstRoot == "" || secondRoot == "" || firstRoot == secondRoot {
		t.Fatalf("event roots must be server-generated per request: %q %q", firstRoot, secondRoot)
	}
}

func TestRiskIdentitySourceUsesContractValues(t *testing.T) {
	direct, _ := gin.CreateTestContext(httptest.NewRecorder())
	direct.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	direct.Request.RemoteAddr = "203.0.113.10:443"
	if got := requestRiskIdentity(direct, false).IPSource; got != "remote_addr" {
		t.Fatalf("direct source = %q", got)
	}

	proxied, _ := gin.CreateTestContext(httptest.NewRecorder())
	proxied.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	proxied.Request.RemoteAddr = "192.0.2.2:443"
	proxied.Request.Header.Set("X-Forwarded-For", "203.0.113.10")
	proxied.Request.Header.Set("X-Real-IP", "203.0.113.10")
	proxied.Request.Header.Set("CF-Connecting-IP", "203.0.113.10")
	engine := gin.New()
	if err := engine.SetTrustedProxies([]string{"192.0.2.2"}); err != nil {
		t.Fatal(err)
	}
	engine.GET("/", func(c *gin.Context) {
		identity := requestRiskIdentity(c, false)
		if identity.IPSource != "trusted_real_ip" {
			t.Errorf("proxy source = %q", identity.IPSource)
		}
	})
	engine.ServeHTTP(proxied.Writer, proxied.Request)
}

func TestRiskIdentityAcceptsGeoOnlyFromPinnedCloudflareLastHop(t *testing.T) {
	t.Setenv("RISK_IDENTITY_TRUST_CLOUDFLARE_HEADERS", "true")
	engine := gin.New()
	if err := engine.SetTrustedProxies([]string{"192.0.2.2", "173.245.48.0/20"}); err != nil {
		t.Fatal(err)
	}
	engine.GET("/", func(c *gin.Context) {
		identity := requestRiskIdentity(c, false)
		if identity.IPSource != "cf_connecting_ip" || !identity.GeoVerified || identity.GeoSource != "cloudflare_verified" {
			t.Fatalf("verified Cloudflare identity rejected: %#v", identity)
		}
		if identity.CountryCode != "CN" || identity.Region != "Hubei" || identity.City != "Wuhan" || identity.ASN != 4134 {
			t.Fatalf("Cloudflare geo mismatch: %#v", identity)
		}
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.2:443"
	request.Header.Set("X-Forwarded-For", "203.0.113.10, 173.245.48.10")
	request.Header.Set("CF-Connecting-IP", "203.0.113.10")
	request.Header.Set("X-Risk-CF-Country", "CN")
	request.Header.Set("X-Risk-CF-Region", "Hubei")
	request.Header.Set("X-Risk-CF-City", "Wuhan")
	request.Header.Set("X-Risk-CF-ASN", "4134")
	engine.ServeHTTP(httptest.NewRecorder(), request)
}

func TestRiskIdentityRejectsSpoofedGeoFromDirectOriginTraffic(t *testing.T) {
	t.Setenv("RISK_IDENTITY_TRUST_CLOUDFLARE_HEADERS", "true")
	engine := gin.New()
	if err := engine.SetTrustedProxies([]string{"192.0.2.2", "173.245.48.0/20"}); err != nil {
		t.Fatal(err)
	}
	engine.GET("/", func(c *gin.Context) {
		identity := requestRiskIdentity(c, false)
		if identity.IPSource == "cf_connecting_ip" || identity.GeoVerified || identity.CountryCode != "" || identity.ASN != 0 {
			t.Fatalf("direct-origin geo spoof accepted: %#v", identity)
		}
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.2:443"
	request.Header.Set("X-Forwarded-For", "198.51.100.24")
	request.Header.Set("CF-Connecting-IP", "198.51.100.24")
	request.Header.Set("X-Risk-CF-Country", "US")
	request.Header.Set("X-Risk-CF-ASN", "64512")
	engine.ServeHTTP(httptest.NewRecorder(), request)
}

func TestRiskIdentityIgnoresCloudflareGeoWhileTrustSwitchIsDisabled(t *testing.T) {
	t.Setenv("RISK_IDENTITY_TRUST_CLOUDFLARE_HEADERS", "false")
	engine := gin.New()
	if err := engine.SetTrustedProxies([]string{"192.0.2.2", "173.245.48.0/20"}); err != nil {
		t.Fatal(err)
	}
	engine.GET("/", func(c *gin.Context) {
		identity := requestRiskIdentity(c, false)
		if identity.GeoVerified || identity.CountryCode != "" || identity.ASN != 0 {
			t.Fatalf("disabled Cloudflare geo trust accepted metadata: %#v", identity)
		}
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.2:443"
	request.Header.Set("X-Forwarded-For", "203.0.113.10, 173.245.48.10")
	request.Header.Set("CF-Connecting-IP", "203.0.113.10")
	request.Header.Set("X-Risk-CF-Country", "CN")
	request.Header.Set("X-Risk-CF-ASN", "4134")
	engine.ServeHTTP(httptest.NewRecorder(), request)
}

func TestRiskIdentityCloudflareGeoValidationRejectsInvalidValues(t *testing.T) {
	for _, code := range []string{"", "X", "123", "C1", "XX"} {
		if validRiskIdentityCountryCode(code) {
			t.Fatalf("invalid country code accepted: %q", code)
		}
	}
	for _, code := range []string{"CN", "US"} {
		if !validRiskIdentityCountryCode(code) {
			t.Fatalf("valid country code rejected: %q", code)
		}
	}
	if !sameRiskIdentityAddress("2606:4700:4700::1111", "2606:4700:4700:0:0:0:0:1111") {
		t.Fatal("equivalent IPv6 addresses did not match")
	}
}

func TestCoarseBrowserProfileDoesNotProduceInvasiveFingerprint(t *testing.T) {
	browser, osFamily, device := coarseBrowserProfile("Mozilla/5.0 (Windows NT 10.0) AppleWebKit Chrome/130.0 Safari/537.36")
	if browser != "chrome" || osFamily != "windows" || device != "desktop" {
		t.Fatalf("profile = %s/%s/%s", browser, osFamily, device)
	}
}

func TestRiskIdentityAuthStageScope(t *testing.T) {
	tests := []struct {
		method, path, eventClass, eventType string
		want                                bool
	}{
		{http.MethodPost, "/api/v1/auth/send-verify-code", "registration", "verification_code", true},
		{http.MethodPost, "/api/v1/auth/oauth/pending/send-verify-code", "oauth", "verification_code", true},
		{http.MethodGet, "/api/v1/auth/oauth/linuxdo/start", "oauth", "oauth_start", true},
		{http.MethodGet, "/api/v1/auth/oauth/wechat/callback", "oauth", "oauth_callback", true},
		{http.MethodPost, "/api/v1/auth/oauth/oidc/complete-registration", "oauth", "oauth_completion", true},
		{http.MethodGet, "/api/v1/auth/oauth/wechat/payment/start", "", "", false},
		{http.MethodGet, "/api/v1/auth/oauth/linuxdo/bind/start", "", "", false},
		{http.MethodPost, "/api/v1/auth/login", "", "", false},
	}
	for _, test := range tests {
		eventType, eventClass, ok := classifyRiskIdentityAuthStage(test.method, test.path)
		if ok != test.want || eventType != test.eventType || eventClass != test.eventClass {
			t.Fatalf("stage %s %s = %q/%q/%v", test.method, test.path, eventType, eventClass, ok)
		}
	}
}
