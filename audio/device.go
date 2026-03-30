package audio

import (
	"fmt"
	"strings"

	"github.com/moutend/go-wca/pkg/wca"
)

type Direction int

const (
	Output Direction = iota
	Input
)

func (d Direction) String() string {
	switch d {
	case Output:
		return "Output"
	case Input:
		return "Input"
	default:
		return "Unknown"
	}
}

type Device struct {
	ID        string
	Name      string
	Direction Direction
}

func (m *Manager) ListDevices() ([]Device, error) {
	var all []Device
	for _, dir := range []Direction{Output, Input} {
		found, err := m.listByDirection(dir)
		if err != nil {
			return nil, err
		}
		all = append(all, found...)
	}
	return all, nil
}

func (m *Manager) listByDirection(dir Direction) ([]Device, error) {
	var col *wca.IMMDeviceCollection
	if err := m.enumerator.EnumAudioEndpoints(
		eDataFlow(dir), wca.DEVICE_STATE_ACTIVE, &col,
	); err != nil {
		return nil, fmt.Errorf("EnumAudioEndpoints(%s): %w", dir, err)
	}
	defer col.Release()

	var count uint32
	if err := col.GetCount(&count); err != nil {
		return nil, fmt.Errorf("GetCount: %w", err)
	}

	devices := make([]Device, 0, count)
	for i := uint32(0); i < count; i++ {
		var endpoint *wca.IMMDevice
		if err := col.Item(i, &endpoint); err != nil {
			return nil, fmt.Errorf("Item(%d): %w", i, err)
		}

		dev, err := deviceFromEndpoint(endpoint, dir)
		endpoint.Release() // always release, even on error
		if err != nil {
			return nil, err
		}
		devices = append(devices, dev)
	}
	return devices, nil
}

func deviceFromEndpoint(endpoint *wca.IMMDevice, dir Direction) (Device, error) {
	var id string
	if err := endpoint.GetId(&id); err != nil {
		return Device{}, fmt.Errorf("GetId: %w", err)
	}

	name, err := deviceFriendlyName(endpoint)
	if err != nil {
		name = id // fall back to raw endpoint ID
	}
	if start := strings.Index(name, "("); start != -1 {
		if end := strings.LastIndex(name, ")"); end > start {
			name = name[start+1 : end]
		}
	}
	return Device{ID: id, Name: name, Direction: dir}, nil
}

func deviceFriendlyName(endpoint *wca.IMMDevice) (string, error) {
	var ps *wca.IPropertyStore
	if err := endpoint.OpenPropertyStore(wca.STGM_READ, &ps); err != nil {
		return "", fmt.Errorf("OpenPropertyStore: %w", err)
	}
	defer ps.Release()

	var pv wca.PROPVARIANT
	if err := ps.GetValue(&wca.PKEY_Device_FriendlyName, &pv); err != nil {
		return "", fmt.Errorf("GetValue(FriendlyName): %w", err)
	}
	// Technically leaks the PROPVARIANT string, doesn't matter for our usage.
	return pv.String(), nil
}

func (m *Manager) GetVolume(deviceID string) (float32, error) {
	av, release, err := m.openEndpointVolume(deviceID)
	if err != nil {
		return 0, err
	}
	defer release()

	var level float32
	if err := av.GetMasterVolumeLevelScalar(&level); err != nil {
		return 0, fmt.Errorf("GetMasterVolumeLevelScalar: %w", err)
	}
	return level, nil
}

func (m *Manager) SetVolume(deviceID string, level float32) error {
	av, release, err := m.openEndpointVolume(deviceID)
	if err != nil {
		return err
	}
	defer release()

	if err := av.SetMasterVolumeLevelScalar(level, nil); err != nil {
		return fmt.Errorf("SetMasterVolumeLevelScalar: %w", err)
	}
	return nil
}

func (m *Manager) openEndpointVolume(deviceID string) (*wca.IAudioEndpointVolume, func(), error) {
	// go-wca's GetDevice() is a stub, so we find the endpoint manually.
	endpoint, err := m.findEndpointByID(deviceID)
	if err != nil {
		return nil, nil, err
	}

	var av *wca.IAudioEndpointVolume
	if err := endpoint.Activate(
		wca.IID_IAudioEndpointVolume, wca.CLSCTX_ALL, nil, &av,
	); err != nil {
		endpoint.Release()
		return nil, nil, fmt.Errorf("Activate(IAudioEndpointVolume) on %s: %w", deviceID, err)
	}
	endpoint.Release()

	return av, func() { av.Release() }, nil
}

func (m *Manager) findEndpointByID(deviceID string) (*wca.IMMDevice, error) {
	for _, df := range []uint32{wca.ERender, wca.ECapture} {
		var col *wca.IMMDeviceCollection
		if err := m.enumerator.EnumAudioEndpoints(df, wca.DEVICE_STATE_ACTIVE, &col); err != nil {
			continue
		}

		var count uint32
		col.GetCount(&count)

		for i := uint32(0); i < count; i++ {
			var ep *wca.IMMDevice
			if err := col.Item(i, &ep); err != nil {
				continue
			}

			var id string
			if err := ep.GetId(&id); err != nil {
				ep.Release()
				continue
			}

			if id == deviceID {
				col.Release()
				return ep, nil // caller owns this reference
			}
			ep.Release()
		}
		col.Release()
	}
	return nil, fmt.Errorf("device not found: %s", deviceID)
}

func eDataFlow(d Direction) uint32 {
	if d == Input {
		return wca.ECapture
	}
	return wca.ERender
}
