package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func serve(origin, method string) *httptest.ResponseRecorder {
	h := corsMiddleware(allowlistChecker([]string{"http://localhost:5177", "http://localhost:5176"}))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	req := httptest.NewRequest(method, "/auth/me", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCORSEchoesOnlyAllowlistedOrigins(t *testing.T) {
	for _, o := range []string{"http://localhost:5177", "http://localhost:5176"} {
		rec := serve(o, "GET")
		if rec.Header().Get("Access-Control-Allow-Origin") != o || rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Errorf("%s: headers %v", o, rec.Header())
		}
	}
	for _, o := range []string{"http://evil.example", "http://localhost:5174", "http://localhost:5177.evil.example", "null", ""} {
		rec := serve(o, "GET")
		if rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Errorf("origin %q must not be granted credentialed access: %v", o, rec.Header())
		}
	}
}

func TestCORSNeverUsesWildcardWithCredentials(t *testing.T) {
	if got := serve("http://localhost:5177", "GET").Header().Get("Access-Control-Allow-Origin"); got == "*" {
		t.Error("wildcard origin is not allowed on a cookie-bearing route group")
	}
}

func TestCORSPreflight(t *testing.T) {
	rec := serve("http://localhost:5177", "OPTIONS")
	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight = %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" || rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Errorf("preflight headers missing: %v", rec.Header())
	}
}

func TestAllowlistCheckerHandlesEmptyList(t *testing.T) {
	if allowlistChecker(nil)("http://localhost:5177") {
		t.Error("an empty allowlist must reject everything")
	}
}
