//go:build opencv

package uixt

import (
	"bytes"
	"image"
	"image/color"
	"io/ioutil"
	"sort"
	"strconv"
	"testing"

	"gocv.io/x/gocv"
)

func TestFindImageLocation(t *testing.T) {
	pathSource := "/Users/hero/Documents/temp/2020-05/opencv/Snipaste_2020-05-18_16-20-31.png"
	pathSearch := "/Users/hero/Documents/temp/2020-05/opencv/Snipaste.png"

	// pathSource = "/Users/hero/Documents/temp/2020-05/opencv/IMG_5291.jpg"
	// pathSearch = "/Users/hero/Documents/temp/2020-05/opencv/IMG_5.png"

	fileSource, err := ioutil.ReadFile(pathSource)
	checkErr(t, err)
	fileSearch, err := ioutil.ReadFile(pathSearch)
	checkErr(t, err)
	bufferSource := bytes.NewBuffer(fileSource)
	bufferSearch := bytes.NewBuffer(fileSearch)
	_, _ = bufferSource, bufferSearch

	var imgLoc image.Point
	// imgLoc, err := FindImageLocationFromRaw(bufferSource, bufferSearch, 0.95)
	imgLoc, err = FindImageLocationFromDisk(pathSource, pathSearch, 0.95, TmSqdiff)
	checkErr(t, err)
	t.Log(imgLoc)

	imgLoc, err = FindImageLocationFromDisk(pathSource, pathSearch, 0.95, TmSqdiffNormed)
	checkErr(t, err)
	t.Log(imgLoc)

	// imgLoc, err = FindImageLocationFromDisk(pathSource, pathSearch, 0.95, gocv.TmCcoeff)
	// checkErr(t, err)
	// t.Log(imgLoc)

	imgLoc, err = FindImageLocationFromDisk(pathSource, pathSearch, 0.95, TmCcoeffNormed)
	checkErr(t, err)
	t.Log(imgLoc)

	// imgLoc, err = FindImageLocationFromDisk(pathSource, pathSearch, 0.95, gocv.TmCcorr)
	// checkErr(t, err)
	// t.Log(imgLoc)

	imgLoc, err = FindImageLocationFromDisk(pathSource, pathSearch, 0.95, TmCcorrNormed)
	checkErr(t, err)
	t.Log(imgLoc)

	return

	window := gocv.NewWindow("Find Image")
	defer window.Close()
	blue := color.RGBA{R: 0, G: 0, B: 255, A: 0}
	matBig := gocv.IMRead(pathSource, gocv.IMReadColor)
	matTpl := gocv.IMRead(pathSearch, gocv.IMReadColor)
	rect := image.Rect(imgLoc.X, imgLoc.Y, imgLoc.X+matTpl.Cols(), imgLoc.Y+matTpl.Rows())
	gocv.Rectangle(&matBig, rect, blue, 3)
	for {
		window.IMShow(matBig)
		if window.WaitKey(1) >= 0 {
			break
		}
	}
}

func TestFindImageRectFromDisk(t *testing.T) {
	pathSource := "/Users/hero/Documents/temp/2020-05/opencv/Snipaste_2020-05-18_16-20-31.png"
	pathSource = "/Users/hero/Documents/temp/2020-05/opencv/loop1.png"
	// pathSource = "/Users/hero/Documents/temp/2020-05/opencv/loop2.png"

	pathSearch := "/Users/hero/Documents/temp/2020-05/opencv/Snipaste.png"

	imgRect, err := FindImageRectFromDisk(pathSource, pathSearch, 0.95, TmCcorrNormed)
	checkErr(t, err)
	t.Log(imgRect)

	// return

	window := gocv.NewWindow("Find Image")
	defer window.Close()
	blue := color.RGBA{R: 0, G: 0, B: 255, A: 0}
	matBig := gocv.IMRead(pathSource, gocv.IMReadColor)
	gocv.Rectangle(&matBig, imgRect, blue, 3)
	for {
		window.IMShow(matBig)
		if window.WaitKey(1) >= 0 {
			break
		}
	}
}

func TestFindAllImageLocationsFromDisk(t *testing.T) {
	pathSource := "/Users/hero/Documents/temp/2020-05/opencv/Snipaste_2020-05-18_16-20-31.png"
	pathSearch := "/Users/hero/Documents/temp/2020-05/opencv/Snipaste.png"

	pathSource = "/Users/hero/Documents/temp/2020-05/opencv/IMG_5291.jpg"
	pathSearch = "/Users/hero/Documents/temp/2020-05/opencv/IMG_5.png"

	locs, err := FindAllImageLocationsFromDisk(pathSource, pathSearch, 0.95)
	checkErr(t, err)
	t.Log(locs)

	sort.Slice(locs, func(i, j int) bool {
		if locs[i].Y < locs[j].Y {
			return true
		} else if locs[i].Y == locs[j].Y {
			if locs[i].X < locs[j].X {
				return true
			}
		}
		return false
	})

	// return

	window := gocv.NewWindow("Find Image")
	defer window.Close()
	blue := color.RGBA{R: 0, G: 0, B: 255, A: 0}
	matBig := gocv.IMRead(pathSource, gocv.IMReadColor)
	matTpl := gocv.IMRead(pathSearch, gocv.IMReadColor)
	for i := range locs {
		rect := image.Rect(locs[i].X, locs[i].Y, locs[i].X+matTpl.Cols(), locs[i].Y+matTpl.Rows())
		gocv.Rectangle(&matBig, rect, blue, 3)
		gocv.PutText(&matBig, strconv.FormatInt(int64(i), 10), locs[i], gocv.FontHersheySimplex, 2, blue, 3)
	}

	for {
		window.IMShow(matBig)
		if window.WaitKey(1) >= 0 {
			break
		}
	}
}

func TestFindAllImageRectsFromDisk(t *testing.T) {
	pathSource := "/Users/hero/Documents/temp/2020-05/opencv/Snipaste_2020-05-18_16-20-31.png"
	pathSearch := "/Users/hero/Documents/temp/2020-05/opencv/Snipaste.png"

	pathSource = "/Users/hero/Documents/temp/2020-05/opencv/IMG_5291.jpg"
	pathSearch = "/Users/hero/Documents/temp/2020-05/opencv/IMG_5.png"

	rects, err := FindAllImageRectsFromDisk(pathSource, pathSearch, 0.95)
	checkErr(t, err)
	t.Log(rects)

	// return

	window := gocv.NewWindow("Find Image")
	defer window.Close()
	blue := color.RGBA{R: 0, G: 0, B: 255, A: 0}
	matBig := gocv.IMRead(pathSource, gocv.IMReadColor)
	for i := range rects {
		gocv.Rectangle(&matBig, rects[i], blue, 3)
		gocv.PutText(&matBig, strconv.FormatInt(int64(i), 10), rects[i].Min, gocv.FontHersheySimplex, 2, blue, 3)
	}

	for {
		window.IMShow(matBig)
		if window.WaitKey(1) >= 0 {
			break
		}
	}
}

func TestFindImageLocationFromRaw(t *testing.T) {
	pathSource := "/Users/hero/Documents/temp/2020-05/opencv/Snipaste_2020-05-18_16-20-31.png"
	pathSearch := "/Users/hero/Documents/temp/2020-05/opencv/Snipaste.png"

	// pathSource = "/Users/hero/Documents/temp/2020-05/opencv/IMG_5291.jpg"
	// pathSearch = "/Users/hero/Documents/temp/2020-05/opencv/IMG_5.png"

	fileSource, err := ioutil.ReadFile(pathSource)
	checkErr(t, err)
	fileSearch, err := ioutil.ReadFile(pathSearch)
	checkErr(t, err)
	bufferSource := bytes.NewBuffer(fileSource)
	bufferSearch := bytes.NewBuffer(fileSearch)
	_, _ = bufferSource, bufferSearch

	var imgLoc image.Point
	// imgLoc, err := FindImageLocationFromRaw(bufferSource, bufferSearch, 0.95)
	imgLoc, err = FindImageLocationFromRaw(bufferSource, bufferSearch, 0.95, TmSqdiff)
	checkErr(t, err)
	t.Log(imgLoc)

	t.Log(FindImageRectFromRaw(bufferSource, bufferSearch, 0.95, TmSqdiff))

	window := gocv.NewWindow("Find Image")
	defer window.Close()
	blue := color.RGBA{R: 0, G: 0, B: 255, A: 0}
	matBig := gocv.IMRead(pathSource, gocv.IMReadColor)
	matTpl := gocv.IMRead(pathSearch, gocv.IMReadColor)
	rect := image.Rect(imgLoc.X, imgLoc.Y, imgLoc.X+matTpl.Cols(), imgLoc.Y+matTpl.Rows())
	gocv.Rectangle(&matBig, rect, blue, 3)
	for {
		window.IMShow(matBig)
		if window.WaitKey(1) >= 0 {
			break
		}
	}
}

func TestFindAllImageRectsFromRaw(t *testing.T) {
	pathSource := "/Users/hero/Documents/temp/2020-05/opencv/Snipaste_2020-05-18_16-20-31.png"
	pathSearch := "/Users/hero/Documents/temp/2020-05/opencv/Snipaste.png"

	pathSource = "/Users/hero/Documents/temp/2020-05/opencv/IMG_5291.jpg"
	pathSearch = "/Users/hero/Documents/temp/2020-05/opencv/IMG_5.png"

	fileSource, err := ioutil.ReadFile(pathSource)
	checkErr(t, err)
	fileSearch, err := ioutil.ReadFile(pathSearch)
	checkErr(t, err)
	bufferSource := bytes.NewBuffer(fileSource)
	bufferSearch := bytes.NewBuffer(fileSearch)
	_, _ = bufferSource, bufferSearch

	rects, err := FindAllImageRectsFromRaw(bufferSource, bufferSearch, 0.95)
	checkErr(t, err)
	t.Log(rects)

	sort.Slice(rects, func(i, j int) bool {
		if rects[i].Min.Y < rects[j].Min.Y {
			return true
		} else if rects[i].Min.Y == rects[j].Min.Y {
			if rects[i].Min.X < rects[j].Min.X {
				return true
			}
		}
		return false
	})

	// return

	window := gocv.NewWindow("Find Image")
	defer window.Close()
	blue := color.RGBA{R: 0, G: 0, B: 255, A: 0}
	matBig := gocv.IMRead(pathSource, gocv.IMReadColor)
	for i := range rects {
		gocv.Rectangle(&matBig, rects[i], blue, 3)
		gocv.PutText(&matBig, strconv.FormatInt(int64(i), 10), rects[i].Min, gocv.FontHersheySimplex, 2, blue, 3)
	}

	for {
		window.IMShow(matBig)
		if window.WaitKey(1) >= 0 {
			break
		}
	}
}
