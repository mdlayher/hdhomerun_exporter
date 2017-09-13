// Command hdhomerun_exporter implements a Prometheus exporter for SiliconDust
// HDHomeRun devices.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/mdlayher/hdhomerun"
	"github.com/mdlayher/hdhomerun_exporter"
)

func main() {
	var (
		metricsAddr = flag.String("metrics.addr", ":9137", "address for HDHomeRun exporter")
		metricsPath = flag.String("metrics.path", "/metrics", "URL path for surfacing collected metrics")

		hdhrTimeout = flag.Duration("hdhomerun.timeout", 1*time.Second, "timeout value for requests to an HDHomeRun device; use 0 for no timeout")
	)

	flag.Parse()

	// dial is the function used to connect to an HDHomeRun device on each
	// metrics scrape request.
	dial := func(addr string) (*hdhomerun.Client, error) {
		c, err := hdhomerun.Dial(addr)
		if err != nil {
			return nil, err
		}

		c.SetTimeout(*hdhrTimeout)

		return c, nil
	}

	h := hdhomerunexporter.NewHandler(dial)

	mux := http.NewServeMux()
	mux.Handle(*metricsPath, h)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, *metricsPath, http.StatusMovedPermanently)
	})

	log.Printf("starting HDHomeRun exporter on %q", *metricsAddr)

	if err := http.ListenAndServe(*metricsAddr, mux); err != nil {
		log.Fatalf("cannot start HDHomeRun exporter: %v", err)
	}
}
