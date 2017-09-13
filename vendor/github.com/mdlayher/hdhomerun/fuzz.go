// +build gofuzz

package hdhomerun

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
)

func Fuzz(data []byte) int {
	p := new(Packet)
	if err := p.UnmarshalBinary(data); err != nil {
		return 0
	}

	pb, err := p.MarshalBinary()
	if err != nil {
		panic(err)
	}

	if diff := cmp.Diff(data, pb); diff != "" {
		panic(fmt.Sprintf("unexpected output bytes (-want +got):\n%s", diff))
	}

	return 1
}
