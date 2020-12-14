package adblockr

import (
	"context"
	"golang.org/x/net/publicsuffix"
	"net"
	"net/http"
	"net/http/cookiejar"
	"time"
)

func NewHttpClient(nsAddress string, nsTimeoutMs int, timeoutInSecs int) *http.Client {
	dialer := &net.Dialer{
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{
					Timeout: time.Duration(nsTimeoutMs) * time.Millisecond,
				}
				return d.DialContext(ctx, "udp", nsAddress)
			},
		},
	}

	httpCookieJar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})

	c := &http.Client{
		Timeout: time.Duration(timeoutInSecs) * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 1024,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, addr)
			},
		},
		Jar: httpCookieJar,
	}

	return c
}
