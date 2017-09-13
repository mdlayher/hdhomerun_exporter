package hdhomerunexporter_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/mdlayher/hdhomerun"
	"github.com/mdlayher/hdhomerun_exporter"
)

func TestNewHandler(t *testing.T) {
	tests := []struct {
		name   string
		target string
		code   int
	}{
		{
			name: "no target",
			code: http.StatusBadRequest,
		},
		{
			name:   "bad target",
			target: "foo:bar",
			code:   http.StatusInternalServerError,
		},
		{
			name:   "target no port",
			target: "foo",
			code:   http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := testHandler(t, tt.target)

			if diff := cmp.Diff(tt.code, res.StatusCode); diff != "" {
				t.Fatalf("unexpected HTTP status code (-want +got):\n%s", diff)
			}
		})
	}
}

// testHandler performs a single HTTP request to a handler created using
// NewHandler, using the specified target.
func testHandler(t *testing.T, target string) *http.Response {
	t.Helper()

	dial := func(addr string) (*hdhomerun.Client, error) {
		t.Logf("target: %s", addr)
		return nil, errors.New("always fails")
	}

	s := httptest.NewServer(hdhomerunexporter.NewHandler(dial))
	defer s.Close()

	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

	if target != "" {
		q := u.Query()
		q.Set("target", target)
		u.RawQuery = q.Encode()
	}

	res, err := http.Get(u.String())
	if err != nil {
		t.Fatalf("failed to perform HTTP request: %v", err)
	}

	return res
}
