hdhomerun_exporter [![Build Status](https://travis-ci.org/mdlayher/hdhomerun_exporter.svg?branch=master)](https://travis-ci.org/mdlayher/hdhomerun_exporter) [![GoDoc](https://godoc.org/github.com/mdlayher/hdhomerun_exporter?status.svg)](https://godoc.org/github.com/mdlayher/hdhomerun_exporter) [![Go Report Card](https://goreportcard.com/badge/github.com/mdlayher/hdhomerun_exporter)](https://goreportcard.com/report/github.com/mdlayher/hdhomerun_exporter)
==================

Command `hdhomerun_exporter` implements a Prometheus exporter for SiliconDust
HDHomeRun devices. MIT Licensed.

Configuration
-------------

The `hdhomerun_exporter`'s Prometheus scrape configuration (`prometheus.yml`) is
configured in a similar way to the official Prometheus
[`blackbox_exporter`](https://github.com/prometheus/blackbox_exporter).

The `targets` list under `static_configs` should specify the addresses of any
HDHomeRun devices which should be monitored by the exporter.  The address of
the `hdhomerun_exporter` itself must be specified in `relabel_configs` as well.

```yaml
scrape_configs:
  - job_name: 'hdhomerun'
    static_configs:
      - targets:
        - '192.168.1.10' # hdhomerun device.
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - target_label: __address__
        replacement: '127.0.0.1:9137' # hdhomerun_exporter.
```