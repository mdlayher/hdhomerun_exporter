package hdhomerunexporter

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mdlayher/hdhomerun"
	"github.com/prometheus/prometheus/util/promlint"
)

func TestCollector(t *testing.T) {
	tests := []struct {
		name    string
		d       device
		metrics []string
	}{
		{
			name: "no tuners",
			d: &testDevice{
				model: "hdhomerun_test",
			},
			metrics: []string{
				`hdhomerun_device_info{model="hdhomerun_test"} 1`,
			},
		},
		{
			name: "not tuned",
			d: &testDevice{
				model: "hdhomerun_test",
				tuners: []testTuner{{
					index: 0,
					debug: &hdhomerun.TunerDebug{
						Tuner: &hdhomerun.TunerStatus{
							Channel: "none",
							Lock:    "none",
						},
						Device:          &hdhomerun.DeviceStatus{},
						CableCARD:       &hdhomerun.CableCARDStatus{},
						TransportStream: &hdhomerun.TransportStreamStatus{},
						Network:         &hdhomerun.NetworkStatus{},
					},
				}},
			},
			metrics: []string{
				`hdhomerun_cablecard_bytes_per_second 0`,
				`hdhomerun_cablecard_overflow 0`,
				`hdhomerun_cablecard_resync 0`,
				`hdhomerun_device_info{model="hdhomerun_test"} 1`,
				`hdhomerun_network_errors{tuner="0"} 0`,
				`hdhomerun_network_packets_per_second{tuner="0"} 0`,
				`hdhomerun_tuner_info{channel="none",lock="none",tuner="0"} 1`,
				`hdhomerun_tuner_signal_strength_ratio{tuner="0"} 0`,
				`hdhomerun_tuner_signal_to_noise_ratio{tuner="0"} 0`,
				`hdhomerun_tuner_symbol_error_ratio{tuner="0"} 0`,
			},
		},
		{
			name: "tuned",
			d: &testDevice{
				model: "hdhomerun_test",
				tuners: []testTuner{
					{
						index: 0,
						debug: &hdhomerun.TunerDebug{
							Tuner: &hdhomerun.TunerStatus{
								Channel:              "qam:381000000",
								Lock:                 "qam256:381000000",
								SignalStrength:       100,
								SignalToNoiseQuality: 100,
								SymbolErrorQuality:   100,
							},
							Device: &hdhomerun.DeviceStatus{
								BitsPerSecond: 38809216,
								Resync:        1,
								Overflow:      1,
							},
							CableCARD: &hdhomerun.CableCARDStatus{
								BitsPerSecond: 38810720,
								Resync:        1,
								Overflow:      1,
							},
							TransportStream: &hdhomerun.TransportStreamStatus{
								BitsPerSecond:   2534240,
								TransportErrors: 1,
								CRCErrors:       1,
							},
							Network: &hdhomerun.NetworkStatus{
								PacketsPerSecond: 241,
								Errors:           1,
								Stop:             hdhomerun.StopReasonNotStopped,
							},
						},
					},
					{
						index: 1,
						debug: &hdhomerun.TunerDebug{
							Tuner: &hdhomerun.TunerStatus{
								Channel: "none",
								Lock:    "none",
							},
							Device:          &hdhomerun.DeviceStatus{},
							CableCARD:       &hdhomerun.CableCARDStatus{},
							TransportStream: &hdhomerun.TransportStreamStatus{},
							Network:         &hdhomerun.NetworkStatus{},
						},
					},
				},
			},
			metrics: []string{
				`hdhomerun_cablecard_bytes_per_second 4.85134e+06`,
				`hdhomerun_cablecard_overflow 1`,
				`hdhomerun_cablecard_resync 1`,
				`hdhomerun_device_info{model="hdhomerun_test"} 1`,
				`hdhomerun_network_errors{tuner="0"} 1`,
				`hdhomerun_network_errors{tuner="1"} 0`,
				`hdhomerun_network_packets_per_second{tuner="0"} 241`,
				`hdhomerun_network_packets_per_second{tuner="1"} 0`,
				`hdhomerun_tuner_info{channel="qam:381000000",lock="qam256:381000000",tuner="0"} 1`,
				`hdhomerun_tuner_info{channel="none",lock="none",tuner="1"} 1`,
				`hdhomerun_tuner_signal_strength_ratio{tuner="0"} 1`,
				`hdhomerun_tuner_signal_strength_ratio{tuner="1"} 0`,
				`hdhomerun_tuner_signal_to_noise_ratio{tuner="0"} 1`,
				`hdhomerun_tuner_signal_to_noise_ratio{tuner="1"} 0`,
				`hdhomerun_tuner_symbol_error_ratio{tuner="0"} 1`,
				`hdhomerun_tuner_symbol_error_ratio{tuner="1"} 0`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := testCollector(t, tt.d)

			s := bufio.NewScanner(bytes.NewReader(body))
			for s.Scan() {
				// Skip metric HELP and TYPE lines.
				text := s.Text()
				if strings.HasPrefix(text, "#") {
					continue
				}

				var found bool
				for _, m := range tt.metrics {
					if text == m {
						found = true
						break
					}
				}

				if !found {
					t.Log(string(body))
					t.Fatalf("metric string not matched in whitelist: %s", text)
				}
			}

			if err := s.Err(); err != nil {
				t.Fatalf("failed to scan metrics: %v", err)
			}
		})
	}
}

// testCollector uses the input device to generate a blob of Prometheus text
// format metrics.
func testCollector(t *testing.T, d device) []byte {
	t.Helper()

	s := httptest.NewServer(serveMetrics(d))
	defer s.Close()

	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

	res, err := http.Get(u.String())
	if err != nil {
		t.Fatalf("failed to perform HTTP request: %v", err)
	}
	defer res.Body.Close()

	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	// Ensure best practices are followed by linting the metrics.
	problems, err := promlint.New(bytes.NewReader(b)).Lint()
	if err != nil {
		t.Fatalf("failed to lint metrics: %v", err)
	}

	if len(problems) > 0 {
		for _, p := range problems {
			t.Logf("lint: %s: %s", p.Metric, p.Text)
		}

		t.Fatal("one or more promlint errors found")
	}

	return b
}

var _ device = &testDevice{}

type testDevice struct {
	model  string
	tuners []testTuner
}

func (d *testDevice) Model() (string, error) {
	return d.model, nil
}

func (d *testDevice) ForEachTuner(fn func(t tuner) error) error {
	for _, t := range d.tuners {
		if err := fn(t); err != nil {
			return err
		}
	}

	return nil
}

var _ tuner = &testTuner{}

type testTuner struct {
	index int
	debug *hdhomerun.TunerDebug
}

func (t testTuner) Index() int                            { return t.index }
func (t testTuner) Debug() (*hdhomerun.TunerDebug, error) { return t.debug, nil }
