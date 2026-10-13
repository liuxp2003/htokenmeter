package helper

import (
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/lxp/htokenmeter/common"
)

// Clients are cached per proxy so TCP+TLS handshakes are amortized across
// requests instead of being re-paid on every upstream attempt. No
// Client.Timeout here: it covers the whole body read and would cut long SSE
// streams — deadlines are applied per request at the call site
// (controller.doUpstreamRequest) with stream-aware semantics.
var clientCache sync.Map // proxy URL -> *http.Client

// HTTPClient returns the shared upstream client for a channel proxy.
func HTTPClient(proxy string) *http.Client {
	if v, ok := clientCache.Load(proxy); ok {
		return v.(*http.Client)
	}
	transport := &http.Transport{
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	if proxy != "" {
		pu, err := url.Parse(proxy)
		if err != nil {
			common.Error("channel proxy URL %q invalid (%v) — using direct connection", proxy, err)
		} else {
			transport.Proxy = http.ProxyURL(pu)
		}
	}
	cl := &http.Client{Transport: transport}
	actual, _ := clientCache.LoadOrStore(proxy, cl)
	return actual.(*http.Client)
}
