package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	neturl "net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/google/uuid"
)

// ─── VK Credential Sets (2 stable app_id with rotating fallback) ───

type VKCredentials struct {
	ClientID     string
	ClientSecret string
}

var vkCredentialsList = []VKCredentials{
	{ClientID: "6287487", ClientSecret: "MuAxFaKDYDOICzGnEOhp"},
	{ClientID: "8202606", ClientSecret: "lMRsTiMCyPnp5vfoldmn"},
}

const (
	vkConnectClientID     = "8093730"
	vkCallsAPIHost        = "api.vk.me"
	vkCallsAnonAPIVersion = "5.276"
)

var vkCallsProfile = Profile{
	UserAgent:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
	SecChUa:         `"Chromium";v="146", "Not-A.Brand";v="24", "Google Chrome";v="146"`,
	SecChUaMobile:   "?0",
	SecChUaPlatform: `"Windows"`,
}

type vkCallsFailureKind string

const (
	vkCallsFailureSkipped vkCallsFailureKind = "skipped"
	vkCallsFailureSetup   vkCallsFailureKind = "setup"
	vkCallsFailureNetwork vkCallsFailureKind = "network"
	vkCallsFailureDecode  vkCallsFailureKind = "decode"
	vkCallsFailureVKAPI   vkCallsFailureKind = "vk_api"
	vkCallsFailureCaptcha vkCallsFailureKind = "captcha"
	vkCallsFailureCall    vkCallsFailureKind = "call_unavailable"
	vkCallsFailureOKCDN   vkCallsFailureKind = "okcdn_api"
	vkCallsFailureParse   vkCallsFailureKind = "parse"
)

type vkCallsFailure struct {
	Step string
	Kind vkCallsFailureKind
	Err  error
}

func (e *vkCallsFailure) Error() string {
	if e == nil {
		return "vkcalls failure"
	}
	if e.Err == nil {
		return fmt.Sprintf("step=%s kind=%s", e.Step, e.Kind)
	}
	return fmt.Sprintf("step=%s kind=%s: %v", e.Step, e.Kind, e.Err)
}

func (e *vkCallsFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newVKCallsFailure(step string, kind vkCallsFailureKind, err error) error {
	if err == nil {
		err = fmt.Errorf("unknown error")
	}
	return &vkCallsFailure{Step: step, Kind: kind, Err: err}
}

func describeVKCallsFailure(err error) string {
	if err == nil {
		return ""
	}
	var failure *vkCallsFailure
	if errors.As(err, &failure) {
		return failure.Error()
	}
	return err.Error()
}

func vkCallsAPIErrorKind(err error) vkCallsFailureKind {
	if _, ok := asCallUnavailableError(err); ok {
		return vkCallsFailureCall
	}
	var captchaErr *VkCaptchaError
	if errors.As(err, &captchaErr) {
		return vkCallsFailureCaptcha
	}
	return vkCallsFailureVKAPI
}

type vkCallsVKAPIError struct {
	Code    int
	Message string
}

func (e *vkCallsVKAPIError) Error() string {
	if e == nil {
		return "VK API error"
	}
	if e.Message == "" {
		return fmt.Sprintf("error_code=%d", e.Code)
	}
	return fmt.Sprintf("error_code=%d %s", e.Code, e.Message)
}

type vkCallsOKAPIError struct {
	Code    int
	Message string
}

func (e *vkCallsOKAPIError) Error() string {
	if e == nil {
		return "OK CDN API error"
	}
	if e.Message == "" {
		return fmt.Sprintf("error_code=%d", e.Code)
	}
	return fmt.Sprintf("error_code=%d %s", e.Code, e.Message)
}

type CallUnavailableError struct {
	Code    int
	Message string
}

func (e *CallUnavailableError) Error() string {
	if e == nil {
		return "VK call is unavailable"
	}
	if e.Message != "" {
		return fmt.Sprintf("VK returns error: %s (error_code=%d)", e.Message, e.Code)
	}
	return fmt.Sprintf("VK call is unavailable (error_code=%d)", e.Code)
}

func asCallUnavailableError(err error) (*CallUnavailableError, bool) {
	var callErr *CallUnavailableError
	if errors.As(err, &callErr) {
		return callErr, true
	}
	return nil, false
}

func fatalCallError(resp map[string]interface{}) *CallUnavailableError {
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		return nil
	}

	code := vkErrorCode(errObj["error_code"])
	switch {
	case code == 951, code == 954:
		// VKCalls messages.*: call not found / invalid join link.
	case code >= 9000 && code <= 9999:
		// Legacy calls.getAnonymousToken call-domain errors.
	default:
		return nil
	}

	msg, _ := errObj["error_msg"].(string)
	return &CallUnavailableError{Code: code, Message: msg}
}

func vkErrorCode(raw interface{}) int {
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}

const vkCredentialAttemptLimit = 4

// ─── VK auth mode & anon path (configurable, defaults to anonymous/vkcalls) ───

var (
	vkAuthModeValue     atomic.Value
	vkAnonPathValue     atomic.Value
)

func init() {
	vkAuthModeValue.Store("anonymous")
	vkAnonPathValue.Store("vkcalls")
}

func getVkAuthMode() string {
	mode, _ := vkAuthModeValue.Load().(string)
	if mode == "" {
		return "anonymous"
	}
	return mode
}

func SetVkAuthMode(mode string) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "anonymous" {
		mode = "account"
	}
	vkAuthModeValue.Store(mode)
}

func getVkAnonPath() string {
	path, _ := vkAnonPathValue.Load().(string)
	if path == "" {
		return "vkcalls"
	}
	return path
}

func SetVkAnonPath(path string) {
	path = strings.ToLower(strings.TrimSpace(path))
	if path == "legacy" {
		vkAnonPathValue.Store("legacy")
		return
	}
	vkAnonPathValue.Store("vkcalls")
}

// ─── Credential Caching ───

type TurnCredentials struct {
	Username    string
	Password    string
	ServerAddrs []string
	ExpiresAt   time.Time
	Link        string
}

type StreamCredentialsCache struct {
	creds         TurnCredentials
	mutex         sync.RWMutex
	errorCount    atomic.Int32
	lastErrorTime atomic.Int64
}

const (
	credentialLifetime = 10 * time.Minute
	cacheSafetyMargin  = 60 * time.Second
	maxCacheErrors     = 3
	errorWindow        = 10 * time.Second
)

var streamsPerCache = 10

func getCacheID(streamID int) int {
	return streamID / streamsPerCache
}

var credentialsStore = struct {
	mu     sync.RWMutex
	caches map[int]*StreamCredentialsCache
}{
	caches: make(map[int]*StreamCredentialsCache),
}

func getStreamCache(streamID int) *StreamCredentialsCache {
	cacheID := getCacheID(streamID)

	credentialsStore.mu.RLock()
	cache, exists := credentialsStore.caches[cacheID]
	credentialsStore.mu.RUnlock()

	if exists {
		return cache
	}

	credentialsStore.mu.Lock()
	defer credentialsStore.mu.Unlock()

	if cache, exists = credentialsStore.caches[cacheID]; exists {
		return cache
	}

	cache = &StreamCredentialsCache{}
	credentialsStore.caches[cacheID] = cache
	return cache
}

func (c *StreamCredentialsCache) invalidate(streamID int) {
	c.mutex.Lock()
	c.creds = TurnCredentials{}
	c.mutex.Unlock()

	c.errorCount.Store(0)
	c.lastErrorTime.Store(0)

	log.Printf("[STREAM %d] [VK Auth] Credentials cache invalidated", streamID)
}

func cloneStringSlice(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "401") ||
		strings.Contains(errStr, "Unauthorized") ||
		strings.Contains(errStr, "authentication") ||
		strings.Contains(errStr, "invalid credential") ||
		strings.Contains(errStr, "stale nonce")
}

func handleAuthError(streamID int) bool {
	cache := getStreamCache(streamID)
	cacheID := getCacheID(streamID)

	now := time.Now().Unix()

	if now-cache.lastErrorTime.Load() > int64(errorWindow.Seconds()) {
		cache.errorCount.Store(0)
	}

	count := cache.errorCount.Add(1)
	cache.lastErrorTime.Store(now)

	log.Printf("[STREAM %d] Auth error (cache=%d, count=%d/%d)", streamID, cacheID, count, maxCacheErrors)

	if count >= maxCacheErrors {
		log.Printf("[VK Auth] Multiple auth errors detected (%d), invalidating cache %d", count, cacheID)
		cache.invalidate(streamID)
		return true
	}
	return false
}

// ─── Captcha lockout ───

var globalCaptchaLockout atomic.Int64

// ─── Random delay ───

func vkDelayRandom(minMs, maxMs int) {
	ms := minMs + rand.Intn(maxMs-minMs+1)
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// ─── Cached credential fetcher ───

func getVkCredsCached(ctx context.Context, link string, streamID int, deviceID string, captchaResultChan chan string, getCaptchaMode func() string, emitCaptchaRequest func(mode, redirectURI, sessionToken string)) (string, string, []string, error) {
	cache := getStreamCache(streamID)
	cacheID := getCacheID(streamID)

	cache.mutex.RLock()
	if cache.creds.Link == link && time.Now().Before(cache.creds.ExpiresAt) && len(cache.creds.ServerAddrs) > 0 {
		expires := time.Until(cache.creds.ExpiresAt)
		u, p := cache.creds.Username, cache.creds.Password
		addr := cache.creds.ServerAddrs[streamID%len(cache.creds.ServerAddrs)]
		addrs := cloneStringSlice(cache.creds.ServerAddrs)
		cache.mutex.RUnlock()
		log.Printf("[STREAM %d] [VK Auth] Using cached credentials (cache=%d, expires in %v, selected=%s, urls=%d)", streamID, cacheID, expires.Truncate(time.Second), addr, len(addrs))
		return u, p, addrs, nil
	}
	cache.mutex.RUnlock()

	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	// Double-check inside lock
	if cache.creds.Link == link && time.Now().Before(cache.creds.ExpiresAt) && len(cache.creds.ServerAddrs) > 0 {
		return cache.creds.Username, cache.creds.Password, cloneStringSlice(cache.creds.ServerAddrs), nil
	}

	user, pass, addrs, err := fetchVkCredsSerialized(ctx, link, streamID, deviceID, captchaResultChan, getCaptchaMode, emitCaptchaRequest)
	if err != nil {
		return "", "", nil, err
	}

	cache.creds = TurnCredentials{
		Username:    user,
		Password:    pass,
		ServerAddrs: addrs,
		ExpiresAt:   time.Now().Add(credentialLifetime - cacheSafetyMargin),
		Link:        link,
	}
	return user, pass, cloneStringSlice(addrs), nil
}

// ─── Serialized (throttled) fetcher ───

var (
	vkRequestMu           sync.Mutex
	globalLastVkFetchTime time.Time
)

func fetchVkCredsSerialized(ctx context.Context, link string, streamID int, deviceID string, captchaResultChan chan string, getCaptchaMode func() string, emitCaptchaRequest func(mode, redirectURI, sessionToken string)) (string, string, []string, error) {
	vkRequestMu.Lock()
	defer vkRequestMu.Unlock()

	// Throttle: 3-6 seconds between requests
	minInterval := 3*time.Second + time.Duration(rand.Intn(3000))*time.Millisecond
	elapsed := time.Since(globalLastVkFetchTime)

	if !globalLastVkFetchTime.IsZero() && elapsed < minInterval {
		wait := minInterval - elapsed
		log.Printf("[STREAM %d] [VK Auth] Throttling: waiting %v to prevent rate limit...", streamID, wait.Truncate(time.Millisecond))
		select {
		case <-ctx.Done():
			return "", "", nil, ctx.Err()
		case <-time.After(wait):
		}
	}

	defer func() {
		globalLastVkFetchTime = time.Now()
	}()

	return fetchVkCreds(ctx, link, streamID, deviceID, captchaResultChan, getCaptchaMode, emitCaptchaRequest)
}

// ─── Main credential fetcher (rotates through stable credential sets) ───

func fetchVkCreds(ctx context.Context, link string, streamID int, deviceID string, captchaResultChan chan string, getCaptchaMode func() string, emitCaptchaRequest func(mode, redirectURI, sessionToken string)) (string, string, []string, error) {
	if time.Now().Unix() < globalCaptchaLockout.Load() {
		return "", "", nil, fmt.Errorf("CAPTCHA_WAIT_REQUIRED: global lockout active")
	}

	if getVkAnonPath() == "vkcalls" {
		log.Printf("[STREAM %d] [VKCalls] VK anon path: vkcalls", streamID)
		if user, pass, addrs, err := getVKCredsViaVKCallsPath(ctx, link, streamID); err == nil {
			log.Printf("[STREAM %d] [VK Auth] Success via VK Calls path", streamID)
			return user, pass, addrs, nil
		} else {
			if callErr, ok := asCallUnavailableError(err); ok {
				log.Printf("[STREAM %d] [VK Auth] VK Calls path returned non-retryable call error: %v", streamID, callErr)
				return "", "", nil, callErr
			}
			log.Printf("[STREAM %d] [VK Auth] VK Calls path failed (%s), falling back to legacy", streamID, describeVKCallsFailure(err))
		}
	} else {
		log.Printf("[STREAM %d] [VKCalls] VK anon path: legacy (skipping VK Calls)", streamID)
	}

	var lastErr error
	jar := tlsclient.NewCookieJar()

	for attempt := 0; attempt < vkCredentialAttemptLimit; attempt++ {
		creds := vkCredentialsList[attempt%len(vkCredentialsList)]
		log.Printf("[STREAM %d] [VK Auth] Trying credentials: client_id=%s (attempt %d/%d)", streamID, creds.ClientID, attempt+1, vkCredentialAttemptLimit)

		user, pass, addrs, err := getTokenChain(ctx, link, streamID, deviceID, creds, jar, captchaResultChan, getCaptchaMode, emitCaptchaRequest)

		if err == nil {
			log.Printf("[STREAM %d] [VK Auth] Success with client_id=%s", streamID, creds.ClientID)
			return user, pass, addrs, nil
		}

		lastErr = err
		log.Printf("[STREAM %d] [VK Auth] Failed with client_id=%s: %v", streamID, creds.ClientID, err)

		if callErr, ok := asCallUnavailableError(err); ok {
			return "", "", nil, callErr
		}

		if strings.Contains(err.Error(), "CAPTCHA_WAIT_REQUIRED") || strings.Contains(err.Error(), "FATAL_CAPTCHA") {
			return "", "", nil, err
		}

		if strings.Contains(err.Error(), "error_code:29") || strings.Contains(err.Error(), "error_code: 29") || strings.Contains(err.Error(), "Rate limit") {
			log.Printf("[STREAM %d] [VK Auth] Rate limit detected, trying next credentials...", streamID)
		}

		if attempt%len(vkCredentialsList) == len(vkCredentialsList)-1 && attempt+1 < vkCredentialAttemptLimit {
			wait := time.Duration(900+rand.Intn(900)) * time.Millisecond
			log.Printf("[STREAM %d] [VK Auth] Both VK credentials failed, retrying stable pair after %v...", streamID, wait)
			select {
			case <-ctx.Done():
				return "", "", nil, ctx.Err()
			case <-time.After(wait):
			}
		}
	}

	return "", "", nil, fmt.Errorf("all VK credentials failed: %w", lastErr)
}

// ─── VK Calls Path ───

func getVKCredsViaVKCallsPath(ctx context.Context, link string, streamID int) (string, string, []string, error) {
	if os.Getenv("VK_SKIP_VKCALLS") == "1" {
		return "", "", nil, newVKCallsFailure("preflight", vkCallsFailureSkipped, fmt.Errorf("disabled by VK_SKIP_VKCALLS=1"))
	}

	vkDeviceID := uuid.New().String()
	name := generateName()
	profile := vkCallsProfile
	linkURL := neturl.QueryEscape("https://vk.com/call/join/" + link)
	nameEnc := neturl.QueryEscape(name)

	client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(),
		tlsclient.WithTimeoutSeconds(20),
		tlsclient.WithClientProfile(profiles.Chrome_146),
		tlsclient.WithCookieJar(tlsclient.NewCookieJar()),
	)
	if err != nil {
		return "", "", nil, newVKCallsFailure("setup", vkCallsFailureSetup, fmt.Errorf("create tls client: %w", err))
	}

	log.Printf("[STREAM %d] [VKCalls] Identity - Name: %s | device_id=%s | TLS=Chrome_146 | UA: %s", streamID, name, vkDeviceID, profile.UserAgent)

	doRequest := func(step string, url string) (map[string]interface{}, error) {
		req, err := fhttp.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(nil))
		if err != nil {
			return nil, newVKCallsFailure(step, vkCallsFailureSetup, fmt.Errorf("create request: %w", err))
		}
		req.Header.Set("User-Agent", profile.UserAgent)
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
		req.Header.Set("Accept-Language", "en-GB,en;q=0.9")

		httpResp, err := client.Do(req)
		if err != nil {
			return nil, newVKCallsFailure(step, vkCallsFailureNetwork, fmt.Errorf("request failed: %w", err))
		}
		defer func() {
			if closeErr := httpResp.Body.Close(); closeErr != nil {
				log.Printf("close response body: %s", closeErr)
			}
		}()

		body, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, newVKCallsFailure(step, vkCallsFailureNetwork, fmt.Errorf("read response: %w", err))
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, newVKCallsFailure(step, vkCallsFailureDecode, fmt.Errorf("unmarshal JSON: %w, body: %s", err, truncateVKCallsLog(string(body), 200)))
		}
		return resp, nil
	}

	// Step 1: auth.getAnonymToken
	step1 := "step1 auth.getAnonymToken"
	step1URL := fmt.Sprintf(
		"https://%s/method/auth.getAnonymToken?v=%s&client_id=%s&link=%s&device_id=%s&anonymName=%s&lang=en",
		vkCallsAPIHost, vkCallsAnonAPIVersion, vkConnectClientID,
		linkURL, vkDeviceID, nameEnc,
	)
	resp1, err := doRequest(step1, step1URL)
	if err != nil {
		return "", "", nil, err
	}
	anonymToken, err := extractVKCallsStr(resp1, "response", "token")
	if err != nil {
		return "", "", nil, newVKCallsFailure(step1, vkCallsFailureParse, fmt.Errorf("parse token: %w (resp: %s)", err, truncateVKCallsResp(resp1)))
	}
	anonymTokenEnc := neturl.QueryEscape(anonymToken)
	log.Printf("[STREAM %d] [VKCalls] step1 OK, anonymous_token (%d chars)", streamID, len(anonymToken))

	// Step 2: messages.getCallPreview
	step2 := "step2 messages.getCallPreview"
	step2URL := fmt.Sprintf(
		"https://%s/method/messages.getCallPreview?v=%s&anonymous_token=%s&device_id=%s&extended=1&fields=first_name,last_name,photo_200&lang=en&link=%s",
		vkCallsAPIHost, vkCallsAnonAPIVersion, anonymTokenEnc, vkDeviceID, linkURL,
	)
	resp2, err := doRequest(step2, step2URL)
	if err != nil {
		return "", "", nil, err
	}
	if apiErr := vkCallsAPIError(resp2); apiErr != nil {
		if captchaErr, ok := apiErr.(*VkCaptchaError); ok {
			log.Printf("[STREAM %d] [VKCalls] step2 captcha gate appeared (sid=%q, redirect_uri=%t)", streamID, captchaErr.CaptchaSid, captchaErr.RedirectURI != "")
		} else if callErr, ok := asCallUnavailableError(apiErr); ok {
			log.Printf("[STREAM %d] [VKCalls] step2 non-retryable call error: %v", streamID, callErr)
		}
		return "", "", nil, newVKCallsFailure(step2, vkCallsAPIErrorKind(apiErr), apiErr)
	}
	userIDFloat, err := extractVKCallsFloat(resp2, "response", "user_id")
	if err != nil {
		return "", "", nil, newVKCallsFailure(step2, vkCallsFailureParse, fmt.Errorf("parse user_id: %w (resp: %s)", err, truncateVKCallsResp(resp2)))
	}
	userIDStr := fmt.Sprintf("%.0f", userIDFloat)
	secret, err := extractVKCallsStr(resp2, "response", "secret")
	if err != nil {
		return "", "", nil, newVKCallsFailure(step2, vkCallsFailureParse, fmt.Errorf("parse secret: %w", err))
	}
	log.Printf("[STREAM %d] [VKCalls] step2 OK, user_id=%s, secret (%d chars)", streamID, userIDStr, len(secret))

	// Step 3: messages.getAnonymCallToken
	step3 := "step3 messages.getAnonymCallToken"
	step3URL := fmt.Sprintf(
		"https://%s/method/messages.getAnonymCallToken?v=%s&anonymous_token=%s&device_id=%s&link=%s&name=%s&user_id=%s&secret=%s&lang=en",
		vkCallsAPIHost, vkCallsAnonAPIVersion, anonymTokenEnc, vkDeviceID, linkURL,
		nameEnc, userIDStr, neturl.QueryEscape(secret),
	)
	resp3, err := doRequest(step3, step3URL)
	if err != nil {
		return "", "", nil, err
	}
	if apiErr := vkCallsAPIError(resp3); apiErr != nil {
		if captchaErr, ok := apiErr.(*VkCaptchaError); ok {
			log.Printf("[STREAM %d] [VKCalls] step3 captcha gate appeared (sid=%q, redirect_uri=%t)", streamID, captchaErr.CaptchaSid, captchaErr.RedirectURI != "")
		} else if callErr, ok := asCallUnavailableError(apiErr); ok {
			log.Printf("[STREAM %d] [VKCalls] step3 non-retryable call error: %v", streamID, callErr)
		}
		return "", "", nil, newVKCallsFailure(step3, vkCallsAPIErrorKind(apiErr), apiErr)
	}
	okAnonymToken, err := extractVKCallsStr(resp3, "response", "token")
	if err != nil {
		return "", "", nil, newVKCallsFailure(step3, vkCallsFailureParse, fmt.Errorf("parse token: %w (resp: %s)", err, truncateVKCallsResp(resp3)))
	}
	log.Printf("[STREAM %d] [VKCalls] step3 OK, OK anonymToken (%d chars)", streamID, len(okAnonymToken))

	// Step 4: auth.anonymLogin (OK.ru)
	okDeviceID := uuid.New().String()
	step4 := "step4 auth.anonymLogin"
	step4URL := "https://calls.okcdn.ru/fb.do?session_data=" +
		neturl.QueryEscape(fmt.Sprintf(
			`{"version":2,"device_id":"%s","client_version":"1.0.1"}`, okDeviceID,
		)) +
		"&method=auth.anonymLogin&format=JSON&application_key=CGMMEJLGDIHBABABA"
	resp4, err := doRequest(step4, step4URL)
	if err != nil {
		return "", "", nil, err
	}
	sessionKey, err := extractVKCallsStr(resp4, "session_key")
	if err != nil {
		return "", "", nil, newVKCallsFailure(step4, vkCallsFailureParse, fmt.Errorf("parse session_key: %w (resp: %s)", err, truncateVKCallsResp(resp4)))
	}
	log.Printf("[STREAM %d] [VKCalls] step4 OK, OK session_key (%d chars)", streamID, len(sessionKey))

	// Step 5: joinConversationByLink → TURN creds
	step5 := "step5 vchat.joinConversationByLink"
	step5URL := fmt.Sprintf(
		"https://calls.okcdn.ru/fb.do?joinLink=%s&isVideo=false&protocolVersion=5&anonymToken=%s&method=vchat.joinConversationByLink&format=JSON&application_key=CGMMEJLGDIHBABABA&session_key=%s",
		link, okAnonymToken, sessionKey,
	)
	resp5, err := doRequest(step5, step5URL)
	if err != nil {
		return "", "", nil, err
	}
	if okErr := vkCallsOKError(resp5); okErr != nil {
		return "", "", nil, newVKCallsFailure(step5, vkCallsFailureOKCDN, fmt.Errorf("%w (resp: %s)", okErr, truncateVKCallsResp(resp5)))
	}

	user, err := extractVKCallsStr(resp5, "turn_server", "username")
	if err != nil {
		return "", "", nil, newVKCallsFailure(step5, vkCallsFailureParse, fmt.Errorf("parse username: %w (resp: %s)", err, truncateVKCallsResp(resp5)))
	}
	pass, err := extractVKCallsStr(resp5, "turn_server", "credential")
	if err != nil {
		return "", "", nil, newVKCallsFailure(step5, vkCallsFailureParse, fmt.Errorf("parse credential: %w", err))
	}
	addrs := parseVKCallsTURNAddresses(resp5)
	if len(addrs) == 0 {
		return "", "", nil, newVKCallsFailure(step5, vkCallsFailureParse, fmt.Errorf("turn_server.urls empty"))
	}

	log.Printf("[STREAM %d] [VKCalls] SUCCESS, TURN urls=%d", streamID, len(addrs))
	return user, pass, addrs, nil
}

func extractVKCallsStr(resp map[string]interface{}, keys ...string) (string, error) {
	var cur interface{} = resp
	for _, k := range keys {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("expected map at key %q, got %T", k, cur)
		}
		cur = m[k]
	}
	s, ok := cur.(string)
	if !ok {
		return "", fmt.Errorf("expected string at end of path, got %T", cur)
	}
	return s, nil
}

func extractVKCallsFloat(resp map[string]interface{}, keys ...string) (float64, error) {
	var cur interface{} = resp
	for _, k := range keys {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return 0, fmt.Errorf("expected map at key %q, got %T", k, cur)
		}
		cur = m[k]
	}
	f, ok := cur.(float64)
	if !ok {
		return 0, fmt.Errorf("expected float64 at end of path, got %T", cur)
	}
	return f, nil
}

func parseVKCallsTURNAddresses(resp map[string]interface{}) []string {
	turnServer, ok := resp["turn_server"].(map[string]interface{})
	if !ok {
		return nil
	}
	urls, ok := turnServer["urls"].([]interface{})
	if !ok {
		return nil
	}
	var addrs []string
	for i, u := range urls {
		s, ok := u.(string)
		if !ok {
			log.Printf("[VKCalls] turn_server.urls[%d]=<non-string %T>, skipping", i, u)
			continue
		}
		clean := strings.Split(s, "?")[0]
		addr := strings.TrimPrefix(strings.TrimPrefix(clean, "turn:"), "turns:")
		log.Printf("[VKCalls] turn_server.urls[%d]=%s", i, addr)
		addrs = append(addrs, addr)
	}
	return addrs
}

func vkCallsAPIError(resp map[string]interface{}) error {
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		return nil
	}
	code, _ := errObj["error_code"].(float64)
	msg, _ := errObj["error_msg"].(string)
	if code == 0 && msg == "" {
		return nil
	}
	if callErr := fatalCallError(resp); callErr != nil {
		return callErr
	}
	if int(code) == 14 {
		if errJSON, err := json.Marshal(errObj); err == nil {
			log.Printf("[VKCalls] captcha error response: %s", truncateVKCallsLog(string(errJSON), 300))
		}
		return parseVkCaptchaError(errObj)
	}
	return &vkCallsVKAPIError{Code: int(code), Message: msg}
}

func vkCallsOKError(resp map[string]interface{}) error {
	code, ok := resp["error_code"].(float64)
	if !ok || code == 0 {
		return nil
	}
	msg, _ := resp["error_msg"].(string)
	return &vkCallsOKAPIError{Code: int(code), Message: msg}
}

func truncateVKCallsLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func truncateVKCallsResp(resp map[string]interface{}) string {
	b, err := json.Marshal(resp)
	if err != nil {
		return fmt.Sprintf("(unmarshallable: %v)", err)
	}
	return truncateVKCallsLog(string(b), 300)
}

func turnURLsToAddresses(urls []string) []string {
	var addresses []string
	for _, urlStr := range urls {
		urlStr = strings.TrimSpace(urlStr)
		if urlStr == "" {
			continue
		}
		clean := strings.Split(urlStr, "?")[0]
		address := strings.TrimPrefix(strings.TrimPrefix(clean, "turn:"), "turns:")
		if address != "" {
			addresses = append(addresses, address)
		}
	}
	return addresses
}

// ─── Token chain: anon_token → getCallPreview → getAnonymousToken → OK session → joinConversation → TURN creds ───

func getTokenChain(ctx context.Context, link string, streamID int, deviceID string, creds VKCredentials, jar tlsclient.CookieJar, captchaResultChan chan string, getCaptchaMode func() string, emitCaptchaRequest func(mode, redirectURI, sessionToken string)) (string, string, []string, error) {
	profile := getRandomProfile()
	if saved, err := LoadProfileFromDisk(); err == nil && saved != nil && strings.TrimSpace(saved.UserAgent) != "" {
		profile = saved.Profile
		log.Printf("[STREAM %d] [VK Auth] Используем профиль устройства из vk_profile.json", streamID)
	}

	client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(),
		tlsclient.WithTimeoutSeconds(20),
		tlsclient.WithClientProfile(profiles.Chrome_146),
		tlsclient.WithCookieJar(jar),
	)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to initialize tls_client: %w", err)
	}

	name := generateName()
	escapedName := neturl.QueryEscape(name)

	log.Printf("[STREAM %d] [VK Auth] Identity - Name: %s | UA: %s", streamID, name, profile.UserAgent)

	doRequest := func(data string, url string) (resp map[string]interface{}, err error) {
		parsedURL, err := neturl.Parse(url)
		if err != nil {
			return nil, fmt.Errorf("parse request URL: %w", err)
		}
		domain := parsedURL.Hostname()

		req, err := fhttp.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer([]byte(data)))
		if err != nil {
			return nil, err
		}

		req.Host = domain
		applyBrowserProfileFhttp(req, profile)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Origin", "https://vk.ru")
		req.Header.Set("Referer", "https://vk.ru/")
		req.Header.Set("Sec-Fetch-Site", "same-site")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Dest", "empty")
		req.Header.Set("Priority", "u=1, i")

		httpResp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() {
			if closeErr := httpResp.Body.Close(); closeErr != nil {
				log.Printf("close response body: %s", closeErr)
			}
		}()

		body, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, err
		}

		err = json.Unmarshal(body, &resp)
		if err != nil {
			return nil, err
		}
		return resp, nil
	}

	// Step 1: get_anonym_token
	data := fmt.Sprintf("client_id=%s&token_type=messages&client_secret=%s&version=1&app_id=%s", creds.ClientID, creds.ClientSecret, creds.ClientID)
	resp, err := doRequest(data, "https://login.vk.ru/?act=get_anonym_token")
	if err != nil {
		return "", "", nil, err
	}
	dataMap, ok := resp["data"].(map[string]interface{})
	if !ok {
		return "", "", nil, fmt.Errorf("unexpected anon token response: %v", resp)
	}
	token1, ok := dataMap["access_token"].(string)
	if !ok {
		return "", "", nil, fmt.Errorf("missing access_token in response: %v", resp)
	}

	vkDelayRandom(100, 150)

	// Step 2: getCallPreview (mimics real VK client behavior)
	data = fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&fields=photo_200&access_token=%s", link, token1)
	resp, err = doRequest(data, "https://api.vk.ru/method/calls.getCallPreview?v=5.275&client_id="+creds.ClientID)
	if err != nil {
		log.Printf("[STREAM %d] [VK Auth] Warning: getCallPreview failed: %v", streamID, err)
	} else if callErr := fatalCallError(resp); callErr != nil {
		log.Printf("[STREAM %d] [VK Auth] getCallPreview returned non-retryable call error: %v", streamID, callErr)
		return "", "", nil, callErr
	}

	vkDelayRandom(200, 400)

	// Step 3: getAnonymousToken (with captcha handling)
	data = fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&name=%s&access_token=%s", link, escapedName, token1)
	urlAddr := fmt.Sprintf("https://api.vk.ru/method/calls.getAnonymousToken?v=5.275&client_id=%s", creds.ClientID)

	var token2 string
	var savedProfile *SavedProfile
	savedProfile, _ = LoadProfileFromDisk()

	for attempt := 0; ; attempt++ {
		resp, err = doRequest(data, urlAddr)
		if err != nil {
			return "", "", nil, err
		}

		if errObj, hasErr := resp["error"].(map[string]interface{}); hasErr {
			if callErr := fatalCallError(resp); callErr != nil {
				log.Printf("[STREAM %d] [VK Auth] getAnonymousToken returned non-retryable call error: %v", streamID, callErr)
				return "", "", nil, callErr
			}

			captchaErr := parseVkCaptchaError(errObj)
			if captchaErr != nil && captchaErr.RedirectURI != "" && captchaErr.SessionToken != "" {
				if attempt >= 3 {
					log.Printf("[STREAM %d] [Captcha] Max attempts reached", streamID)
					globalCaptchaLockout.Store(time.Now().Add(60 * time.Second).Unix())
					return "", "", nil, fmt.Errorf("CAPTCHA_WAIT_REQUIRED")
				}

				successToken, solveErr := solveCaptchaBySelectedMode(ctx, streamID, attempt+1, captchaErr, client, profile, savedProfile, captchaResultChan, getCaptchaMode, emitCaptchaRequest)
				if solveErr != nil {
					if errors.Is(solveErr, errCaptchaSessionExpired) {
						log.Printf("[STREAM %d] [КАПЧА] сессия исчерпана — запрос новой капчи у VK", streamID)
						savedProfile, _ = LoadProfileFromDisk()
						data = fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&name=%s&access_token=%s", link, escapedName, token1)
						vkDelayRandom(800, 1500)
						continue
					}
					log.Printf("[STREAM %d] [Captcha] Solve failed: %v", streamID, solveErr)
					globalCaptchaLockout.Store(time.Now().Add(60 * time.Second).Unix())
					return "", "", nil, fmt.Errorf("CAPTCHA_WAIT_REQUIRED")
				}

				captchaAttempt := captchaErr.CaptchaAttempt
				if captchaAttempt == "0" || captchaAttempt == "" {
					captchaAttempt = "1"
				}

				data = fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&name=%s&captcha_key=&captcha_sid=%s&is_sound_captcha=0&success_token=%s&captcha_ts=%s&captcha_attempt=%s&access_token=%s",
					link, escapedName, captchaErr.CaptchaSid, neturl.QueryEscape(successToken), captchaErr.CaptchaTs, captchaAttempt, token1)
				continue
			}
			return "", "", nil, fmt.Errorf("VK API error: %v", errObj)
		}

		respMap, okLoop := resp["response"].(map[string]interface{})
		if !okLoop {
			return "", "", nil, fmt.Errorf("unexpected getAnonymousToken response: %v", resp)
		}
		token2, okLoop = respMap["token"].(string)
		if !okLoop {
			return "", "", nil, fmt.Errorf("missing token in response: %v", resp)
		}
		break
	}

	vkDelayRandom(100, 150)

	// Step 4: OK.ru anonymLogin
	sessionData := fmt.Sprintf(`{"version":2,"device_id":"%s","client_version":"1.0.1"}`, uuid.New())
	data = fmt.Sprintf("session_data=%s&method=auth.anonymLogin&format=JSON&application_key=CGMMEJLGDIHBABABA", neturl.QueryEscape(sessionData))
	resp, err = doRequest(data, "https://calls.okcdn.ru/fb.do")
	if err != nil {
		return "", "", nil, err
	}
	token3, ok := resp["session_key"].(string)
	if !ok {
		return "", "", nil, fmt.Errorf("missing session_key in response: %v", resp)
	}

	vkDelayRandom(100, 150)

	// Step 5: joinConversationByLink → TURN creds
	data = fmt.Sprintf("joinLink=%s&isVideo=false&protocolVersion=5&capabilities=2F7F&anonymToken=%s&method=vchat.joinConversationByLink&format=JSON&application_key=CGMMEJLGDIHBABABA&session_key=%s", link, token2, token3)
	resp, err = doRequest(data, "https://calls.okcdn.ru/fb.do")
	if err != nil {
		return "", "", nil, err
	}

	tsRaw, ok := resp["turn_server"].(map[string]interface{})
	if !ok {
		return "", "", nil, fmt.Errorf("missing turn_server in response: %v", resp)
	}
	user, ok := tsRaw["username"].(string)
	if !ok {
		return "", "", nil, fmt.Errorf("missing username in turn_server")
	}
	pass, ok := tsRaw["credential"].(string)
	if !ok {
		return "", "", nil, fmt.Errorf("missing credential in turn_server")
	}
	urlsRaw, ok := tsRaw["urls"].([]interface{})
	if !ok || len(urlsRaw) == 0 {
		return "", "", nil, fmt.Errorf("missing or empty urls in turn_server")
	}

	log.Printf("[STREAM %d] [VK Auth] TURN urls (%d total):", streamID, len(urlsRaw))
	for i, u := range urlsRaw {
		log.Printf("[STREAM %d] [VK Auth]   [%d] %v", streamID, i, u)
	}

	var addresses []string
	for _, u := range urlsRaw {
		urlStr, ok := u.(string)
		if !ok {
			continue
		}
		addresses = append(addresses, turnURLsToAddresses([]string{urlStr})...)
	}

	if len(addresses) == 0 {
		return "", "", nil, fmt.Errorf("no valid TURN addresses found")
	}

	return user, pass, addresses, nil
}

func markCaptchaSessionExpired(streamID int) error {
	if _, err := rotateCaptchaProfile(); err != nil {
		log.Printf("[STREAM %d] [КАПЧА] не удалось обновить browser_fp: %v", streamID, err)
	} else {
		log.Printf("[STREAM %d] [КАПЧА] browser_fp обновлён после исчерпания сессии", streamID)
	}
	return errCaptchaSessionExpired
}

// rotateCaptchaProfile — creates a new profile on disk.
// In the standalone app this writes to vk_profile.json.
// In the core package, we just generate a fresh profile in memory.
func rotateCaptchaProfile() (*SavedProfile, error) {
	fp, err := captchaV2BrowserFP()
	if err != nil {
		return nil, err
	}
	p := getRandomProfile()
	deviceJSON := captchaV2VariedDeviceJSON(captchaV2DeviceInfo)
	return &SavedProfile{
		Profile:    p,
		DeviceJSON: deviceJSON,
		BrowserFp:  fp,
	}, nil
}

func solveCaptchaBySelectedMode(
	ctx context.Context,
	streamID int,
	attempt int,
	captchaErr *VkCaptchaError,
	client tlsclient.HttpClient,
	profile Profile,
	savedProfile *SavedProfile,
	captchaResultChan chan string,
	getCaptchaMode func() string,
	emitCaptchaRequest func(mode, redirectURI, sessionToken string),
) (string, error) {
	if fresh, err := rotateCaptchaProfile(); err == nil {
		savedProfile = fresh
	} else {
		log.Printf("[STREAM %d] [КАПЧА] profile rotate failed: %v", streamID, err)
	}

	switch getCaptchaMode() {
	case "wv":
		log.Printf("[STREAM %d] [КАПЧА] WBV: режим из настроек Android (attempt %d)", streamID, attempt)
		return requestWebViewCaptcha(streamID, captchaErr, "selected", captchaSelectedWebViewTimeout, captchaResultChan, emitCaptchaRequest)

	case "rjs":
		log.Printf("[STREAM %d] [КАПЧА] RJS: Go v2 выбран в настройках (attempt %d)", streamID, attempt)
		token, solveErr := solveVkCaptchaV2Attempts(ctx, captchaErr, client, profile, savedProfile, 2)
		if solveErr == nil {
			return token, nil
		}
		if ctx.Err() != nil {
			return "", solveErr
		}
		if isCaptchaSessionDead(solveErr) {
			log.Printf("[STREAM %d] [КАПЧА] RJS: сессия капчи мёртва, запрашиваем новую у VK", streamID)
			return "", markCaptchaSessionExpired(streamID)
		}
		if isCaptchaSessionExhausted(solveErr) {
			log.Printf("[STREAM %d] [КАПЧА] RJS: rate limit, fallback на WBV Auto", streamID)
			return requestWebViewCaptcha(streamID, captchaErr, "auto", captchaAutoWebViewTimeout, captchaResultChan, emitCaptchaRequest)
		}
		log.Printf("[STREAM %d] [КАПЧА] RJS: ошибка, fallback на WBV Auto: %v", streamID, solveErr)
		return requestWebViewCaptcha(streamID, captchaErr, "auto", captchaAutoWebViewTimeout, captchaResultChan, emitCaptchaRequest)
	}

	log.Printf("[STREAM %d] [КАПЧА] AUTO: старт цепочки (captcha attempt %d)", streamID, attempt)

	token, solveErr := solveVkCaptchaV2Attempts(ctx, captchaErr, client, profile, savedProfile, 2)
	if solveErr == nil {
		log.Printf("[STREAM %d] [КАПЧА] AUTO: Go v2 решил капчу", streamID)
		return token, nil
	}
	if ctx.Err() != nil {
		return "", solveErr
	}
	lastErr := solveErr
	if isCaptchaSessionDead(solveErr) {
		log.Printf("[STREAM %d] [КАПЧА] AUTO: сессия капчи мёртва, запрашиваем новую у VK", streamID)
		return "", markCaptchaSessionExpired(streamID)
	}
	if errors.Is(solveErr, errCaptchaV2RateLimit) || strings.Contains(strings.ToLower(solveErr.Error()), "rate limit") {
		log.Printf("[STREAM %d] [КАПЧА] AUTO: rate limit на Go v2, пробуем WBV", streamID)
	}
	log.Printf("[STREAM %d] [КАПЧА] AUTO: Go v2 не решил за 2 попытки: %v", streamID, solveErr)

	for wbvAttempt := 1; wbvAttempt <= 2; wbvAttempt++ {
		log.Printf("[STREAM %d] [КАПЧА] AUTO: WBV Auto попытка %d/2 (timeout %s)", streamID, wbvAttempt, captchaAutoWebViewTimeout)
		token, solveErr := requestWebViewCaptcha(streamID, captchaErr, "auto", captchaAutoWebViewTimeout, captchaResultChan, emitCaptchaRequest)
		if solveErr == nil {
			log.Printf("[STREAM %d] [КАПЧА] AUTO: WBV Auto решил капчу", streamID)
			return token, nil
		}
		if ctx.Err() != nil {
			return "", solveErr
		}
		lastErr = solveErr
		if isWebViewCaptchaTimeout(solveErr) {
			log.Printf("[STREAM %d] [КАПЧА] AUTO: WBV Auto timeout %d/2", streamID, wbvAttempt)
		} else {
			log.Printf("[STREAM %d] [КАПЧА] AUTO: WBV Auto ошибка %d/2: %v", streamID, wbvAttempt, solveErr)
		}

		timer := time.NewTimer(time.Duration(250+rand.Intn(250)) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}

	log.Printf("[STREAM %d] [КАПЧА] AUTO: финальная Go v2 попытка после WBV", streamID)
	token, solveErr = solveVkCaptchaV2Attempts(ctx, captchaErr, client, profile, savedProfile, 1)
	if solveErr == nil {
		log.Printf("[STREAM %d] [КАПЧА] AUTO: финальная Go v2 решила капчу", streamID)
		return token, nil
	}
	if ctx.Err() != nil {
		return "", solveErr
	}
	lastErr = solveErr
	log.Printf("[STREAM %d] [КАПЧА] AUTO: финальная Go v2 ошибка: %v", streamID, solveErr)

	log.Printf("[STREAM %d] [КАПЧА] AUTO: автоцепочка не прошла, открыт ручной WebView", streamID)
	token, solveErr = requestWebViewCaptcha(streamID, captchaErr, "manual", captchaManualWebViewTimeout, captchaResultChan, emitCaptchaRequest)
	if solveErr == nil {
		log.Printf("[STREAM %d] [КАПЧА] AUTO: ручной WebView решил капчу", streamID)
		return token, nil
	}
	if lastErr != nil {
		return "", fmt.Errorf("automatic captcha chain failed: %w; manual fallback failed: %v", lastErr, solveErr)
	}
	return "", solveErr
}

// ─── WebView captcha helpers ───

const (
	captchaAutoWebViewTimeout     = 10 * time.Second
	captchaManualWebViewTimeout   = 60 * time.Second
	captchaSelectedWebViewTimeout = 120 * time.Second
)

func drainCaptchaResult(ch chan string) {
	select {
	case <-ch:
	default:
	}
}

func requestWebViewCaptcha(
	streamID int,
	captchaErr *VkCaptchaError,
	mode string,
	timeout time.Duration,
	captchaResultChan chan string,
	emitCaptchaRequest func(mode, redirectURI, sessionToken string),
) (string, error) {
	if captchaResultChan == nil || captchaErr == nil || captchaErr.RedirectURI == "" || captchaErr.SessionToken == "" {
		return "", fmt.Errorf("webview captcha data is incomplete")
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "manual" && mode != "selected" {
		mode = "auto"
	}
	if timeout <= 0 {
		timeout = captchaAutoWebViewTimeout
	}

	drainCaptchaResult(captchaResultChan)
	emitCaptchaRequest(mode, captchaErr.RedirectURI, captchaErr.SessionToken)

	waitCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case result := <-captchaResultChan:
		result = strings.TrimSpace(result)
		if result == "" {
			return "", fmt.Errorf("webview captcha returned empty result")
		}
		lowerResult := strings.ToLower(result)
		if lowerResult == "error:timeout" {
			return "", fmt.Errorf("webview captcha timed out")
		}
		if strings.HasPrefix(lowerResult, "error:") {
			return "", fmt.Errorf("webview captcha failed: %s", result)
		}
		log.Printf("[STREAM %d] [CAPTCHA] WBV: %s solve succeeded", streamID, mode)
		return result, nil
	case <-waitCtx.Done():
		return "", fmt.Errorf("webview captcha timed out")
	}
}

func isWebViewCaptchaTimeout(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "timed out")
}

// GetCreds returns TURN credentials for a given stream.
func GetCreds(ctx context.Context, link string, streamID int, deviceID string, captchaResultChan chan string, getCaptchaMode func() string, emitCaptchaRequest func(mode, redirectURI, sessionToken string)) (string, string, []string, error) {
	return getVkCredsCached(ctx, link, streamID, deviceID, captchaResultChan, getCaptchaMode, emitCaptchaRequest)
}

func goDNSServersForPreset(preset string) []string {
	switch strings.ToLower(strings.TrimSpace(preset)) {
	case "cloudflare":
		return []string{"1.1.1.1:53", "1.0.0.1:53"}
	case "google":
		return []string{"8.8.8.8:53", "8.8.4.4:53"}
	default:
		return []string{"77.88.8.8:53", "77.88.8.1:53"}
	}
}

func isLoopbackDNSAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "::1" || host == "localhost" || host == "0.0.0.0"
}

func goDNSServersForArg(arg string) []string {
	arg = strings.TrimSpace(arg)
	if strings.HasPrefix(arg, "custom:") {
		raw := strings.TrimPrefix(arg, "custom:")
		var out []string
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !strings.Contains(part, ":") {
				part += ":53"
			}
			out = append(out, part)
		}
		if len(out) > 0 {
			return out
		}
		return goDNSServersForPreset("yandex")
	}
	return goDNSServersForPreset(arg)
}

func goDNSLabel(arg string) string {
	arg = strings.TrimSpace(arg)
	if strings.HasPrefix(arg, "custom:") {
		return "Custom DNS"
	}
	if strings.HasPrefix(arg, "doh:") {
		return "Custom DoH"
	}
	switch strings.ToLower(arg) {
	case "cloudflare":
		return "Cloudflare"
	case "google":
		return "Google DNS"
	case "doh-cloudflare":
		return "Cloudflare DoH"
	case "doh-google":
		return "Google DoH"
	case "doh-yandex":
		return "Yandex DoH"
	default:
		return "Yandex DNS"
	}
}

func formatGoDNSServers(servers []string) string {
	parts := make([]string, 0, len(servers))
	for _, s := range servers {
		host, port, err := net.SplitHostPort(s)
		if err != nil {
			parts = append(parts, s)
			continue
		}
		if port == "53" {
			parts = append(parts, host)
		} else {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

func setupGlobalResolver(arg string) {
	arg = strings.TrimSpace(arg)
	original := arg
	if arg == "" {
		arg = "yandex"
	}

	if goDNSIsDoH(arg) {
		endpoints := goDoHEndpointsForArg(arg)
		setupDoHResolver(arg, endpoints)
		return
	}

	knownPresets := map[string]bool{
		"yandex":     true,
		"cloudflare": true,
		"google":     true,
	}
	isCustom := strings.HasPrefix(arg, "custom:")
	normalized := strings.ToLower(arg)

	if !knownPresets[normalized] && !isCustom && original != "" {
		log.Printf("[CLIENT] DNS: unknown preset %q, using yandex as fallback", original)
		arg = "yandex"
	}

	servers := goDNSServersForArg(arg)
	log.Printf("[CLIENT] DNS for VK: %s (%s)", goDNSLabel(arg), formatGoDNSServers(servers))

	dialer := &net.Dialer{
		Timeout:   2 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			var lastErr error
			for _, dns := range servers {
				conn, err := dialer.DialContext(ctx, "udp", dns)
				if err == nil {
					return conn, nil
				}
				conn, err = dialer.DialContext(ctx, "tcp", dns)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if address != "" && !isLoopbackDNSAddress(address) {
				conn, err := dialer.DialContext(ctx, network, address)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
	}
}

