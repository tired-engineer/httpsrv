package httpsrv

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockRoundTripper is a mock implementation of http.RoundTripper for testing.	

type mockRoundTripper struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
	lastRequest   *http.Request
	callCount     int
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	m.callCount++
	m.lastRequest = req
	if m.roundTripFunc != nil {
		return m.roundTripFunc(req)
	}
	// Default behavior: return a dummy successful response
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

// TestAddSRVRoundTripper verifies that AddSRVRoundTripper correctly sets up
// the srvRoundTripper for http+srv and https+srv schemes.
func TestAddSRVRoundTripper(t *testing.T) {
	// Store original lookupSRV and restore it after the test
	originalLookupSRV := lookupSRV
	defer func() { lookupSRV = originalLookupSRV }()

	tests := []struct {
		name       string
		scheme     string
		expectCall bool // whether our srvRoundTripper's lookupSRV should be called
	}{
		{"HTTP+SRV scheme", "http+srv", true},
		{"HTTPS+SRV scheme", "https+srv", true},
		{"HTTP scheme (should bypass)", "http", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOriginalRT := &mockRoundTripper{}
			transport := &http.Transport{}
			AddSRVRoundTripper(mockOriginalRT, transport)

			client := &http.Client{Transport: transport}

			lookupCalled := false
			// Mock lookupSRV to check if it's called by our roundtripper
			lookupSRV = func(service, proto, name string) (string, []*net.SRV, error) {
				lookupCalled = true
				return "", nil, errors.New("mock SRV lookup error") // Return error to stop further processing
			}

			// Create a dummy server that the original roundtripper would hit if not for SRV error
			dummyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintln(w, "Hello from dummy server")
			}))
			defer dummyServer.Close()

			var targetURL string
			if tt.scheme == "http" || tt.scheme == "https" {
				// For non-SRV schemes, point to the dummy server directly
				// This assumes the default RoundTripper in http.Transport will be used.
				// Note: AddSRVRoundTripper registers for specific schemes, so for 'http', it won't use srvRoundTripper.
				// We need to ensure the transport can handle 'http' if we test it this way.
				// A simpler check for non-SRV might be to ensure our mockOriginalRT is called directly.
				// For this test, we are checking if our SRV specific logic is triggered.
				targetURL = tt.scheme + "://example.com/test"
			} else {
				targetURL = tt.scheme + "://service.consul/test"
			}

			_, err := client.Get(targetURL)

			if tt.expectCall {
				if !lookupCalled {
					t.Errorf("Expected lookupSRV to be called for scheme %s, but it wasn't", tt.scheme)
				}
				if err == nil || !strings.Contains(err.Error(), "mock SRV lookup error") {
					t.Errorf("Expected error from mock SRV lookup for scheme %s, got: %v", tt.scheme, err)
				}
			} else {
				if lookupCalled {
					t.Errorf("Expected lookupSRV NOT to be called for scheme %s, but it was", tt.scheme)
				}
				// For non-SRV schemes, the request might succeed or fail depending on example.com and default transport behavior.
				// The key is that our SRV logic (and thus lookupSRV mock) wasn't invoked.
			}
		})
	}
}

func TestSRVRoundTripper_RoundTrip(t *testing.T) {
	// Store original lookupSRV and restore it after each subtest group or test
	originalLookupSRV := lookupSRV
	defer func() { lookupSRV = originalLookupSRV }()

	t.Run("UnknownScheme", func(t *testing.T) {
		mockOrigRT := &mockRoundTripper{}
		rt := &srvRoundTripper{original: mockOrigRT}
		req := httptest.NewRequest("GET", "ftp+srv://example.com/path", nil)

		_, err := rt.RoundTrip(req)

		if err == nil {
			t.Fatal("Expected an error for unknown scheme, got nil")
		}
		if !strings.Contains(err.Error(), "unknown scheme ftp+srv") {
			t.Errorf("Expected error message for unknown scheme, got: %s", err.Error())
		}
		if mockOrigRT.callCount > 0 {
			t.Error("Expected original RoundTripper not to be called for unknown scheme")
		}
	})

	t.Run("SRVLookupError", func(t *testing.T) {
		mockOrigRT := &mockRoundTripper{}
		rt := &srvRoundTripper{original: mockOrigRT}
		expectedErr := errors.New("dns lookup failed")

		lookupSRV = func(service, proto, name string) (string, []*net.SRV, error) {
			return "", nil, expectedErr
		}
		defer func() { lookupSRV = originalLookupSRV }() // Restore for next test

		req := httptest.NewRequest("GET", "http+srv://service.consul/path", nil)
		_, err := rt.RoundTrip(req)

		if !errors.Is(err, expectedErr) {
			t.Fatalf("Expected SRV lookup error '%v', got: %v", expectedErr, err)
		}
		if mockOrigRT.callCount > 0 {
			t.Error("Expected original RoundTripper not to be called on SRV lookup failure")
		}
	})

	t.Run("SRVLookupReturnsNoRecords", func(t *testing.T) {
		// This test assumes the recommended change to handle empty SRV records is made.
		mockOrigRT := &mockRoundTripper{}
		rt := &srvRoundTripper{original: mockOrigRT}

		lookupSRV = func(service, proto, name string) (string, []*net.SRV, error) {
			return "cname.example.com", []*net.SRV{}, nil // No records
		}
		defer func() { lookupSRV = originalLookupSRV }()

		req := httptest.NewRequest("GET", "http+srv://service.consul/path", nil)
		_, err := rt.RoundTrip(req)

		if err == nil {
			t.Fatal("Expected an error when SRV lookup returns no records, got nil")
		}
		if !strings.Contains(err.Error(), "SRV lookup for service.consul returned no records") {
			t.Errorf("Expected specific error for no SRV records, got: %s", err.Error())
		}
		if mockOrigRT.callCount > 0 {
			t.Error("Expected original RoundTripper not to be called when no SRV records found")
		}
	})

	successTestCases := []struct {
		name               string
		originalScheme     string
		expectedScheme     string
		srvHostname        string
		srvPort            uint16
		srvTarget          string // May include trailing dot
		expectedHostInURL  string
		requestPath        string
	}{
		{
			name:               "HTTP+SRV successful lookup",
			originalScheme:     "http+srv",
			expectedScheme:     "http",
			srvHostname:        "api.service.consul",
			srvPort:            8080,
			srvTarget:          "node1.consul.", // Note trailing dot
			expectedHostInURL:  "node1.consul.:8080",
			requestPath:        "/healthz",
		},
		{
			name:               "HTTPS+SRV successful lookup",
			originalScheme:     "https+srv",
			expectedScheme:     "https",
			srvHostname:        "secure.service.consul",
			srvPort:            8443,
			srvTarget:          "secure-node.internal", // No trailing dot
			expectedHostInURL:  "secure-node.internal:8443",
			requestPath:        "/status",
		},
	}

	for _, tt := range successTestCases {
		t.Run(tt.name, func(t *testing.T) {
			mockOrigRT := &mockRoundTripper{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					// This is the request received by the original RoundTripper
					if req.URL.Scheme != tt.expectedScheme {
						t.Errorf("Expected scheme %s, got %s", tt.expectedScheme, req.URL.Scheme)
					}
					if req.URL.Host != tt.expectedHostInURL {
						t.Errorf("Expected host %s, got %s", tt.expectedHostInURL, req.URL.Host)
					}
					if req.URL.Path != tt.requestPath {
						t.Errorf("Expected path %s, got %s", tt.requestPath, req.URL.Path)
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
				},
			}
			rt := &srvRoundTripper{original: mockOrigRT}

			lookupSRV = func(service, proto, name string) (string, []*net.SRV, error) {
				if name != tt.srvHostname {
					t.Fatalf("lookupSRV called with unexpected hostname: got %s, want %s", name, tt.srvHostname)
				}
				return "cname.example.com", []*net.SRV{
					{Target: tt.srvTarget, Port: tt.srvPort, Priority: 10, Weight: 100},
					{Target: "other.target.consul", Port: 9090, Priority: 20, Weight: 100}, // Ensure first is used
				}, nil
			}
			defer func() { lookupSRV = originalLookupSRV }()

			initialURL := fmt.Sprintf("%s://%s%s", tt.originalScheme, tt.srvHostname, tt.requestPath)
			req := httptest.NewRequest("GET", initialURL, nil)

			resp, err := rt.RoundTrip(req)
			if err != nil {
				t.Fatalf("RoundTrip failed: %v", err)
			}
			defer resp.Body.Close()

			if mockOrigRT.callCount != 1 {
				t.Errorf("Expected original RoundTripper to be called once, called %d times", mockOrigRT.callCount)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status OK, got %d", resp.StatusCode)
			}
		})
	}

	t.Run("OriginalRoundTripperError", func(t *testing.T) {
		expectedErr := errors.New("original transport failed")
		mockOrigRT := &mockRoundTripper{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return nil, expectedErr
			},
		}
		rt := &srvRoundTripper{original: mockOrigRT}

		lookupSRV = func(service, proto, name string) (string, []*net.SRV, error) {
			return "cname.example.com", []*net.SRV{
				{Target: "target.host.", Port: 1234},
			}, nil
		}
		defer func() { lookupSRV = originalLookupSRV }()

		req := httptest.NewRequest("GET", "http+srv://service.consul/path", nil)
		_, err := rt.RoundTrip(req)

		if !errors.Is(err, expectedErr) {
			t.Fatalf("Expected error '%v' from original RoundTripper, got: %v", expectedErr, err)
		}
		if mockOrigRT.callCount != 1 {
			t.Errorf("Expected original RoundTripper to be called once, called %d times", mockOrigRT.callCount)
		}
	})
}
