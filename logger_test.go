package httpretty

import (
	"bytes"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestSanitizeHeader(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodGet, "http://example.com/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Api-Key", "super-secret-key-123")
	req.Header.Set("X-Custom-Token", "custom-token-xyz")
	req.Header.Set("User-Agent", "TestClient/1.0")

	logger := &Logger{
		RequestHeader: true,
	}
	logger.SanitizeHeader([]string{"X-Api-Key", "X-Custom-Token"})

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.PrintRequest(req)

	output := buf.String()
	if !strings.Contains(output, "> X-Api-Key: [REDACTED]") {
		t.Errorf("expected X-Api-Key to be redacted, got:\n%s", output)
	}
	if !strings.Contains(output, "> X-Custom-Token: [REDACTED]") {
		t.Errorf("expected X-Custom-Token to be redacted, got:\n%s", output)
	}
	if !strings.Contains(output, "> User-Agent: TestClient/1.0") {
		t.Errorf("expected User-Agent to remain unredacted, got:\n%s", output)
	}
}

func TestSanitizeHeaderDefaultOverride(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer sensitive-token-here")
	req.Header.Set("Cookie", "session_id=secret-session-id")

	logger := &Logger{
		RequestHeader: true,
	}
	// Redact Authorization and Cookie with [REDACTED] instead of default block characters
	logger.SanitizeHeader([]string{"Authorization", "Cookie"})

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.PrintRequest(req)

	output := buf.String()
	if !strings.Contains(output, "> Authorization: [REDACTED]") {
		t.Errorf("expected Authorization to be redacted with [REDACTED], got:\n%s", output)
	}
	if strings.Contains(output, "████████████████████") {
		t.Errorf("expected default block sanitization to be overridden by [REDACTED], got:\n%s", output)
	}
	if !strings.Contains(output, "> Cookie: [REDACTED]") {
		t.Errorf("expected Cookie to be redacted with [REDACTED], got:\n%s", output)
	}
}

func TestSanitizeHeaderStructField(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Secret-Auth", "secret-value")

	logger := &Logger{
		RequestHeader:   true,
		SanitizeHeaders: []string{"X-Secret-Auth"},
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.PrintRequest(req)

	output := buf.String()
	if !strings.Contains(output, "> X-Secret-Auth: [REDACTED]") {
		t.Errorf("expected X-Secret-Auth to be redacted from struct field config, got:\n%s", output)
	}
}

func TestRedactHeadersAlias(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Token", "secret-token")

	logger := &Logger{
		RequestHeader: true,
	}
	logger.RedactHeaders([]string{"X-Token"})

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.PrintRequest(req)

	output := buf.String()
	if !strings.Contains(output, "> X-Token: [REDACTED]") {
		t.Errorf("expected X-Token to be redacted using RedactHeaders alias, got:\n%s", output)
	}
}

func TestSetHeaderSanitizer(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Custom-Token", "supersecret12345")
	req.Header.Set("Authorization", "Bearer topsecretpassword")

	logger := &Logger{
		RequestHeader: true,
	}
	logger.SetHeaderSanitizer(func(name, value string) string {
		if strings.EqualFold(name, "X-Custom-Token") {
			return value[:5] + "****"
		}
		return value
	})

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.PrintRequest(req)

	output := buf.String()
	if !strings.Contains(output, "> X-Custom-Token: super****") {
		t.Errorf("expected masked X-Custom-Token, got:\n%s", output)
	}
	// Authorization should fall through to default sanitizers since custom sanitizer returned original value
	if !strings.Contains(output, "> Authorization: Bearer ████████████████████") {
		t.Errorf("expected Authorization to fall through to default sanitizer, got:\n%s", output)
	}
}

func TestSetHeaderSanitizerOverrideDefault(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer topsecretpassword")

	logger := &Logger{
		RequestHeader: true,
	}
	logger.SetHeaderSanitizer(func(name, value string) string {
		if strings.EqualFold(name, "Authorization") {
			return "Bearer [REDACTED]"
		}
		return value
	})

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.PrintRequest(req)

	output := buf.String()
	if !strings.Contains(output, "> Authorization: Bearer [REDACTED]") {
		t.Errorf("expected Authorization to be customized by HeaderSanitizer, got:\n%s", output)
	}
	if strings.Contains(output, "████████████████████") {
		t.Errorf("did not expect default block sanitization, got:\n%s", output)
	}
}

func TestSetHeaderSanitizerPanic(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Bad-Header", "bad-value")

	logger := &Logger{
		RequestHeader: true,
	}
	logger.SetHeaderSanitizer(func(name, value string) string {
		if name == "X-Bad-Header" {
			panic("intentional panic in sanitizer")
		}
		return value
	})

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	// Verify PrintRequest does not crash when HeaderSanitizer panics
	logger.PrintRequest(req)

	output := buf.String()
	if !strings.Contains(output, "panic while sanitizing header X-Bad-Header") {
		t.Errorf("expected panic warning in output, got:\n%s", output)
	}
	if !strings.Contains(output, "> X-Bad-Header: bad-value") {
		t.Errorf("expected fallback to original header value after panic, got:\n%s", output)
	}
}

func TestSanitizeHeaderCaseInsensitive(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header["x-custom-key"] = []string{"secret-key"}

	logger := &Logger{
		RequestHeader: true,
	}
	logger.SanitizeHeader([]string{"X-CUSTOM-KEY"})

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.PrintRequest(req)

	output := buf.String()
	if !strings.Contains(output, "[REDACTED]") {
		t.Errorf("expected case-insensitive match to redact header, got:\n%s", output)
	}
}

func TestSanitizeHeaderSkipSanitize(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Api-Key", "raw-secret-key")
	req.Header.Set("Authorization", "Bearer raw-token")

	logger := &Logger{
		RequestHeader: true,
		SkipSanitize:  true,
	}
	logger.SanitizeHeader([]string{"X-Api-Key", "Authorization"})

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.PrintRequest(req)

	output := buf.String()
	if !strings.Contains(output, "> X-Api-Key: raw-secret-key") {
		t.Errorf("expected raw X-Api-Key with SkipSanitize=true, got:\n%s", output)
	}
	if !strings.Contains(output, "> Authorization: Bearer raw-token") {
		t.Errorf("expected raw Authorization with SkipSanitize=true, got:\n%s", output)
	}
}

func TestSanitizeHeaderResponse(t *testing.T) {
	t.Parallel()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Proto:      "HTTP/1.1",
		Header:     http.Header{},
	}
	resp.Header.Set("X-Server-Secret", "server-secret-value")
	resp.Header.Set("Set-Cookie", "session=12345; Secure")

	logger := &Logger{
		ResponseHeader: true,
	}
	logger.SanitizeHeader([]string{"X-Server-Secret"})

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.PrintResponse(resp)

	output := buf.String()
	if !strings.Contains(output, "< X-Server-Secret: [REDACTED]") {
		t.Errorf("expected response header X-Server-Secret to be redacted, got:\n%s", output)
	}
	// Set-Cookie should still be sanitized with default sanitizer
	if !strings.Contains(output, "< Set-Cookie: session=████████████████████; Secure") {
		t.Errorf("expected Set-Cookie to retain default sanitization, got:\n%s", output)
	}
}

func TestExportedSanitizers(t *testing.T) {
	t.Parallel()
	// Test exported sanitizers for issue #14 (accessible from outside internal package)
	authSanitized := AuthorizationSanitizer("Bearer mysecret123")
	if authSanitized != "Bearer ████████████████████" {
		t.Errorf("AuthorizationSanitizer error: got %q", authSanitized)
	}

	cookieSanitized := CookieSanitizer("session=abc123")
	if cookieSanitized != "session=████████████████████" {
		t.Errorf("CookieSanitizer error: got %q", cookieSanitized)
	}

	setCookieSanitized := SetCookieSanitizer("session=abc123; Path=/; Secure")
	if setCookieSanitized != "session=████████████████████; Path=/; Secure" {
		t.Errorf("SetCookieSanitizer error: got %q", setCookieSanitized)
	}
}

func TestSanitizeHeaderConcurrency(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader: true,
	}
	logger.SanitizeHeader([]string{"X-Token", "Authorization"})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
			if err != nil {
				t.Errorf("cannot create request: %v", err)
				return
			}
			req.Header.Set("X-Token", "token-val")
			req.Header.Set("Authorization", "Bearer val")
			req.Header.Set("Other", "val")

			var buf bytes.Buffer
			l := &Logger{
				RequestHeader: true,
			}
			l.SanitizeHeader([]string{"X-Token"})
			l.SetOutput(&buf)
			l.PrintRequest(req)

			if !strings.Contains(buf.String(), "> X-Token: [REDACTED]") {
				t.Errorf("missing [REDACTED] in concurrent test")
			}
		}(i)
	}
	wg.Wait()
}
