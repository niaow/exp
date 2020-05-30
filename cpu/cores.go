package cpu

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/unix"
)

// Core is a unique identifier for a CPU core.
// BUGS: this may become invalid if the process is migrated to another machine.
type Core struct {
	index uint16 // currerntly limited to 1024 by the OS
}

// Run a series of functions on this CPU core.
func (c Core) Run(ch <-chan func(Core)) (err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Get the old CPU mask.
	var oldmask unix.CPUSet
	err = unix.SchedGetaffinity(0, &oldmask)
	if err != nil {
		return fmt.Errorf("failed to load old CPU mask: %w", err)
	}

	// Pin to the core.
	var newmask unix.CPUSet
	newmask.Set(int(c.index))
	err = unix.SchedSetaffinity(0, &newmask)
	if err != nil {
		return fmt.Errorf("failed to load new CPU mask: %w", err)
	}

	// Revert to the old CPU mask when we are done.
	defer func() {
		rerr := unix.SchedSetaffinity(0, &oldmask)
		if rerr != nil {
			err = fmt.Errorf("failed to load new CPU mask: %w", rerr)
		}
	}()

	for f := range ch {
		f(c)
	}

	return nil
}

// ListCores lists the available CPU cores on the current machine.
func ListCores() ([]Core, error) {
	// TODO: make this more robust
	// TODO: NUMA?
	// TODO: big.LITTLE?
	cores := make([]Core, runtime.NumCPU())
	for i := range cores {
		cores[i] = Core{
			index: uint16(i),
		}
	}
	return cores, nil
}
