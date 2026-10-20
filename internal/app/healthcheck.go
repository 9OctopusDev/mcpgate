package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

func runHealthcheck(ctx context.Context, args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	raw := flags.String("url", "http://127.0.0.1:9090/readyz", "loopback readiness URL")
	if flags.Parse(args) != nil {
		return 2
	}
	parsed, err := url.Parse(*raw)
	if err != nil || parsed.Scheme != "http" || parsed.Path != "/readyz" {
		fmt.Fprintln(stderr, "healthcheck URL is invalid")
		return 2
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		fmt.Fprintln(stderr, "healthcheck URL must be loopback")
		return 2
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, parsed.String(), nil)
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
