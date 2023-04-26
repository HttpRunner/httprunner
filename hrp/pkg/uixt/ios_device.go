package uixt

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"io"
	builtinLog "log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/httprunner/httprunner/v4/hrp/internal/code"
	"github.com/httprunner/httprunner/v4/hrp/internal/env"
	"github.com/httprunner/httprunner/v4/hrp/pkg/gidevice"
)

const (
	defaultWDAPort   = 8100
	defaultMjpegPort = 9100
)

const (
	// Changes the value of maximum depth for traversing elements source tree.
	// It may help to prevent out of memory or timeout errors while getting the elements source tree,
	// but it might restrict the depth of source tree.
	// A part of elements source tree might be lost if the value was too small. Defaults to 50
	snapshotMaxDepth = 10
	// Allows to customize accept/dismiss alert button selector.
	// It helps you to handle an arbitrary element as accept button in accept alert command.
	// The selector should be a valid class chain expression, where the search root is the alert element itself.
	// The default button location algorithm is used if the provided selector is wrong or does not match any element.
	// e.g. **/XCUIElementTypeButton[`label CONTAINS[c] ‘accept’`]
	acceptAlertButtonSelector  = "**/XCUIElementTypeButton[`label IN {'允许','好','仅在使用应用期间','稍后再说'}`]"
	dismissAlertButtonSelector = "**/XCUIElementTypeButton[`label IN {'不允许','暂不'}`]"
)

type IOSPerfOption = gidevice.PerfOption

var (
	WithIOSPerfSystemCPU         = gidevice.WithPerfSystemCPU
	WithIOSPerfSystemMem         = gidevice.WithPerfSystemMem
	WithIOSPerfSystemDisk        = gidevice.WithPerfSystemDisk
	WithIOSPerfSystemNetwork     = gidevice.WithPerfSystemNetwork
	WithIOSPerfGPU               = gidevice.WithPerfGPU
	WithIOSPerfFPS               = gidevice.WithPerfFPS
	WithIOSPerfNetwork           = gidevice.WithPerfNetwork
	WithIOSPerfBundleID          = gidevice.WithPerfBundleID
	WithIOSPerfPID               = gidevice.WithPerfPID
	WithIOSPerfOutputInterval    = gidevice.WithPerfOutputInterval
	WithIOSPerfProcessAttributes = gidevice.WithPerfProcessAttributes
	WithIOSPerfSystemAttributes  = gidevice.WithPerfSystemAttributes
)

type IOSPcapOption = gidevice.PcapOption

var (
	WithIOSPcapAll      = gidevice.WithPcapAll
	WithIOSPcapPID      = gidevice.WithPcapPID
	WithIOSPcapProcName = gidevice.WithPcapProcName
	WithIOSPcapBundleID = gidevice.WithPcapBundleID
)

type IOSDeviceOption func(*IOSDevice)

func WithUDID(udid string) IOSDeviceOption {
	return func(device *IOSDevice) {
		device.UDID = udid
	}
}

func WithWDAPort(port int) IOSDeviceOption {
	return func(device *IOSDevice) {
		device.Port = port
	}
}

func WithWDAMjpegPort(port int) IOSDeviceOption {
	return func(device *IOSDevice) {
		device.MjpegPort = port
	}
}

func WithWDALogOn(logOn bool) IOSDeviceOption {
	return func(device *IOSDevice) {
		device.LogOn = logOn
	}
}

func WithResetHomeOnStartup(reset bool) IOSDeviceOption {
	return func(device *IOSDevice) {
		device.ResetHomeOnStartup = reset
	}
}

func WithSnapshotMaxDepth(depth int) IOSDeviceOption {
	return func(device *IOSDevice) {
		device.SnapshotMaxDepth = depth
	}
}

func WithAcceptAlertButtonSelector(selector string) IOSDeviceOption {
	return func(device *IOSDevice) {
		device.AcceptAlertButtonSelector = selector
	}
}

func WithDismissAlertButtonSelector(selector string) IOSDeviceOption {
	return func(device *IOSDevice) {
		device.DismissAlertButtonSelector = selector
	}
}

func WithXCTest(bundleID string) IOSDeviceOption {
	return func(device *IOSDevice) {
		device.XCTestBundleID = bundleID
	}
}
func WithClosePopup(isTrue bool) IOSDeviceOption {
	return func(device *IOSDevice) {
		device.ClosePopup = isTrue
	}
}
func WithIOSPerfOptions(options ...gidevice.PerfOption) IOSDeviceOption {
	return func(device *IOSDevice) {
		device.PerfOptions = &gidevice.PerfOptions{}
		for _, option := range options {
			option(device.PerfOptions)
		}
	}
}

func WithIOSPcapOptions(options ...gidevice.PcapOption) IOSDeviceOption {
	return func(device *IOSDevice) {
		device.PcapOptions = &gidevice.PcapOptions{}
		for _, option := range options {
			option(device.PcapOptions)
		}
	}
}

func GetIOSDevices(udid ...string) (devices []gidevice.Device, err error) {
	var usbmux gidevice.Usbmux
	if usbmux, err = gidevice.NewUsbmux(); err != nil {
		return nil, errors.Wrap(code.IOSDeviceConnectionError,
			fmt.Sprintf("init usbmux failed: %v", err))
	}

	if devices, err = usbmux.Devices(); err != nil {
		return nil, errors.Wrap(code.IOSDeviceConnectionError,
			fmt.Sprintf("list ios devices failed: %v", err))
	}

	// filter by udid
	var deviceList []gidevice.Device
	for _, d := range devices {
		for _, u := range udid {
			if u != "" && u != d.Properties().SerialNumber {
				continue
			}
			// filter non-usb ios devices
			if d.Properties().ConnectionType != "USB" {
				continue
			}
			deviceList = append(deviceList, d)
		}
	}

	if len(deviceList) == 0 {
		var err error
		if udid == nil || (len(udid) == 1 && udid[0] == "") {
			err = fmt.Errorf("no ios device found")
		} else {
			err = fmt.Errorf("no ios device found for udid %v", udid)
		}
		return nil, err
	}
	return deviceList, nil
}

func GetIOSDeviceOptions(dev *IOSDevice) (deviceOptions []IOSDeviceOption) {
	if dev.UDID != "" {
		deviceOptions = append(deviceOptions, WithUDID(dev.UDID))
	}
	if dev.Port != 0 {
		deviceOptions = append(deviceOptions, WithWDAPort(dev.Port))
	}
	if dev.MjpegPort != 0 {
		deviceOptions = append(deviceOptions, WithWDAMjpegPort(dev.MjpegPort))
	}
	if dev.LogOn {
		deviceOptions = append(deviceOptions, WithWDALogOn(true))
	}
	if dev.PerfOptions != nil {
		deviceOptions = append(deviceOptions, WithIOSPerfOptions(dev.perfOpitons()...))
	}
	if dev.PcapOptions != nil {
		deviceOptions = append(deviceOptions, WithIOSPcapOptions(dev.pcapOpitons()...))
	}
	if dev.XCTestBundleID != "" {
		deviceOptions = append(deviceOptions, WithXCTest(dev.XCTestBundleID))
	}
	if dev.ResetHomeOnStartup {
		deviceOptions = append(deviceOptions, WithResetHomeOnStartup(true))
	}
	if dev.SnapshotMaxDepth != 0 {
		deviceOptions = append(deviceOptions, WithSnapshotMaxDepth(dev.SnapshotMaxDepth))
	}
	if dev.AcceptAlertButtonSelector != "" {
		deviceOptions = append(deviceOptions, WithAcceptAlertButtonSelector(dev.AcceptAlertButtonSelector))
	}
	if dev.DismissAlertButtonSelector != "" {
		deviceOptions = append(deviceOptions, WithDismissAlertButtonSelector(dev.DismissAlertButtonSelector))
	}
	if dev.ClosePopup {
		deviceOptions = append(deviceOptions, WithClosePopup(true))
	}
	return
}

func NewIOSDevice(options ...IOSDeviceOption) (device *IOSDevice, err error) {
	device = &IOSDevice{
		Port:                       defaultWDAPort,
		MjpegPort:                  defaultMjpegPort,
		SnapshotMaxDepth:           snapshotMaxDepth,
		AcceptAlertButtonSelector:  acceptAlertButtonSelector,
		DismissAlertButtonSelector: dismissAlertButtonSelector,
		// switch to iOS springboard before init WDA session
		// avoid getting stuck when some super app is active such as douyin or wexin
		ResetHomeOnStartup: true,
	}
	for _, option := range options {
		option(device)
	}

	deviceList, err := GetIOSDevices(device.UDID)
	if err != nil {
		return nil, errors.Wrap(code.IOSDeviceConnectionError, err.Error())
	}

	dev := deviceList[0]
	udid := dev.Properties().SerialNumber
	device.UDID = udid
	device.d = dev

	// run xctest if XCTestBundleID is set
	if device.XCTestBundleID != "" {
		_, err = device.RunXCTest(device.XCTestBundleID)
		if err != nil {
			log.Error().Err(err).Str("udid", udid).Msg("failed to init XCTest")
			return
		}
	}

	log.Info().Str("udid", device.UDID).Msg("select ios device")
	return device, nil
}

type IOSDevice struct {
	d              gidevice.Device
	PerfOptions    *gidevice.PerfOptions `json:"perf_options,omitempty" yaml:"perf_options,omitempty"`
	PcapOptions    *gidevice.PcapOptions `json:"pcap_options,omitempty" yaml:"pcap_options,omitempty"`
	UDID           string                `json:"udid,omitempty" yaml:"udid,omitempty"`
	Port           int                   `json:"port,omitempty" yaml:"port,omitempty"`             // WDA remote port
	MjpegPort      int                   `json:"mjpeg_port,omitempty" yaml:"mjpeg_port,omitempty"` // WDA remote MJPEG port
	LogOn          bool                  `json:"log_on,omitempty" yaml:"log_on,omitempty"`
	XCTestBundleID string                `json:"xctest_bundle_id,omitempty" yaml:"xctest_bundle_id,omitempty"`

	// switch to iOS springboard before init WDA session
	ResetHomeOnStartup bool `json:"reset_home_on_startup,omitempty" yaml:"reset_home_on_startup,omitempty"`

	// config appium settings
	SnapshotMaxDepth           int    `json:"snapshot_max_depth,omitempty" yaml:"snapshot_max_depth,omitempty"`
	AcceptAlertButtonSelector  string `json:"accept_alert_button_selector,omitempty" yaml:"accept_alert_button_selector,omitempty"`
	DismissAlertButtonSelector string `json:"dismiss_alert_button_selector,omitempty" yaml:"dismiss_alert_button_selector,omitempty"`

	// performance monitor
	perfStop chan struct{} // stop performance monitor
	perfFile string        // saved perf file path

	// pcap monitor
	pcapStop chan struct{} // stop pcap monitor
	pcapFile string        // saved pcap file path

	ClosePopup bool `json:"close_popup,omitempty" yaml:"close_popup,omitempty"`
}

func (dev *IOSDevice) UUID() string {
	return dev.UDID
}

func (dev *IOSDevice) LogEnabled() bool {
	return dev.LogOn
}

func (dev *IOSDevice) NewDriver(capabilities Capabilities) (driverExt *DriverExt, err error) {
	// init WDA driver
	if capabilities == nil {
		capabilities = NewCapabilities()
		capabilities.WithDefaultAlertAction(AlertActionAccept)
	}

	var driver WebDriver
	if env.WDA_USB_DRIVER == "" {
		// default use http driver
		driver, err = dev.NewHTTPDriver(capabilities)
	} else {
		driver, err = dev.NewUSBDriver(capabilities)
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to init WDA driver")
	}

	if dev.ResetHomeOnStartup {
		log.Info().Msg("go back to home screen")
		if err = driver.Homescreen(); err != nil {
			return nil, errors.Wrap(code.MobileUIDriverError,
				fmt.Sprintf("go back to home screen failed: %v", err))
		}
	}

	driverExt, err = NewDriverExt(dev, driver)
	if err != nil {
		return nil, err
	}
	err = driverExt.extendCV()
	if err != nil {
		return nil, errors.Wrap(code.MobileUIDriverError,
			fmt.Sprintf("extend OpenCV failed: %v", err))
	}
	settings, err := driverExt.Driver.SetAppiumSettings(map[string]interface{}{
		"snapshotMaxDepth":          dev.SnapshotMaxDepth,
		"acceptAlertButtonSelector": dev.AcceptAlertButtonSelector,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to set appium WDA settings")
	}
	log.Info().Interface("appiumWDASettings", settings).Msg("set appium WDA settings")

	if dev.LogOn {
		err = driverExt.Driver.StartCaptureLog("hrp_wda_log")
		if err != nil {
			return nil, err
		}
	}

	if dev.PerfOptions != nil {
		if err := dev.StartPerf(); err != nil {
			return nil, err
		}
	}

	if dev.PcapOptions != nil {
		if err := dev.StartPcap(); err != nil {
			return nil, err
		}
	}

	driverExt.ClosePopup = dev.ClosePopup

	return driverExt, nil
}

func (dev *IOSDevice) StartPerf() error {
	log.Info().Msg("start performance monitor")
	data, err := dev.d.PerfStart(dev.perfOpitons()...)
	if err != nil {
		return err
	}

	dev.perfFile = filepath.Join(env.ResultsPath, "perf.data")
	file, err := os.OpenFile(dev.perfFile,
		os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}

	dev.perfStop = make(chan struct{})
	// start performance monitor
	go func() {
		for {
			select {
			case <-dev.perfStop:
				file.Close()
				dev.d.PerfStop()
				return
			case d := <-data:
				_, err = file.WriteString(string(d) + "\n")
				if err != nil {
					log.Error().Err(err).
						Str("line", string(d)).
						Msg("write perf data failed")
				}
			}
		}
	}()
	return nil
}

func (dev *IOSDevice) StopPerf() string {
	if dev.perfStop == nil {
		return ""
	}
	close(dev.perfStop)
	log.Info().Str("perfFile", dev.perfFile).Msg("stop performance monitor")
	return dev.perfFile
}

func (dev *IOSDevice) StartPcap() error {
	log.Info().Msg("start packet capture")
	packets, err := dev.d.PcapStart(dev.pcapOpitons()...)
	if err != nil {
		return err
	}

	dev.pcapFile = filepath.Join(env.ResultsPath, "dump.pcap")
	file, err := os.OpenFile(dev.pcapFile,
		os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}

	// pcap magic number
	// https://www.ietf.org/archive/id/draft-gharris-opsawg-pcap-01.html
	_, _ = file.Write([]byte{
		0xd4, 0xc3, 0xb2, 0xa1, 0x02, 0x00, 0x04, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xff, 0xff, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
	})

	dev.pcapStop = make(chan struct{})
	// start pcap monitor
	go func() {
		for {
			select {
			case <-dev.pcapStop:
				file.Close()
				dev.d.PcapStop()
				return
			case d := <-packets:
				_, err = file.Write(d)
				if err != nil {
					log.Error().Err(err).Msg("write pcap data failed")
				}
			}
		}
	}()
	return nil
}

// StopPcap stops pcap monitor and returns the saved pcap file path
func (dev *IOSDevice) StopPcap() string {
	if dev.pcapStop == nil {
		return ""
	}
	close(dev.pcapStop)
	log.Info().Str("pcapFile", dev.pcapFile).Msg("stop packet capture")
	return dev.pcapFile
}

func (dev *IOSDevice) forward(localPort, remotePort int) error {
	log.Info().Int("localPort", localPort).Int("remotePort", remotePort).
		Str("udid", dev.UDID).Msg("forward tcp port")

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", localPort))
	if err != nil {
		log.Error().Err(err).Msg("listen tcp error")
		return err
	}

	go func(listener net.Listener, device gidevice.Device) {
		for {
			accept, err := listener.Accept()
			if err != nil {
				log.Error().Err(err).Msg("accept error")
				continue
			}

			rInnerConn, err := device.NewConnect(remotePort)
			if err != nil {
				log.Error().Err(err).Msg("connect to ios device failed")
				os.Exit(code.GetErrorCode(code.IOSDeviceConnectionError))
			}

			rConn := rInnerConn.RawConn()
			_ = rConn.SetDeadline(time.Time{})

			go func(lConn net.Conn) {
				go func(lConn, rConn net.Conn) {
					if _, err := io.Copy(lConn, rConn); err != nil {
						log.Error().Err(err).Msg("copy local -> remote")
					}
				}(lConn, rConn)
				go func(lConn, rConn net.Conn) {
					if _, err := io.Copy(rConn, lConn); err != nil {
						log.Error().Err(err).Msg("copy local <- remote")
					}
				}(lConn, rConn)
			}(accept)
		}
	}(listener, dev.d)

	return nil
}

func (dev *IOSDevice) perfOpitons() (perfOptions []gidevice.PerfOption) {
	if dev.PerfOptions == nil {
		return
	}

	// system
	if dev.PerfOptions.SysCPU {
		perfOptions = append(perfOptions, gidevice.WithPerfSystemCPU(true))
	}
	if dev.PerfOptions.SysMem {
		perfOptions = append(perfOptions, gidevice.WithPerfSystemMem(true))
	}
	if dev.PerfOptions.SysDisk {
		perfOptions = append(perfOptions, gidevice.WithPerfSystemDisk(true))
	}
	if dev.PerfOptions.SysNetwork {
		perfOptions = append(perfOptions, gidevice.WithPerfSystemNetwork(true))
	}
	if dev.PerfOptions.FPS {
		perfOptions = append(perfOptions, gidevice.WithPerfFPS(true))
	}
	if dev.PerfOptions.Network {
		perfOptions = append(perfOptions, gidevice.WithPerfNetwork(true))
	}

	// process
	if dev.PerfOptions.BundleID != "" {
		perfOptions = append(perfOptions,
			gidevice.WithPerfBundleID(dev.PerfOptions.BundleID))
	}
	if dev.PerfOptions.Pid != 0 {
		perfOptions = append(perfOptions,
			gidevice.WithPerfPID(dev.PerfOptions.Pid))
	}

	// config
	if dev.PerfOptions.OutputInterval != 0 {
		perfOptions = append(perfOptions,
			gidevice.WithPerfOutputInterval(dev.PerfOptions.OutputInterval))
	}
	if dev.PerfOptions.SystemAttributes != nil {
		perfOptions = append(perfOptions,
			gidevice.WithPerfSystemAttributes(dev.PerfOptions.SystemAttributes...))
	}
	if dev.PerfOptions.ProcessAttributes != nil {
		perfOptions = append(perfOptions,
			gidevice.WithPerfProcessAttributes(dev.PerfOptions.ProcessAttributes...))
	}
	return
}

func (dev *IOSDevice) pcapOpitons() (pcapOptions []gidevice.PcapOption) {
	if dev.PcapOptions == nil {
		return
	}

	if dev.PcapOptions.All {
		pcapOptions = append(pcapOptions, gidevice.WithPcapAll(true))
	}
	if dev.PcapOptions.Pid > 0 {
		pcapOptions = append(pcapOptions, gidevice.WithPcapPID(dev.PcapOptions.Pid))
	}
	if dev.PcapOptions.ProcName != "" {
		pcapOptions = append(pcapOptions, gidevice.WithPcapProcName(dev.PcapOptions.ProcName))
	}
	if dev.PcapOptions.BundleID != "" {
		pcapOptions = append(pcapOptions, gidevice.WithPcapBundleID(dev.PcapOptions.BundleID))
	}

	return
}

// NewHTTPDriver creates new remote HTTP client, this will also start a new session.
func (dev *IOSDevice) NewHTTPDriver(capabilities Capabilities) (driver WebDriver, err error) {
	var localPort int
	localPort, err = strconv.Atoi(env.WDA_LOCAL_PORT)
	if err != nil {
		localPort, err = getFreePort()
		if err != nil {
			return nil, errors.Wrap(code.IOSDeviceHTTPDriverError,
				fmt.Sprintf("get free port failed: %v", err))
		}

		if err = dev.forward(localPort, dev.Port); err != nil {
			return nil, errors.Wrap(code.IOSDeviceHTTPDriverError,
				fmt.Sprintf("forward tcp port failed: %v", err))
		}
	} else {
		log.Info().Int("WDA_LOCAL_PORT", localPort).Msg("reuse WDA local port")
	}

	var localMjpegPort int
	localMjpegPort, err = strconv.Atoi(env.WDA_LOCAL_MJPEG_PORT)
	if err != nil {
		localMjpegPort, err = getFreePort()
		if err != nil {
			return nil, errors.Wrap(code.IOSDeviceHTTPDriverError,
				fmt.Sprintf("get free port failed: %v", err))
		}
		if err = dev.forward(localMjpegPort, dev.MjpegPort); err != nil {
			return nil, errors.Wrap(code.IOSDeviceHTTPDriverError,
				fmt.Sprintf("forward tcp port failed: %v", err))
		}
	} else {
		log.Info().Int("WDA_LOCAL_MJPEG_PORT", localMjpegPort).
			Msg("reuse WDA local mjpeg port")
	}

	log.Info().Interface("capabilities", capabilities).
		Int("localPort", localPort).Int("localMjpegPort", localMjpegPort).
		Msg("init WDA HTTP driver")

	wd := new(wdaDriver)
	wd.client = http.DefaultClient

	host := "127.0.0.1"
	if wd.urlPrefix, err = url.Parse(fmt.Sprintf("http://%s:%d", host, localPort)); err != nil {
		return nil, errors.Wrap(code.IOSDeviceHTTPDriverError, err.Error())
	}
	var sessionInfo SessionInfo
	if sessionInfo, err = wd.NewSession(capabilities); err != nil {
		return nil, errors.Wrap(code.IOSDeviceHTTPDriverError, err.Error())
	}
	wd.sessionId = sessionInfo.SessionId

	if wd.mjpegHTTPConn, err = net.Dial(
		"tcp",
		fmt.Sprintf("%s:%d", host, localMjpegPort),
	); err != nil {
		return nil, errors.Wrap(code.IOSDeviceHTTPDriverError, err.Error())
	}
	wd.mjpegClient = convertToHTTPClient(wd.mjpegHTTPConn)

	return wd, nil
}

// NewUSBDriver creates new client via USB connected device, this will also start a new session.
func (dev *IOSDevice) NewUSBDriver(capabilities Capabilities) (driver WebDriver, err error) {
	log.Info().Interface("capabilities", capabilities).
		Str("udid", dev.UDID).Msg("init WDA USB driver")

	wd := new(wdaDriver)
	if wd.defaultConn, err = dev.d.NewConnect(dev.Port, 0); err != nil {
		return nil, errors.Wrap(code.IOSDeviceUSBDriverError,
			fmt.Sprintf("connect port %d failed: %v", dev.Port, err))
	}
	wd.client = convertToHTTPClient(wd.defaultConn.RawConn())

	if wd.mjpegUSBConn, err = dev.d.NewConnect(dev.MjpegPort, 0); err != nil {
		return nil, errors.Wrap(code.IOSDeviceUSBDriverError,
			fmt.Sprintf("connect MJPEG port %d failed: %v", dev.MjpegPort, err))
	}
	wd.mjpegClient = convertToHTTPClient(wd.mjpegUSBConn.RawConn())

	if wd.urlPrefix, err = url.Parse("http://" + dev.UDID); err != nil {
		return nil, errors.Wrap(code.IOSDeviceUSBDriverError, err.Error())
	}
	if _, err = wd.NewSession(capabilities); err != nil {
		return nil, errors.Wrap(code.IOSDeviceUSBDriverError, err.Error())
	}

	return wd, nil
}

func (dev *IOSDevice) RunXCTest(bundleID string) (cancel context.CancelFunc, err error) {
	log.Info().Str("bundleID", bundleID).Msg("run xctest")
	out, cancel, err := dev.d.XCTest(bundleID)
	if err != nil {
		return nil, errors.Wrap(err, "run xctest failed")
	}
	// wait for xctest to start
	time.Sleep(5 * time.Second)

	f, err := os.OpenFile(fmt.Sprintf("xctest_%s.log", dev.UDID),
		os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o666)
	if err != nil {
		return nil, err
	}
	defer builtinLog.SetOutput(f)

	// print xctest running logs
	go func() {
		for s := range out {
			builtinLog.Print(s)
		}
		f.Close()
	}()

	return cancel, nil
}
