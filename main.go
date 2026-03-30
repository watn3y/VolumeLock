package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"github.com/moutend/go-wca/pkg/wca"
	"watn3y/VolumeLock/audio"
)

type lockState struct {
	mu                sync.Mutex
	outputEnabled     bool
	outputDevID       string
	outputTarget      float32
	outputLockDefault bool
	outputLockComms   bool
	inputEnabled      bool
	inputDevID        string
	inputTarget       float32
	inputLockDefault  bool
	inputLockComms    bool
}

// Needs its own Manager — COM objects can't cross OS threads.
func runEnforcer(ctx context.Context, state *lockState) {
	runtime.LockOSThread()

	mgr, err := audio.NewManager()
	if err != nil {
		log.Printf("enforcer NewManager: %v", err)
		return
	}
	defer mgr.Close()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			state.mu.Lock()
			outEnabled := state.outputEnabled
			outID := state.outputDevID
			outTarget := state.outputTarget
			outDef := state.outputLockDefault
			outComms := state.outputLockComms
			inEnabled := state.inputEnabled
			inID := state.inputDevID
			inTarget := state.inputTarget
			inDef := state.inputLockDefault
			inComms := state.inputLockComms
			state.mu.Unlock()

			if outEnabled && outID != "" {
				enforceVolume(mgr, outID, outTarget, "output")
				if outDef {
					enforceDefault(mgr, outID, audio.Output, wca.EConsole, "output default", mgr.SetDefaultDevice)
				}
				if outComms {
					enforceDefault(mgr, outID, audio.Output, wca.ECommunications, "output comms", mgr.SetDefaultCommsDevice)
				}
			}
			if inEnabled && inID != "" {
				enforceVolume(mgr, inID, inTarget, "input")
				if inDef {
					enforceDefault(mgr, inID, audio.Input, wca.EConsole, "input default", mgr.SetDefaultDevice)
				}
				if inComms {
					enforceDefault(mgr, inID, audio.Input, wca.ECommunications, "input comms", mgr.SetDefaultCommsDevice)
				}
			}
		}
	}
}

func enforceVolume(mgr *audio.Manager, deviceID string, target float32, label string) {
	current, err := mgr.GetVolume(deviceID)
	if err != nil {
		log.Printf("GetVolume %s: %v", label, err)
		return
	}
	d := current - target
	if d > 0.005 || d < -0.005 {
		if err := mgr.SetVolume(deviceID, target); err != nil {
			log.Printf("SetVolume %s: %v", label, err)
		}
	}
}

func enforceDefault(mgr *audio.Manager, deviceID string, dir audio.Direction, role uint32, label string, set func(string) error) {
	current, err := mgr.GetDefaultDevice(dir, role)
	if err != nil || current.ID != deviceID {
		if err := set(deviceID); err != nil {
			log.Printf("SetDefault %s: %v", label, err)
		}
	}
}

func defaultDeviceIndex(mgr *audio.Manager, dir audio.Direction, devices []audio.Device) int {
	def, err := mgr.GetDefaultDevice(dir, wca.EConsole)
	if err != nil {
		return 0
	}
	for i, d := range devices {
		if d.ID == def.ID {
			return i
		}
	}
	return 0
}

func main() {
	startToTray := flag.Bool("tray", false, "start minimized to system tray")
	flag.Parse()

	runtime.LockOSThread()

	mgr, err := audio.NewManager()
	if err != nil {
		log.Fatalf("audio.NewManager: %v", err)
	}
	defer mgr.Close()

	devices, err := mgr.ListDevices()
	if err != nil {
		log.Fatalf("ListDevices: %v", err)
	}

	var outputDevices, inputDevices []audio.Device
	for _, d := range devices {
		if d.Direction == audio.Output {
			outputDevices = append(outputDevices, d)
		} else {
			inputDevices = append(inputDevices, d)
		}
	}

	outputNames := make([]string, len(outputDevices))
	for i, d := range outputDevices {
		outputNames[i] = d.Name
	}
	inputNames := make([]string, len(inputDevices))
	for i, d := range inputDevices {
		inputNames[i] = d.Name
	}

	outputDefaultIdx := defaultDeviceIndex(mgr, audio.Output, outputDevices)
	inputDefaultIdx := defaultDeviceIndex(mgr, audio.Input, inputDevices)

	state := &lockState{outputTarget: 0.50, inputTarget: 0.80}
	if len(outputDevices) > 0 {
		state.outputDevID = outputDevices[outputDefaultIdx].ID
	}
	if len(inputDevices) > 0 {
		state.inputDevID = inputDevices[inputDefaultIdx].ID
	}

	var (
		outputEnabledCheck *walk.CheckBox
		outputCombo        *walk.ComboBox
		outputSlider       *walk.Slider
		outputLabel        *walk.Label
		outputDefCheck     *walk.CheckBox
		outputCommsCheck   *walk.CheckBox
		inputEnabledCheck  *walk.CheckBox
		inputCombo         *walk.ComboBox
		inputSlider        *walk.Slider
		inputLabel         *walk.Label
		inputDefCheck      *walk.CheckBox
		inputCommsCheck    *walk.CheckBox
		startBtn           *walk.PushButton
		statusLabel        *walk.Label
	)

	var enforcerCancel context.CancelFunc

	refreshDevices := func() {
		devs, err := mgr.ListDevices()
		if err != nil {
			log.Printf("refresh ListDevices: %v", err)
			return
		}

		var newOut, newIn []audio.Device
		for _, d := range devs {
			if d.Direction == audio.Output {
				newOut = append(newOut, d)
			} else {
				newIn = append(newIn, d)
			}
		}

		outNames := make([]string, len(newOut))
		for i, d := range newOut {
			outNames[i] = d.Name
		}
		inNames := make([]string, len(newIn))
		for i, d := range newIn {
			inNames[i] = d.Name
		}

		outputDevices = newOut
		inputDevices = newIn

		outIdx := defaultDeviceIndex(mgr, audio.Output, newOut)
		inIdx := defaultDeviceIndex(mgr, audio.Input, newIn)

		outputCombo.SetModel(outNames)
		outputCombo.SetCurrentIndex(outIdx)
		inputCombo.SetModel(inNames)
		inputCombo.SetCurrentIndex(inIdx)

		state.mu.Lock()
		if len(newOut) > 0 {
			state.outputDevID = newOut[outIdx].ID
		}
		if len(newIn) > 0 {
			state.inputDevID = newIn[inIdx].ID
		}
		state.mu.Unlock()
	}

	var mw *walk.MainWindow

	sectionFont := Font{Family: "Segoe UI", PointSize: 9, Bold: true}
	innerLayout := VBox{Margins: Margins{Left: 10}, Spacing: 3}

	mwDecl := MainWindow{
		AssignTo: &mw,
		Visible:  false, // shown after post-create setup below
		Title:    "VolumeLock",
		Size:     Size{Width: 1, Height: 1}, // Walk auto-grows to layout minimum
		Layout:   VBox{Margins: Margins{Top: 10, Bottom: 10, Left: 10, Right: 10}, Spacing: 4},
		Children: []Widget{

			// Speakers
			CheckBox{
				AssignTo:  &outputEnabledCheck,
				Text:      "Speakers",
				Font:      sectionFont,
				Alignment: AlignHNearVNear,
				OnCheckedChanged: func() {
					state.mu.Lock()
					state.outputEnabled = outputEnabledCheck.Checked()
					state.mu.Unlock()
				},
			},
			Composite{
				Layout: innerLayout,
				Children: []Widget{
					ComboBox{
						AssignTo:     &outputCombo,
						Model:        outputNames,
						CurrentIndex: outputDefaultIdx,
						OnCurrentIndexChanged: func() {
							i := outputCombo.CurrentIndex()
							if i >= 0 && i < len(outputDevices) {
								state.mu.Lock()
								state.outputDevID = outputDevices[i].ID
								state.mu.Unlock()
							}
						},
					},
					Composite{
						Layout: HBox{MarginsZero: true, Spacing: 4},
						Children: []Widget{
							Slider{
								AssignTo: &outputSlider,
								MinValue: 0,
								MaxValue: 100,
								Value:    50,
								OnValueChanged: func() {
									v := outputSlider.Value()
									outputLabel.SetText(fmt.Sprintf("%d%%", v))
									state.mu.Lock()
									state.outputTarget = float32(v) / 100.0
									state.mu.Unlock()
								},
							},
							Label{AssignTo: &outputLabel, Text: "50%", MinSize: Size{Width: 32}},
						},
					},
					CheckBox{
						AssignTo:  &outputDefCheck,
						Text:      "Default Device",
						Alignment: AlignHNearVNear,
						OnCheckedChanged: func() {
							state.mu.Lock()
							state.outputLockDefault = outputDefCheck.Checked()
							state.mu.Unlock()
						},
					},
					CheckBox{
						AssignTo:  &outputCommsCheck,
						Text:      "Default Communication Device",
						Alignment: AlignHNearVNear,
						OnCheckedChanged: func() {
							state.mu.Lock()
							state.outputLockComms = outputCommsCheck.Checked()
							state.mu.Unlock()
						},
					},
				},
			},

			VSpacer{Size: 4},

			// Microphone
			CheckBox{
				AssignTo:  &inputEnabledCheck,
				Text:      "Microphone",
				Font:      sectionFont,
				Alignment: AlignHNearVNear,
				OnCheckedChanged: func() {
					state.mu.Lock()
					state.inputEnabled = inputEnabledCheck.Checked()
					state.mu.Unlock()
				},
			},
			Composite{
				Layout: innerLayout,
				Children: []Widget{
					ComboBox{
						AssignTo:     &inputCombo,
						Model:        inputNames,
						CurrentIndex: inputDefaultIdx,
						OnCurrentIndexChanged: func() {
							i := inputCombo.CurrentIndex()
							if i >= 0 && i < len(inputDevices) {
								state.mu.Lock()
								state.inputDevID = inputDevices[i].ID
								state.mu.Unlock()
							}
						},
					},
					Composite{
						Layout: HBox{MarginsZero: true, Spacing: 4},
						Children: []Widget{
							Slider{
								AssignTo: &inputSlider,
								MinValue: 0,
								MaxValue: 100,
								Value:    80,
								OnValueChanged: func() {
									v := inputSlider.Value()
									inputLabel.SetText(fmt.Sprintf("%d%%", v))
									state.mu.Lock()
									state.inputTarget = float32(v) / 100.0
									state.mu.Unlock()
								},
							},
							Label{AssignTo: &inputLabel, Text: "80%", MinSize: Size{Width: 32}},
						},
					},
					CheckBox{
						AssignTo:  &inputDefCheck,
						Text:      "Default Device",
						Alignment: AlignHNearVNear,
						OnCheckedChanged: func() {
							state.mu.Lock()
							state.inputLockDefault = inputDefCheck.Checked()
							state.mu.Unlock()
						},
					},
					CheckBox{
						AssignTo:  &inputCommsCheck,
						Text:      "Default Communication Device",
						Alignment: AlignHNearVNear,
						OnCheckedChanged: func() {
							state.mu.Lock()
							state.inputLockComms = inputCommsCheck.Checked()
							state.mu.Unlock()
						},
					},
				},
			},

			VSpacer{Size: 4},

			// Controls
			PushButton{
				Text:      "Refresh Devices",
				OnClicked: func() { refreshDevices() },
			},
			PushButton{
				AssignTo: &startBtn,
				Text:     "Start VolumeLock",
				OnClicked: func() {
					if enforcerCancel != nil {
						enforcerCancel()
						enforcerCancel = nil
						startBtn.SetText("Start VolumeLock")
						statusLabel.SetText("● Not running")
						statusLabel.SetTextColor(walk.RGB(200, 0, 0))
					} else {
						ctx, cancel := context.WithCancel(context.Background())
						enforcerCancel = cancel
						go runEnforcer(ctx, state)
						startBtn.SetText("Stop VolumeLock")
						statusLabel.SetText("● Running")
						statusLabel.SetTextColor(walk.RGB(0, 150, 0))
					}
				},
			},
			Label{
				AssignTo:  &statusLabel,
				Text:      "● Not running",
				TextColor: walk.RGB(200, 0, 0),
				Alignment: AlignHNearVNear,
			},
		},
	}

	if err := mwDecl.Create(); err != nil {
		log.Fatal(err)
	}

	// Without a background brush, Windows ignores SetTextColor on static labels.
	if brush, err := walk.NewSystemColorBrush(walk.SysColorBtnFace); err == nil {
		statusLabel.SetBackground(brush)
	}

	// No resize / maximize.
	wndStyle := win.GetWindowLong(mw.Handle(), win.GWL_STYLE)
	win.SetWindowLong(mw.Handle(), win.GWL_STYLE, wndStyle&^(win.WS_MAXIMIZEBOX|win.WS_THICKFRAME))

	// Tray icon.
	ni, err := walk.NewNotifyIcon(mw)
	if err != nil {
		log.Fatal(err)
	}
	defer ni.Dispose()
	ni.SetIcon(walk.IconApplication())
	ni.SetToolTip("VolumeLock")

	showWindow := func() {
		mw.Show()
		win.SetForegroundWindow(mw.Handle())
		ni.SetVisible(false)
	}

	ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			showWindow()
		}
	})

	showAction := walk.NewAction()
	showAction.SetText("Show")
	showAction.Triggered().Attach(showWindow)
	ni.ContextMenu().Actions().Add(showAction)

	exitAction := walk.NewAction()
	exitAction.SetText("Exit")
	exitAction.Triggered().Attach(func() { walk.App().Exit(0) })
	ni.ContextMenu().Actions().Add(exitAction)

	// Minimize → hide to tray instead.
	var oldMWProc uintptr
	mwCallback := syscall.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
		if uint32(msg) == win.WM_SYSCOMMAND && wParam == win.SC_MINIMIZE {
			mw.Hide()
			ni.SetVisible(true)
			return 0
		}
		return win.CallWindowProc(oldMWProc, win.HWND(hwnd), uint32(msg), wParam, lParam)
	})
	oldMWProc = win.SetWindowLongPtr(mw.Handle(), win.GWL_WNDPROC, mwCallback)

	if *startToTray {
		ni.SetVisible(true)
	} else {
		mw.Show()
	}
	mw.Run()
}
