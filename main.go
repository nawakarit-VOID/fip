package main

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	kzip "github.com/klauspost/compress/zip"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ===== STREAMING ZIP (klauspost + faster) =====
func zipFolder(ctx context.Context, source, target string, progress func(float64)) error {
	zipfile, err := os.Create(target)
	if err != nil {
		return err
	}
	defer zipfile.Close()

	archive := kzip.NewWriter(zipfile)
	defer archive.Close()

	// total size
	var total int64
	filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})

	var done int64

	err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		header, err := kzip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(source, path)
		header.Name = relPath

		if info.IsDir() {
			header.Name += "/"
			_, err := archive.CreateHeader(header)
			return err
		}

		// faster deflate (klauspost optimized)
		header.Method = kzip.Deflate

		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		buf := make([]byte, 64*1024) // bigger buffer
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			n, err := file.Read(buf)
			if n > 0 {
				_, werr := writer.Write(buf[:n])
				if werr != nil {
					return werr
				}
				atomic.AddInt64(&done, int64(n))
				if total > 0 && progress != nil {
					progress(float64(done) / float64(total))
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
		}

		return nil
	})

	return err
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	// สร้างโฟลเดอร์จากชื่อ zip
	base := filepath.Base(src)
	name := base[:len(base)-len(filepath.Ext(base))]
	dest = filepath.Join(dest, name)

	if err := os.MkdirAll(dest, os.ModePerm); err != nil {
		return err
	}

	// ===== Parallel unzip =====
	var wg sync.WaitGroup
	errChan := make(chan error, len(r.File))

	// จำกัดจำนวน worker กัน disk ตัน
	sem := make(chan struct{}, 4) // ปรับได้ตาม CPU/SSD

	for _, f := range r.File {
		f := f
		wg.Add(1)

		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			fpath := filepath.Join(dest, f.Name)

			if f.FileInfo().IsDir() {
				os.MkdirAll(fpath, os.ModePerm)
				return
			}

			if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
				errChan <- err
				return
			}

			outFile, err := os.Create(fpath)
			if err != nil {
				errChan <- err
				return
			}

			rc, err := f.Open()
			if err != nil {
				outFile.Close()
				errChan <- err
				return
			}

			_, err = io.Copy(outFile, rc)

			outFile.Close()
			rc.Close()

			if err != nil {
				errChan <- err
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// return first error (ถ้ามี)
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return nil
}

func main() {
	a := app.New()
	w := a.NewWindow("Zip Tool")

	sourceEntry := widget.NewEntry()
	sourceEntry.SetPlaceHolder("ลากไฟล์/โฟลเดอร์มาวาง หรือกดเลือก")

	targetEntry := widget.NewEntry()
	targetEntry.SetPlaceHolder("ปลายทาง")

	status := widget.NewLabel("Ready")
	progressBar := widget.NewProgressBar()

	var cancel context.CancelFunc

	browseSrc := widget.NewButton("เลือกต้นทาง", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if uri != nil {
				sourceEntry.SetText(uri.Path())
			}
		}, w)
	})

	browseDst := widget.NewButton("เลือกปลายทาง", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if uri != nil {
				targetEntry.SetText(uri.Path())
			}
		}, w)
	})

	// วิธีที่นายใช้: w.SetOnDropped() ✔ ใช้ได้จริงใน Fyne v2
	w.SetOnDropped(func(pos fyne.Position, uris []fyne.URI) {
		if len(uris) > 0 {
			sourceEntry.SetText(uris[0].Path())
		}
	})

	// (เสริม) drop zone แบบ widget เผื่ออยากมีพื้นที่ให้ลากชัดๆ
	drop := newDropZone(func(path string) {
		sourceEntry.SetText(path)
	})

	zipBtn := widget.NewButton("ZIP", func() {
		ctx, c := context.WithCancel(context.Background())
		cancel = c

		src := sourceEntry.Text
		dst := filepath.Join(targetEntry.Text, filepath.Base(src)+".zip")

		go func() {
			err := zipFolder(ctx, src, dst, func(p float64) {
				progressBar.SetValue(p)
			})
			if err != nil {
				status.SetText("Error: " + err.Error())
			} else {
				status.SetText("Done")
			}
		}()
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		if cancel != nil {
			cancel()
			status.SetText("Cancelled")
		}
	})

	unzipBtn := widget.NewButton("UNZIP", func() {
		src := sourceEntry.Text
		dst := targetEntry.Text

		status.SetText("Unzipping...")
		go func() {
			err := unzip(src, dst)
			if err != nil {
				status.SetText("Error: " + err.Error())
			} else {
				status.SetText("Done")
			}
		}()

	})

	ui := container.NewVBox(
		widget.NewLabel("Zip Tool (Drag & Drop รองรับ)"),
		drop,
		sourceEntry,
		browseSrc,
		targetEntry,
		browseDst,
		zipBtn,
		unzipBtn,
		cancelBtn,
		progressBar,

		status,
	)

	w.SetContent(ui)
	w.Resize(fyne.NewSize(400, 320))
	w.ShowAndRun()
}

// ===== Drag & Drop Widget (optional) =====
type dropZone struct {
	widget.BaseWidget
	onDrop func(string)
}

func newDropZone(onDrop func(string)) *dropZone {
	d := &dropZone{onDrop: onDrop}
	d.ExtendBaseWidget(d)
	return d
}

func (d *dropZone) CreateRenderer() fyne.WidgetRenderer {
	label := widget.NewLabel("📂 ลากไฟล์/โฟลเดอร์มาวางที่นี่")
	return widget.NewSimpleRenderer(label)
}

// Fyne v2 file drop
func (d *dropZone) DropFiles(pos fyne.Position, uris []fyne.URI) {
	if len(uris) > 0 && d.onDrop != nil {
		d.onDrop(uris[0].Path())
	}
}
