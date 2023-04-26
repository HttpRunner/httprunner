package uixt

import (
	"bytes"
	"context"
	"fmt"
	"github.com/httprunner/httprunner/v4/hrp/internal/env"
	"net"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/httprunner/httprunner/v4/hrp/internal/code"
	"github.com/httprunner/httprunner/v4/hrp/internal/json"
	"github.com/httprunner/httprunner/v4/hrp/internal/myexec"
	"github.com/httprunner/httprunner/v4/hrp/pkg/gadb"
)

var (
	AdbServerHost  = "localhost"
	AdbServerPort  = gadb.AdbServerPort // 5037
	UIA2ServerHost = "localhost"
	UIA2ServerPort = 6790
	DeviceTempPath = "/data/local/tmp"
)

const forwardToPrefix = "forward-to-"

type AndroidDeviceOption func(*AndroidDevice)

func WithSerialNumber(serial string) AndroidDeviceOption {
	return func(device *AndroidDevice) {
		device.SerialNumber = serial
	}
}

func WithUIA2(uia2On bool) AndroidDeviceOption {
	return func(device *AndroidDevice) {
		device.UIA2 = uia2On
	}
}

func WithUIA2IP(ip string) AndroidDeviceOption {
	return func(device *AndroidDevice) {
		device.UIA2IP = ip
	}
}

func WithUIA2Port(port int) AndroidDeviceOption {
	return func(device *AndroidDevice) {
		device.UIA2Port = port
	}
}

func WithAdbLogOn(logOn bool) AndroidDeviceOption {
	return func(device *AndroidDevice) {
		device.LogOn = logOn
	}
}

func WithAdbClosePopup(isTrue bool) AndroidDeviceOption {
	return func(device *AndroidDevice) {
		device.ClosePopup = isTrue
	}
}

func GetAndroidDeviceOptions(dev *AndroidDevice) (deviceOptions []AndroidDeviceOption) {
	if dev.SerialNumber != "" {
		deviceOptions = append(deviceOptions, WithSerialNumber(dev.SerialNumber))
	}
	if dev.UIA2 {
		deviceOptions = append(deviceOptions, WithUIA2(true))
	}
	if dev.UIA2IP != "" {
		deviceOptions = append(deviceOptions, WithUIA2IP(dev.UIA2IP))
	}
	if dev.UIA2Port != 0 {
		deviceOptions = append(deviceOptions, WithUIA2Port(dev.UIA2Port))
	}
	if dev.LogOn {
		deviceOptions = append(deviceOptions, WithAdbLogOn(true))
	}
	if dev.ClosePopup {
		deviceOptions = append(deviceOptions, WithAdbClosePopup(true))
	}
	return
}

// uiautomator2 server must be started before
// adb shell am instrument -w io.appium.uiautomator2.server.test/androidx.test.runner.AndroidJUnitRunner
func NewAndroidDevice(options ...AndroidDeviceOption) (device *AndroidDevice, err error) {
	device = &AndroidDevice{
		UIA2IP:   UIA2ServerHost,
		UIA2Port: UIA2ServerPort,
	}
	for _, option := range options {
		option(device)
	}

	deviceList, err := GetAndroidDevices(device.SerialNumber)
	if err != nil {
		return nil, errors.Wrap(code.AndroidDeviceConnectionError, err.Error())
	}

	dev := deviceList[0]
	device.SerialNumber = dev.Serial()
	device.d = dev
	device.logcat = NewAdbLogcat(device.SerialNumber)

	log.Info().Str("serial", device.SerialNumber).Msg("select android device")
	return device, nil
}

func GetAndroidDevices(serial ...string) (devices []*gadb.Device, err error) {
	var adbClient gadb.Client
	if adbClient, err = gadb.NewClientWith(AdbServerHost, AdbServerPort); err != nil {
		return nil, errors.Wrap(code.AndroidDeviceConnectionError, err.Error())
	}

	if devices, err = adbClient.DeviceList(); err != nil {
		return nil, errors.Wrap(code.AndroidDeviceConnectionError,
			fmt.Sprintf("list android devices failed: %v", err))
	}

	var deviceList []*gadb.Device
	// filter by serial
	for _, d := range devices {
		for _, s := range serial {
			if s != "" && s != d.Serial() {
				continue
			}
			deviceList = append(deviceList, d)
		}
	}

	if len(deviceList) == 0 {
		var err error
		if serial == nil || (len(serial) == 1 && serial[0] == "") {
			err = fmt.Errorf("no android device found")
		} else {
			err = fmt.Errorf("no android device found for serial %v", serial)
		}
		return nil, err
	}
	return deviceList, nil
}

type AndroidDevice struct {
	d            *gadb.Device
	logcat       *AdbLogcat
	SerialNumber string `json:"serial,omitempty" yaml:"serial,omitempty"`
	UIA2         bool   `json:"uia2,omitempty" yaml:"uia2,omitempty"`           // use uiautomator2
	UIA2IP       string `json:"uia2_ip,omitempty" yaml:"uia2_ip,omitempty"`     // uiautomator2 server ip
	UIA2Port     int    `json:"uia2_port,omitempty" yaml:"uia2_port,omitempty"` // uiautomator2 server port
	LogOn        bool   `json:"log_on,omitempty" yaml:"log_on,omitempty"`
	ClosePopup   bool   `json:"close_popup,omitempty" yaml:"close_popup,omitempty"`
}

func (dev *AndroidDevice) UUID() string {
	return dev.SerialNumber
}

func (dev *AndroidDevice) LogEnabled() bool {
	return dev.LogOn
}

func (dev *AndroidDevice) NewDriver(capabilities Capabilities) (driverExt *DriverExt, err error) {
	var driver WebDriver
	disableUIA2 := env.DISABLE_UIAUTOMATOR_SERVER == "true"
	if disableUIA2 {
		driver, err = dev.NewAdbDriver()
	} else {
		driver, err = dev.NewUSBDriver(capabilities)
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to init UIA driver")
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

	if dev.LogOn {
		err = driverExt.Driver.StartCaptureLog("hrp_adb_log")
		if err != nil {
			return nil, err
		}
	}

	driverExt.ClosePopup = dev.ClosePopup

	return driverExt, nil
}

// NewUSBDriver creates new client via USB connected device, this will also start a new session.
func (dev *AndroidDevice) NewUSBDriver(capabilities Capabilities) (driver WebDriver, err error) {
	var localPort int
	if localPort, err = getFreePort(); err != nil {
		return nil, errors.Wrap(code.AndroidDeviceUSBDriverError,
			fmt.Sprintf("get free port failed: %v", err))
	}
	if err = dev.d.Forward(localPort, UIA2ServerPort); err != nil {
		return nil, errors.Wrap(code.AndroidDeviceUSBDriverError,
			fmt.Sprintf("forward port %d->%d failed: %v",
				localPort, UIA2ServerPort, err))
	}

	rawURL := fmt.Sprintf("http://%s%d:%d/wd/hub",
		forwardToPrefix, localPort, UIA2ServerPort)
	uiaDriver, err := NewUIADriver(capabilities, rawURL)
	if err != nil {
		_ = dev.d.ForwardKill(localPort)
		return nil, errors.Wrap(code.AndroidDeviceUSBDriverError, err.Error())
	}
	uiaDriver.adbClient = dev.d
	uiaDriver.logcat = dev.logcat

	return uiaDriver, nil
}

// NewHTTPDriver creates new remote HTTP client, this will also start a new session.
func (dev *AndroidDevice) NewHTTPDriver(capabilities Capabilities) (driver WebDriver, err error) {
	rawURL := fmt.Sprintf("http://%s:%d/wd/hub", dev.UIA2IP, dev.UIA2Port)
	uiaDriver, err := NewUIADriver(capabilities, rawURL)
	if err != nil {
		return nil, err
	}

	uiaDriver.adbClient = dev.d
	uiaDriver.logcat = dev.logcat
	return uiaDriver, nil
}

func (dev *AndroidDevice) NewAdbDriver() (driver WebDriver, err error) {
	adbDriver := NewAdbDriver()
	adbDriver.adbClient = dev.d
	adbDriver.logcat = dev.logcat
	return adbDriver, nil
}

func (dev *AndroidDevice) StartPerf() error {
	// TODO
	return nil
}

func (dev *AndroidDevice) StopPerf() string {
	// TODO
	return ""
}

func (dev *AndroidDevice) StartPcap() error {
	// TODO
	return nil
}

func (dev *AndroidDevice) StopPcap() string {
	// TODO
	return ""
}

func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, errors.Wrap(err, "resolve tcp addr failed")
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, errors.Wrap(err, "listen tcp addr failed")
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

type AdbLogcat struct {
	serial    string
	logBuffer *bytes.Buffer
	errs      []error
	stopping  chan struct{}
	done      chan struct{}
	cmd       *exec.Cmd
}

func NewAdbLogcat(serial string) *AdbLogcat {
	return &AdbLogcat{
		serial:    serial,
		logBuffer: new(bytes.Buffer),
		stopping:  make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// CatchLogcatContext starts logcat with timeout context
func (l *AdbLogcat) CatchLogcatContext(timeoutCtx context.Context) (err error) {
	if err = l.CatchLogcat(); err != nil {
		return
	}
	go func() {
		select {
		case <-timeoutCtx.Done():
			_ = l.Stop()
		case <-l.stopping:
		}
	}()
	return
}

func (l *AdbLogcat) Stop() error {
	select {
	case <-l.stopping:
	default:
		close(l.stopping)
		<-l.done
		close(l.done)
	}
	return l.Errors()
}

func (l *AdbLogcat) Errors() (err error) {
	for _, e := range l.errs {
		if err != nil {
			err = fmt.Errorf("%v |[DeviceLogcatErr] %v", err, e)
		} else {
			err = fmt.Errorf("[DeviceLogcatErr] %v", e)
		}
	}
	return
}

func (l *AdbLogcat) CatchLogcat() (err error) {
	if l.cmd != nil {
		log.Warn().Msg("logcat already start")
		return nil
	}

	// clear logcat
	if err = myexec.RunCommand("adb", "-s", l.serial, "shell", "logcat", "-c"); err != nil {
		return
	}

	// start logcat
	l.cmd = myexec.Command("adb", "-s", l.serial,
		"logcat", "--format", "time", "-s", "iesqaMonitor:V")
	l.cmd.Stderr = l.logBuffer
	l.cmd.Stdout = l.logBuffer
	if err = l.cmd.Start(); err != nil {
		return
	}
	go func() {
		<-l.stopping
		if e := myexec.KillProcessesByGpid(l.cmd); e != nil {
			log.Error().Err(e).Msg("kill logcat process failed")
		}
		l.done <- struct{}{}
	}()
	return
}

func (l *AdbLogcat) BufferedLogcat() (err error) {
	// -d: dump the current buffered logcat result and exits
	cmd := myexec.Command("adb", "-s", l.serial, "logcat", "-d")
	cmd.Stdout = l.logBuffer
	cmd.Stderr = l.logBuffer
	if err = cmd.Run(); err != nil {
		return
	}
	return
}

type ExportPoint struct {
	Start     int         `json:"start" yaml:"start"`
	End       int         `json:"end" yaml:"end"`
	From      interface{} `json:"from" yaml:"from"`
	To        interface{} `json:"to" yaml:"to"`
	Operation string      `json:"operation" yaml:"operation"`
	Ext       string      `json:"ext" yaml:"ext"`
	RunTime   int         `json:"run_time,omitempty" yaml:"run_time,omitempty"`
}

func ConvertPoints(data string) (eps []ExportPoint) {
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		if strings.Contains(line, "ext") {
			idx := strings.Index(line, "{")
			if idx == -1 {
				continue
			}
			line = line[idx:]
			p := ExportPoint{}
			err := json.Unmarshal([]byte(line), &p)
			if err != nil {
				log.Error().Msg("failed to parse point data")
				continue
			}
			eps = append(eps, p)
		}
	}
	return
}

type UiSelectorHelper struct {
	value *bytes.Buffer
}

func NewUiSelectorHelper() UiSelectorHelper {
	return UiSelectorHelper{value: bytes.NewBufferString("new UiSelector()")}
}

func (s UiSelectorHelper) String() string {
	return s.value.String() + ";"
}

// Text Set the search criteria to match the visible text displayed
// in a widget (for example, the text label to launch an app).
//
// The text for the element must match exactly with the string in your input
// argument. Matching is case-sensitive.
func (s UiSelectorHelper) Text(text string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.text("%s")`, text))
	return s
}

// TextMatches Set the search criteria to match the visible text displayed in a layout
// element, using a regular expression.
//
// The text in the widget must match exactly with the string in your
// input argument.
func (s UiSelectorHelper) TextMatches(regex string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.textMatches("%s")`, regex))
	return s
}

// TextStartsWith Set the search criteria to match visible text in a widget that is
// prefixed by the text parameter.
//
// The matching is case-insensitive.
func (s UiSelectorHelper) TextStartsWith(text string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.textStartsWith("%s")`, text))
	return s
}

// TextContains Set the search criteria to match the visible text in a widget
// where the visible text must contain the string in your input argument.
//
// The matching is case-sensitive.
func (s UiSelectorHelper) TextContains(text string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.textContains("%s")`, text))
	return s
}

// ClassName Set the search criteria to match the class property
// for a widget (for example, "android.widget.Button").
func (s UiSelectorHelper) ClassName(className string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.className("%s")`, className))
	return s
}

// ClassNameMatches Set the search criteria to match the class property
// for a widget, using a regular expression.
func (s UiSelectorHelper) ClassNameMatches(regex string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.classNameMatches("%s")`, regex))
	return s
}

// Description Set the search criteria to match the content-description
// property for a widget.
//
// The content-description is typically used
// by the Android Accessibility framework to
// provide an audio prompt for the widget when
// the widget is selected. The content-description
// for the widget must match exactly
// with the string in your input argument.
//
// Matching is case-sensitive.
func (s UiSelectorHelper) Description(desc string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.description("%s")`, desc))
	return s
}

// DescriptionMatches Set the search criteria to match the content-description
// property for a widget.
//
// The content-description is typically used
// by the Android Accessibility framework to
// provide an audio prompt for the widget when
// the widget is selected. The content-description
// for the widget must match exactly
// with the string in your input argument.
func (s UiSelectorHelper) DescriptionMatches(regex string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.descriptionMatches("%s")`, regex))
	return s
}

// DescriptionStartsWith Set the search criteria to match the content-description
// property for a widget.
//
// The content-description is typically used
// by the Android Accessibility framework to
// provide an audio prompt for the widget when
// the widget is selected. The content-description
// for the widget must start
// with the string in your input argument.
//
// Matching is case-insensitive.
func (s UiSelectorHelper) DescriptionStartsWith(desc string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.descriptionStartsWith("%s")`, desc))
	return s
}

// DescriptionContains Set the search criteria to match the content-description
// property for a widget.
//
// The content-description is typically used
// by the Android Accessibility framework to
// provide an audio prompt for the widget when
// the widget is selected. The content-description
// for the widget must contain
// the string in your input argument.
//
// Matching is case-insensitive.
func (s UiSelectorHelper) DescriptionContains(desc string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.descriptionContains("%s")`, desc))
	return s
}

// ResourceId Set the search criteria to match the given resource ID.
func (s UiSelectorHelper) ResourceId(id string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.resourceId("%s")`, id))
	return s
}

// ResourceIdMatches Set the search criteria to match the resource ID
// of the widget, using a regular expression.
func (s UiSelectorHelper) ResourceIdMatches(regex string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.resourceIdMatches("%s")`, regex))
	return s
}

// Index Set the search criteria to match the widget by its node
// index in the layout hierarchy.
//
// The index value must be 0 or greater.
//
// Using the index can be unreliable and should only
// be used as a last resort for matching. Instead,
// consider using the `Instance(int)` method.
func (s UiSelectorHelper) Index(index int) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.index(%d)`, index))
	return s
}

// Instance Set the search criteria to match the
// widget by its instance number.
//
// The instance value must be 0 or greater, where
// the first instance is 0.
//
// For example, to simulate a user click on
// the third image that is enabled in a UI screen, you
// could specify a a search criteria where the instance is
// 2, the `className(String)` matches the image
// widget class, and `enabled(boolean)` is true.
// The code would look like this:
//  `new UiSelector().className("android.widget.ImageView")
//    .enabled(true).instance(2);`
func (s UiSelectorHelper) Instance(instance int) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.instance(%d)`, instance))
	return s
}

// Enabled Set the search criteria to match widgets that are enabled.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Enabled(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.enabled(%t)`, b))
	return s
}

// Focused Set the search criteria to match widgets that have focus.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Focused(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.focused(%t)`, b))
	return s
}

// Focusable Set the search criteria to match widgets that are focusable.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Focusable(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.focusable(%t)`, b))
	return s
}

// Scrollable Set the search criteria to match widgets that are scrollable.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Scrollable(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.scrollable(%t)`, b))
	return s
}

// Selected Set the search criteria to match widgets that
// are currently selected.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Selected(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.selected(%t)`, b))
	return s
}

// Checked Set the search criteria to match widgets that
// are currently checked (usually for checkboxes).
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Checked(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.checked(%t)`, b))
	return s
}

// Checkable Set the search criteria to match widgets that are checkable.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Checkable(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.checkable(%t)`, b))
	return s
}

// Clickable Set the search criteria to match widgets that are clickable.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) Clickable(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.clickable(%t)`, b))
	return s
}

// LongClickable Set the search criteria to match widgets that are long-clickable.
//
// Typically, using this search criteria alone is not useful.
// You should also include additional criteria, such as text,
// content-description, or the class name for a widget.
//
// If no other search criteria is specified, and there is more
// than one matching widget, the first widget in the tree
// is selected.
func (s UiSelectorHelper) LongClickable(b bool) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.longClickable(%t)`, b))
	return s
}

// packageName Set the search criteria to match the package name
// of the application that contains the widget.
func (s UiSelectorHelper) packageName(name string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.packageName(%s)`, name))
	return s
}

// PackageNameMatches Set the search criteria to match the package name
// of the application that contains the widget.
func (s UiSelectorHelper) PackageNameMatches(regex string) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.packageNameMatches(%s)`, regex))
	return s
}

// ChildSelector Adds a child UiSelector criteria to this selector.
//
// Use this selector to narrow the search scope to
// child widgets under a specific parent widget.
func (s UiSelectorHelper) ChildSelector(selector UiSelectorHelper) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.childSelector(%s)`, selector.value.String()))
	return s
}

func (s UiSelectorHelper) PatternSelector(selector UiSelectorHelper) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.patternSelector(%s)`, selector.value.String()))
	return s
}

func (s UiSelectorHelper) ContainerSelector(selector UiSelectorHelper) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.containerSelector(%s)`, selector.value.String()))
	return s
}

// FromParent Adds a child UiSelector criteria to this selector which is used to
// start search from the parent widget.
//
// Use this selector to narrow the search scope to
// sibling widgets as well all child widgets under a parent.
func (s UiSelectorHelper) FromParent(selector UiSelectorHelper) UiSelectorHelper {
	s.value.WriteString(fmt.Sprintf(`.fromParent(%s)`, selector.value.String()))
	return s
}
