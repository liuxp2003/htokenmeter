package helper

import (
	"net/http"
	"net/url"
	"time"

	"github.com/lxp/htokenmeter/common"
)

// HTTPClient builds the upstream client honoring channel proxy and timeouts.
func HTTPClient(proxy string) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	if proxy != "" {
		if pu, err := url.Parse(proxy); err == nil {
			transport.Proxy = http.ProxyURL(pu)
		}
	}
	timeout := time.Duration(common.RelayTimeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}
