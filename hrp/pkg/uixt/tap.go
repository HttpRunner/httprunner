package uixt

import (
	"fmt"
)

func (dExt *DriverExt) TapAbsXY(x, y float64, options ...DataOption) error {
	// close popup if necessary
	if dExt.ClosePopup {
		dExt.ClosePopupHandler()
	}
	// tap on absolute coordinate [x, y]
	return dExt.Driver.TapFloat(x, y, options...)
}

func (dExt *DriverExt) TapXY(x, y float64, options ...DataOption) error {
	// tap on [x, y] percent of window size
	if x > 1 || y > 1 {
		return fmt.Errorf("x, y percentage should be < 1, got x=%v, y=%v", x, y)
	}

	x = x * float64(dExt.windowSize.Width)
	y = y * float64(dExt.windowSize.Height)

	return dExt.TapAbsXY(x, y, options...)
}

func (dExt *DriverExt) GetTextXY(ocrText string, options ...DataOption) (point PointF, err error) {
	// close popup if necessary
	if dExt.ClosePopup {
		dExt.ClosePopupHandler()
	}
	x, y, width, height, err := dExt.FindTextByOCR(ocrText, options...)
	if err != nil {
		return PointF{}, err
	}

	point = PointF{
		X: x + width*0.5,
		Y: y + height*0.5,
	}
	return point, nil
}

func (dExt *DriverExt) GetTextXYs(ocrText []string, options ...DataOption) (points []PointF, err error) {
	// close popup if necessary
	if dExt.ClosePopup {
		dExt.ClosePopupHandler()
	}
	ps, err := dExt.FindTextsByOCR(ocrText, options...)
	if err != nil {
		return nil, err
	}

	for _, point := range ps {
		pointF := PointF{
			X: point[0] + point[2]*0.5,
			Y: point[1] + point[3]*0.5,
		}
		points = append(points, pointF)
	}

	return points, nil
}

func (dExt *DriverExt) GetImageXY(imagePath string, options ...DataOption) (point PointF, err error) {
	// close popup if necessary
	if dExt.ClosePopup {
		dExt.ClosePopupHandler()
	}
	x, y, width, height, err := dExt.FindImageRectInUIKit(imagePath, options...)
	if err != nil {
		return PointF{}, err
	}

	point = PointF{
		X: x + width*0.5,
		Y: y + height*0.5,
	}
	return point, nil
}

func (dExt *DriverExt) TapByOCR(ocrText string, options ...DataOption) error {
	dataOptions := NewDataOptions(options...)

	point, err := dExt.GetTextXY(ocrText, options...)
	if err != nil {
		if dataOptions.IgnoreNotFoundError {
			return nil
		}
		return err
	}

	return dExt.TapAbsXY(point.X, point.Y, options...)
}

func (dExt *DriverExt) TapByCV(imagePath string, options ...DataOption) error {
	dataOptions := NewDataOptions(options...)

	point, err := dExt.GetImageXY(imagePath, options...)
	if err != nil {
		if dataOptions.IgnoreNotFoundError {
			return nil
		}
		return err
	}

	return dExt.TapAbsXY(point.X, point.Y, options...)
}

func (dExt *DriverExt) Tap(param string, options ...DataOption) error {
	return dExt.TapOffset(param, 0.5, 0.5, options...)
}

func (dExt *DriverExt) TapOffset(param string, xOffset, yOffset float64, options ...DataOption) (err error) {
	dataOptions := NewDataOptions(options...)

	x, y, width, height, err := dExt.FindUIRectInUIKit(param, options...)
	if err != nil {
		if dataOptions.IgnoreNotFoundError {
			return nil
		}
		return err
	}

	return dExt.TapAbsXY(x+width*xOffset, y+height*yOffset, options...)
}

func (dExt *DriverExt) DoubleTapXY(x, y float64) error {
	// double tap on coordinate: [x, y] should be relative
	if x > 1 || y > 1 {
		return fmt.Errorf("x, y percentage should be < 1, got x=%v, y=%v", x, y)
	}

	// close popup if necessary
	if dExt.ClosePopup {
		dExt.ClosePopupHandler()
	}

	x = x * float64(dExt.windowSize.Width)
	y = y * float64(dExt.windowSize.Height)
	return dExt.Driver.DoubleTapFloat(x, y)
}

func (dExt *DriverExt) DoubleTap(param string) (err error) {
	return dExt.DoubleTapOffset(param, 0.5, 0.5)
}

func (dExt *DriverExt) DoubleTapOffset(param string, xOffset, yOffset float64) (err error) {

	// close popup if necessary
	if dExt.ClosePopup {
		dExt.ClosePopupHandler()
	}

	var x, y, width, height float64
	if x, y, width, height, err = dExt.FindUIRectInUIKit(param); err != nil {
		return err
	}

	return dExt.Driver.DoubleTapFloat(x+width*xOffset, y+height*yOffset)
}
