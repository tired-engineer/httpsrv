package httpsrv

import (
	"fmt"
	"net"
	"net/http"
)

var lookupSRV = net.LookupSRV // Allow overriding for tests

type srvRoundTripper struct {
	original http.RoundTripper
}

func (s *srvRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	scheme := req.URL.Scheme
	if scheme == "https+srv" {
		req.URL.Scheme = "https"
	} else if scheme == "http+srv" {
		req.URL.Scheme = "http"
	} else {
		return nil, fmt.Errorf("unknown scheme %s", scheme)
	}

	hostname := req.URL.Hostname()
	_, rrs, err := lookupSRV("", "", hostname) // Use the overrideable function
	if err != nil {
		return nil, err
	}
	// It's good practice to handle the case where no SRV records are found.
	if len(rrs) == 0 {
		return nil, fmt.Errorf("SRV lookup for %s returned no records", hostname)
	}
	req.URL.Host = fmt.Sprintf("%s:%d", rrs[0].Target, rrs[0].Port)

	return s.original.RoundTrip(req)
}

// AddSRVRoundTripper adds a round tripper to the transport that handles https+srv and http+srv schemes.
// The round tripper will resolve the SRV records via default resolver and use the first result (host and port).
// The original round tripper will be used for the actual request.
// Example:
//
//	http+srv://simple.service.consul/healthz -> http://ac1e1409.addr.lon.consul.:31883/healthz
func AddSRVRoundTripper(original http.RoundTripper, transport *http.Transport) {
	rtt := &srvRoundTripper{
		original: original,
	}
	transport.RegisterProtocol("https+srv", rtt)
	transport.RegisterProtocol("http+srv", rtt)
}
