//go:build localtest

package uixt

import (
	"fmt"
	"os"
	"testing"
)

func checkCP(buff []byte) error {
	service, err := newVEDEMCPService()
	if err != nil {
		return err
	}
	sdResults, err := service.getCPResult(buff)
	if err != nil {
		return err
	}
	fmt.Println(sdResults)
	return nil
}

func TestCPWithScreenshot(t *testing.T) {
	device, _ := NewIOSDevice(WithWDAPort(8700), WithWDAMjpegPort(8800))
	driver, err := device.NewUSBDriver(nil)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := driver.Screenshot()
	if err != nil {
		t.Fatal(err)
	}

	if err := checkCP(raw.Bytes()); err != nil {
		t.Fatal(err)
	}
}

func TestCPWithLocalFile(t *testing.T) {
	imagePath := "~/Downloads/1669385239_validate_1669385367.png"
	file, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}

	if err := checkCP(file); err != nil {
		t.Fatal(err)
	}
}
