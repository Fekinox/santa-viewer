package main

import (
	"fmt"
	"image"
	"io"
	"log"
	"os"
	"sync"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"gioui.org/x/explorer"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

type ImageFileInfo struct {
	file io.ReadCloser
	err error
}

type SantaViewer struct {
	image image.Image
	imageMutex sync.RWMutex

	isImageLoaded bool

	loadedImageFiles chan ImageFileInfo

	newImageChan chan struct{}
}

func (sv *SantaViewer) ImageWidget() layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		sv.imageMutex.RLock()
		defer sv.imageMutex.RUnlock()

		if !sv.isImageLoaded { return layout.Dimensions{Size: image.Point{}} }

		defer clip.Rect{Max: sv.image.Bounds().Max}.Push(gtx.Ops).Pop()

		imOp := paint.NewImageOp(sv.image)
		imOp.Filter = paint.FilterNearest
		imOp.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)

		return layout.Dimensions{Size: sv.image.Bounds().Max}
	}
}

func (sv *SantaViewer) ViewerWindow(window *app.Window) error {
	// theme := material.NewTheme()
	var ops op.Ops

	go func() {
		for {
			<-sv.newImageChan
			window.Invalidate()
		}
	}()

	for {
		switch e := window.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			inset := layout.UniformInset(5)

			// This graphics context is used for managing the rendering state.
			gtx := app.NewContext(&ops, e)

			imwidget := sv.ImageWidget()

			// Draw the label to the graphics context.
			inset.Layout(gtx, imwidget)

			// fmt.Println(gtx.Constraints)

			// Pass the drawing operations to the GPU.
			e.Frame(gtx.Ops)
		}
	}
}

func (sv *SantaViewer) ControlPanelWindow(window *app.Window) error {
	theme := material.NewTheme()

	exp := explorer.NewExplorer(window)
	var button widget.Clickable

	var ops op.Ops
	for {
	imageLoop:
		for {
			select {
			case x := <-sv.loadedImageFiles:
				func (){
					sv.imageMutex.Lock()
					defer sv.imageMutex.Unlock()
					im, _, err := image.Decode(x.file)
					if err != nil {
						fmt.Println(err)
						sv.image = nil
						sv.isImageLoaded = false
						sv.newImageChan <- struct{}{}
					} else {
						sv.image = im
						sv.isImageLoaded = true
						sv.newImageChan <- struct{}{}
					}
				}()
			default:
				break imageLoop
			}
		}


		e := window.Event()
		exp.ListenEvents(e)
		switch e := e.(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			// This graphics context is used for managing the rendering state.
			gtx := app.NewContext(&ops, e)

			if button.Clicked(gtx) {
				go func() {
					f, err := exp.ChooseFile(
						".jpg",
						".png",
						".gif")

					sv.loadedImageFiles <- ImageFileInfo{
						file: f,
						err: err,
					}
				}()
			}

			btn := material.Button(theme, &button, "Choose image")

			btn.Layout(gtx)

			// Pass the drawing operations to the GPU.
			e.Frame(gtx.Ops)
		}
	}
}

func MakeSantaViewer() *SantaViewer {
	sv := &SantaViewer{
		image: nil,
		isImageLoaded: false,

		loadedImageFiles: make(chan ImageFileInfo),
		newImageChan: make(chan struct{}),
	}

	return sv
}

func main() {
	sv := MakeSantaViewer()
	go func() {
		window := new(app.Window)
		err := sv.ViewerWindow(window)
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	go func() {
		window := new(app.Window)
		err := sv.ControlPanelWindow(window)
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}
