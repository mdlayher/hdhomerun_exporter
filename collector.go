package hdhomerunexporter

import (
	"strconv"
	"sync"

	"github.com/mdlayher/hdhomerun"
	"github.com/prometheus/client_golang/prometheus"
)

var _ prometheus.Collector = &collector{}

// A collector is a prometheus.Collector for a device.
type collector struct {
	DeviceInfo *prometheus.Desc
	TunerInfo  *prometheus.Desc

	TunerSignalStrengthRatio *prometheus.Desc
	TunerSignalToNoiseRatio  *prometheus.Desc
	TunerSymbolErrorRatio    *prometheus.Desc

	CableCARDBytesPerSecond *prometheus.Desc
	CableCARDOverflow       *prometheus.Desc
	CableCARDResync         *prometheus.Desc

	NetworkPacketsPerSecond *prometheus.Desc
	NetworkErrors           *prometheus.Desc

	d device
}

// newCollector constructs a collector using a device.
func newCollector(d device) prometheus.Collector {
	return &collector{
		DeviceInfo: prometheus.NewDesc(
			"hdhomerun_device_info",
			"Metadata about the device.",
			[]string{"model"},
			nil,
		),

		TunerInfo: prometheus.NewDesc(
			"hdhomerun_tuner_info",
			"Metadata about each of the tuners available to a device.",
			[]string{"tuner", "channel", "lock"},
			nil,
		),

		TunerSignalStrengthRatio: prometheus.NewDesc(
			"hdhomerun_tuner_signal_strength_ratio",
			"Television signal strength ratio for this tuner.",
			[]string{"tuner"},
			nil,
		),

		TunerSignalToNoiseRatio: prometheus.NewDesc(
			"hdhomerun_tuner_signal_to_noise_ratio",
			"Television signal-to-noise ratio for this tuner.",
			[]string{"tuner"},
			nil,
		),

		TunerSymbolErrorRatio: prometheus.NewDesc(
			"hdhomerun_tuner_symbol_error_ratio",
			"Television symbol error ratio for this tuner.",
			[]string{"tuner"},
			nil,
		),

		CableCARDBytesPerSecond: prometheus.NewDesc(
			"hdhomerun_cablecard_bytes_per_second",
			"Number of bytes per second being received by the CableCARD.",
			nil,
			nil,
		),

		CableCARDOverflow: prometheus.NewDesc(
			"hdhomerun_cablecard_overflow",
			"Number of buffer overflows for the CableCARD.",
			nil,
			nil,
		),

		CableCARDResync: prometheus.NewDesc(
			"hdhomerun_cablecard_resync",
			"Number of re-sync operations due to missing sync byte in transport stream for the CableCARD.",
			nil,
			nil,
		),

		NetworkPacketsPerSecond: prometheus.NewDesc(
			"hdhomerun_network_packets_per_second",
			"Number of packets per second being sent by the device for this tuner.",
			[]string{"tuner"},
			nil,
		),

		NetworkErrors: prometheus.NewDesc(
			"hdhomerun_network_errors",
			"Number of device network errors for this tuner.",
			[]string{"tuner"},
			nil,
		),

		d: d,
	}
}

// Describe implements prometheus.Collector.
func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.DeviceInfo,
		c.TunerInfo,
		c.TunerSignalStrengthRatio,
		c.TunerSignalToNoiseRatio,
		c.TunerSymbolErrorRatio,
		c.CableCARDBytesPerSecond,
		c.CableCARDOverflow,
		c.CableCARDResync,
		c.NetworkPacketsPerSecond,
		c.NetworkErrors,
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect implements prometheus.Collector.
func (c *collector) Collect(ch chan<- prometheus.Metric) {
	model, err := c.d.Model()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.DeviceInfo, err)
		return
	}

	ch <- prometheus.MustNewConstMetric(
		c.DeviceInfo,
		prometheus.GaugeValue,
		1,
		model,
	)

	// All tuners share the path into the CableCARD, and thus, these stats
	// are identical.
	//
	// https://forum.silicondust.com/forum/viewtopic.php?f=125&t=65957
	var ccOnce sync.Once

	err = c.d.ForEachTuner(func(t tuner) error {
		stats, err := t.Debug()
		if err != nil {
			return err
		}

		tuner := strconv.Itoa(t.Index())

		c.collectTuner(ch, tuner, stats.Tuner)
		c.collectNetwork(ch, tuner, stats.Network)

		ccOnce.Do(func() {
			c.collectCableCARD(ch, stats.CableCARD)
		})

		return nil
	})
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.TunerInfo, err)
		return
	}
}

// collectTuner collects tuner status metrics.
func (c *collector) collectTuner(ch chan<- prometheus.Metric, tuner string, ts *hdhomerun.TunerStatus) {
	if ts == nil {
		return
	}

	labels := []string{
		tuner,
		ts.Channel,
		ts.Lock,
	}

	ch <- prometheus.MustNewConstMetric(
		c.TunerInfo,
		prometheus.GaugeValue,
		1,
		labels...,
	)

	ds := []descValue{
		{
			desc:  c.TunerSignalStrengthRatio,
			value: ratio(ts.SignalStrength),
		},
		{
			desc:  c.TunerSignalToNoiseRatio,
			value: ratio(ts.SignalToNoiseQuality),
		},
		{
			desc:  c.TunerSymbolErrorRatio,
			value: ratio(ts.SymbolErrorQuality),
		},
	}

	for _, d := range ds {
		ch <- prometheus.MustNewConstMetric(
			d.desc,
			prometheus.GaugeValue,
			d.value,
			tuner,
		)
	}
}

// collectCableCARD collects CableCARD status metrics.
func (c *collector) collectCableCARD(ch chan<- prometheus.Metric, cc *hdhomerun.CableCARDStatus) {
	if cc == nil {
		return
	}

	ds := []descValue{
		{
			desc:  c.CableCARDBytesPerSecond,
			value: bytesPerSecond(cc.BitsPerSecond),
		},
		{
			desc:  c.CableCARDOverflow,
			value: float64(cc.Overflow),
		},
		{
			desc:  c.CableCARDResync,
			value: float64(cc.Resync),
		},
	}

	for _, d := range ds {
		ch <- prometheus.MustNewConstMetric(
			d.desc,
			prometheus.GaugeValue,
			d.value,
		)
	}
}

// collectNetwork collects network status metrics.
func (c *collector) collectNetwork(ch chan<- prometheus.Metric, tuner string, net *hdhomerun.NetworkStatus) {
	if net == nil {
		return
	}

	ds := []descValue{
		{
			desc:  c.NetworkPacketsPerSecond,
			value: float64(net.PacketsPerSecond),
		},
		{
			desc:  c.NetworkErrors,
			value: float64(net.Errors),
		},
	}

	for _, d := range ds {
		ch <- prometheus.MustNewConstMetric(
			d.desc,
			prometheus.GaugeValue,
			d.value,
			tuner,
		)
	}
}

// A device is a wrapper for an HDHomeRun device.
type device interface {
	Model() (string, error)
	ForEachTuner(func(t tuner) error) error
}

// A tuner is a wrapper for an HDHomeRun tuner.
type tuner interface {
	Index() int
	Debug() (*hdhomerun.TunerDebug, error)
}

var _ device = &hdhrDevice{}

// A hdhrDevice is a device which wraps a *hdhomerun.Client.
type hdhrDevice struct {
	c *hdhomerun.Client
}

func newDevice(c *hdhomerun.Client) device {
	return &hdhrDevice{c: c}
}

func (d *hdhrDevice) Model() (string, error) {
	return d.c.Model()
}

func (d *hdhrDevice) ForEachTuner(fn func(t tuner) error) error {
	return d.c.ForEachTuner(func(t *hdhomerun.Tuner) error {
		return fn(&hdhrTuner{t: t})
	})
}

var _ tuner = &hdhrTuner{}

// A hdhrTuner is a tuner which wraps a *hdhomerun.Tuner.
type hdhrTuner struct {
	t *hdhomerun.Tuner
}

func (t *hdhrTuner) Index() int {
	return t.t.Index
}

func (t *hdhrTuner) Debug() (*hdhomerun.TunerDebug, error) {
	return t.t.Debug()
}

// ratio converts a percentage into a 0.0-1.0 ratio.
func ratio(percent int) float64 {
	return float64(percent) / 100
}

// bytesPerSecond converts a bits per second measurement into bytes per second.
func bytesPerSecond(bitsPerSecond int) float64 {
	return float64(bitsPerSecond) / 8
}

// A descValue is a Prometheus metric description and associated value.
type descValue struct {
	desc  *prometheus.Desc
	value float64
}
