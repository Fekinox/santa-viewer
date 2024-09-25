package main

import (
	"image"
	"io"
	"log"
	"math"
	"os"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
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
	err  error
}

type SantaViewer struct {
	image      image.Image
	imageMutex sync.RWMutex

	isImageLoaded bool

	loadedImageFiles chan ImageFileInfo

	newImageChan chan struct{}

	zoomToFit bool
	zoomLevel int
	offsetX   int
	offsetY   int
}

func (sv *SantaViewer) ImageWidget(x, y int) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		sv.imageMutex.RLock()
		defer sv.imageMutex.RUnlock()

		d := layout.Dimensions{Size: gtx.Constraints.Max}

		if !sv.isImageLoaded {
			return d
		}

		defer clip.Rect{Max: d.Size}.Push(gtx.Ops).Pop()

		var ox, oy, scale float32
		imsize := sv.image.Bounds().Max

		if sv.zoomToFit {
			// Scale is the largest s such that
			// s * imWidth < containerWidth and
			// s * imHeight < containerHeight.
			// Therefore s = min(imWidth/containerWidth,
			// imHeight/containerHeight)
			scale = min(
				float32(d.Size.X)/float32(imsize.X),
				float32(d.Size.Y)/float32(imsize.Y),
			)

			ox = (float32(d.Size.X) - scale*float32(imsize.X)) / 2
			oy = (float32(d.Size.Y) - scale*float32(imsize.Y)) / 2
		} else {
			if sv.zoomLevel%2 == 0 {
				scale = float32(math.Pow(2.0, float64(sv.zoomLevel/2)))
			} else {
				scale = float32(math.Pow(2.0, float64((sv.zoomLevel-1)/2)) *
					math.Sqrt2)
			}

			ox = (float32(d.Size.X)-scale*float32(imsize.X))/2 +
				float32(x)
			oy = (float32(d.Size.Y)-scale*float32(imsize.Y))/2 +
				float32(y)
		}

		imOp := paint.NewImageOp(sv.image)
		imOp.Filter = paint.FilterNearest
		imOp.Add(gtx.Ops)
		op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(scale, scale)).Offset(f32.Pt(ox, oy))).Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)

		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
}

func (sv *SantaViewer) ViewerWindow(window *app.Window) error {
	var ops op.Ops
	inset := layout.UniformInset(5)

	var leftClick bool
	var lastLeftClick time.Time
	var lastScroll time.Time

	var dragOX int
	var dragOY int

	var curX int
	var curY int

	go func() {
		for {
			<-sv.newImageChan
			sv.zoomToFit = true
			sv.zoomLevel = 0
			sv.offsetX = 0
			sv.offsetY = 0
			window.Invalidate()
		}
	}()

	for {
		switch e := window.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			// This graphics context is used for managing the rendering state.
			gtx := app.NewContext(&ops, e)
			event.Op(gtx.Ops, &leftClick)

			doubleClick := false

			for {
				ev, ok := gtx.Source.Event(pointer.Filter{
					Target: &leftClick,
					Kinds:  pointer.Press | pointer.Release | pointer.Drag |
					pointer.Scroll,
					ScrollY: pointer.ScrollRange{
						Min: -1,
						Max: 1,
					},
				})

				if !ok {
					break
				}

				if x, ok := ev.(pointer.Event); ok {
					switch x.Kind {
					case pointer.Press:
						leftClick = true
						curTime := time.Now()
						if curTime.Sub(lastLeftClick).Milliseconds() < 300 {
							doubleClick = true
						}
						lastLeftClick = curTime

						dragOX = x.Position.Round().X
						dragOY = x.Position.Round().Y

						curX = dragOX
						curY = dragOY
					case pointer.Release:
						leftClick = false

						sv.offsetX += curX - dragOX
						sv.offsetY += curY - dragOY

						dragOX = 0
						dragOY = 0
						curX = 0
						curY = 0
					case pointer.Drag:
						curX = x.Position.Round().X
						curY = x.Position.Round().Y
					case pointer.Scroll:
						// for some godforsaked reason scroll events are
						// doubled
						curTime := time.Now()
						if curTime.Sub(lastScroll).Milliseconds() < 50 {
							continue
						}
						lastScroll = curTime
						sv.zoomLevel -= x.Scroll.Round().Y
					}
				}
			}

			if doubleClick {
				sv.zoomToFit = !sv.zoomToFit
				sv.zoomLevel = 0
				sv.offsetX = 0
				sv.offsetY = 0
			}

			imwidget := sv.ImageWidget(
				sv.offsetX + curX - dragOX,
				sv.offsetY + curY - dragOY,
			)

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
				func() {
					sv.imageMutex.Lock()
					defer sv.imageMutex.Unlock()
					if x.err == nil {
						im, _, err := image.Decode(x.file)
						if err == nil {
							sv.image = im
							sv.isImageLoaded = true
							sv.newImageChan <- struct{}{}
						}
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
						err:  err,
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
		image:         nil,
		isImageLoaded: false,

		loadedImageFiles: make(chan ImageFileInfo),
		newImageChan:     make(chan struct{}),

		zoomToFit: true,
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
