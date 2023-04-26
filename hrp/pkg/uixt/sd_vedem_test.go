//go:build localtest

package uixt

import (
	"fmt"
	"os"
	"testing"
)

func checkSD(buff []byte, detectType string) error {
	service, err := newVEDEMSDService()
	if err != nil {
		return err
	}
	sdResults, err := service.SceneDetection(buff, detectType)
	if err != nil {
		return err
	}
	fmt.Println(sdResults)
	return nil
}

func TestSDWithScreenshot(t *testing.T) {
	device, _ := NewIOSDevice(WithWDAPort(8700), WithWDAMjpegPort(8800))
	driver, err := device.NewUSBDriver(nil)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := driver.Screenshot()
	if err != nil {
		t.Fatal(err)
	}

	if err := checkSD(raw.Bytes(), "checkLiveTypeShop"); err != nil {
		t.Fatal(err)
	}
}

func TestSDWithLocalFile(t *testing.T) {
	imagePath := "~/Downloads/s1.png"
	file, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}

	if err := checkSD(file, "checkLiveTypeShop"); err != nil {
		t.Fatal(err)
	}
}
