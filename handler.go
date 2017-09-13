package hdhomerunexporter

import (
	"fmt"
	"net"
	"net/http"

	"github.com/mdlayher/hdhomerun"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// hdhomerunPort is the default TCP port used to communicate with
	// HDHomeRun devices.
	hdhomerunPort = "65001"
)

var _ http.Handler = &handler{}

// A handler is an http.Handler that serves Prometheus metrics for
// HDHomeRun devices.
type handler struct {
	dial func(addr string) (*hdhomerun.Client, error)
}

// NewHandler returns an http.Handler that serves Prometheus metrics for
// HDHomeRun devices. The dial function specifies how to connect to a
// device with the specified address on each HTTP request.
//
// Each HTTP request must contain a "target" query parameter which indicates
// the network address of the device which should be scraped for metrics.
// If no port is specified, the HDHomeRun device default of 65001 will be used.
func NewHandler(dial func(addr string) (*hdhomerun.Client, error)) http.Handler {
	return &handler{
		dial: dial,
	}
}

// ServeHTTP implements http.Handler.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Prometheus is configured to send a target parameter with each scrape
	// request. This determines which device should be scraped for metrics.
	target := r.URL.Query().Get("target")
	if target == "" {
		http.Error(w, "missing target parameter", http.StatusBadRequest)
		return
	}

	host, port, err := net.SplitHostPort(target)
	if err != nil {
		// Assume no port was provided and use the default.
		host = target
		port = hdhomerunPort
	}

	addr := net.JoinHostPort(host, port)

	c, err := h.dial(addr)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf("failed to dial HDHomeRun device at %q: %v", addr, err),
			http.StatusInternalServerError,
		)
		return
	}
	defer c.Close()

	metrics := serveMetrics(newDevice(c))
	metrics.ServeHTTP(w, r)
}

// serveMetrics creates a Prometheus metrics handler for a Device.
func serveMetrics(d device) http.Handler {
	reg := prometheus.NewRegistry()
	reg.MustRegister(newCollector(d))

	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}
