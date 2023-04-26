package uixt

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"
)

func checkIM(search []byte, source []byte) error {
	service, err := newVEDEMIMService()
	if err != nil {
		return err
	}
	sdResults, err := service.getIMResult(search, source)
	if err != nil {
		return err
	}
	fmt.Println(sdResults)
	return nil
}

func TestIMWithScreenshot(t *testing.T) {
	device, _ := NewIOSDevice(WithWDAPort(8700), WithWDAMjpegPort(8800))
	driver, err := device.NewUSBDriver(nil)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := driver.Screenshot()
	if err != nil {
		t.Fatal(err)
	}

	imagePath := "~/Downloads/1669385239_validate_1669385367.png"
	search, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}

	if err := checkIM(search, raw.Bytes()); err != nil {
		t.Fatal(err)
	}
}

func TestIMWithLocalFile(t *testing.T) {
	imagePath := "/Users/bytedance/Downloads/20221202-223440.png"
	search, err := ioutil.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}

	sourcePath := "/Users/bytedance/Downloads/20221202-223432.jpeg"
	file, err := ioutil.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}

	if err := checkIM(search, file); err != nil {
		t.Fatal(err)
	}
}
