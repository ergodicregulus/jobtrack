package httpx

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// RunHealthcheckIfAsked handles `<binary> healthcheck` and exits.
//
// The image is distroless: no shell, no curl, no wget. A container healthcheck
// therefore has to be the binary itself, which is also the only version of this
// that cannot drift from the service — it resolves the port from the same
// HTTP_ADDR the server binds.
//
// Called before configuration is loaded so a healthcheck never needs a database
// URL or a session secret. A probe that requires production credentials is a
// probe that fails for the wrong reasons.
func RunHealthcheckIfAsked(defaultAddr string) {
	if len(os.Args) < 2 || os.Args[1] != "healthcheck" {
		return
	}

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: cannot parse HTTP_ADDR %q: %v\n", addr, err)
		os.Exit(2)
	}

	// /readyz, not /livez: a container healthcheck answers "should this receive
	// traffic", which is the readiness question. It also returns 503 while
	// draining, so a stopping container is correctly reported unhealthy.
	url := "http://127.0.0.1:" + port + "/readyz"
	client := &http.Client{Timeout: 3 * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// 200-399, which is what a Kubernetes httpGet probe counts as success. It
	// demanded exactly 200, so a worker whose /readyz answers 204 No Content was
	// reported unhealthy by Docker while healthy to Kubernetes.
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "healthcheck: %s returned %d\n", url, resp.StatusCode)
		os.Exit(1)
	}
	os.Exit(0)
}
