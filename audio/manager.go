package audio

import (
	"fmt"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

// Must be created and used from a goroutine locked to an OS thread
// (runtime.LockOSThread) which is a COM apartment threading requirement.
type Manager struct {
	enumerator *wca.IMMDeviceEnumerator
}

func NewManager() (*Manager, error) {
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		// S_FALSE is fine, this means COM is already initialised on this thread.
		if !isCOMSuccess(err) {
			return nil, fmt.Errorf("CoInitializeEx: %w", err)
		}
	}

	var enumerator *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(
		wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL,
		wca.IID_IMMDeviceEnumerator, &enumerator,
	); err != nil {
		ole.CoUninitialize()
		return nil, fmt.Errorf("create IMMDeviceEnumerator: %w", err)
	}

	return &Manager{enumerator: enumerator}, nil
}

func (m *Manager) Close() {
	if m.enumerator != nil {
		m.enumerator.Release()
		m.enumerator = nil
	}
	ole.CoUninitialize()
}

func isCOMSuccess(err error) bool {
	oleErr, ok := err.(*ole.OleError)
	return ok && oleErr.Code() < 0x80000000
}
