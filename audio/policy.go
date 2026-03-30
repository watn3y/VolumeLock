//go:build windows

package audio

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

var (
	// Undocumented IPolicyConfig COM interface for changing the default audio device.
	clsidPolicyConfigClient = ole.NewGUID("{870AF99C-171D-4F9E-AF0D-E63DF40C2BC9}")
	iidIPolicyConfig        = ole.NewGUID("{F8679F50-850A-41CF-9C72-430F290290C8}")
)

// Partial vtable, we only need SetDefaultEndpoint.
type iPolicyConfig struct {
	ole.IUnknown
}

type iPolicyConfigVtbl struct {
	ole.IUnknownVtbl
	GetMixFormat          uintptr
	GetDeviceFormat       uintptr
	ResetDeviceFormat     uintptr
	SetDeviceFormat       uintptr
	GetProcessingPeriod   uintptr
	SetProcessingPeriod   uintptr
	GetShareMode          uintptr
	SetShareMode          uintptr
	GetPropertyValue      uintptr
	SetPropertyValue      uintptr
	SetDefaultEndpoint    uintptr
	SetEndpointVisibility uintptr
}

func (v *iPolicyConfig) vtbl() *iPolicyConfigVtbl {
	return (*iPolicyConfigVtbl)(unsafe.Pointer(v.RawVTable))
}

func (m *Manager) GetDefaultDevice(dir Direction, role uint32) (Device, error) {
	var endpoint *wca.IMMDevice
	if err := m.enumerator.GetDefaultAudioEndpoint(eDataFlow(dir), role, &endpoint); err != nil {
		return Device{}, fmt.Errorf("GetDefaultAudioEndpoint: %w", err)
	}
	defer endpoint.Release()
	return deviceFromEndpoint(endpoint, dir)
}

func (m *Manager) SetDefaultDevice(deviceID string) error {
	return m.setRoles(deviceID, wca.EConsole, wca.EMultimedia)
}

func (m *Manager) SetDefaultCommsDevice(deviceID string) error {
	return m.setRoles(deviceID, wca.ECommunications)
}

func (m *Manager) setRoles(deviceID string, roles ...uint32) error {
	var pc *iPolicyConfig
	if err := wca.CoCreateInstance(
		clsidPolicyConfigClient, 0, wca.CLSCTX_ALL, iidIPolicyConfig, &pc,
	); err != nil {
		return fmt.Errorf("CoCreateInstance(IPolicyConfig): %w", err)
	}
	defer pc.Release()

	devIDW, err := syscall.UTF16PtrFromString(deviceID)
	if err != nil {
		return fmt.Errorf("UTF16PtrFromString: %w", err)
	}

	for _, role := range roles {
		hr, _, _ := syscall.Syscall(
			pc.vtbl().SetDefaultEndpoint,
			3,
			uintptr(unsafe.Pointer(pc)),
			uintptr(unsafe.Pointer(devIDW)),
			uintptr(role),
		)
		if hr != 0 {
			return fmt.Errorf("SetDefaultEndpoint(role=%d): %w", role, ole.NewError(hr))
		}
	}
	return nil
}
