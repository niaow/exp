// +build amd64 386

package cpu

import (
	"testing"

	"github.com/klauspost/cpuid"
)

func TestCores(t *testing.T) {
	cores, err := ListCores()
	if err != nil {
		t.Fatalf("failed to enumerate cores: %v", err)
	}
	coresFound := map[int]struct{}{}
	for i, c := range cores {
		var corenum int
		ch := make(chan func(Core), 1)
		ch <- func(_ Core) { corenum = cpuid.CPU.LogicalCPU() }
		close(ch)
		err = c.Run(ch)
		if err != nil {
			t.Fatalf("failed to run on core %d: %v", i, err)
		}
		coresFound[corenum] = struct{}{}
	}
	if len(coresFound) != len(cores) {
		t.Fatalf("listed %d cores but found %d cores", len(cores), len(coresFound))
	}
}
