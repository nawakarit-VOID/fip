package main

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func zipFolder(source, target string) error {
	zipfile, err := os.Create(target)
	if err != nil {
		return err
	}
	defer zipfile.Close()

	archive := zip.NewWriter(zipfile)
	defer archive.Close()

	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}

		header.Name = relPath

		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}

		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(writer, file)
		return err
	})
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

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.Create(fpath)
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		_, err = io.Copy(outFile, rc)

		outFile.Close()
		rc.Close()

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
		src := sourceEntry.Text
		dstDir := targetEntry.Text

		base := filepath.Base(src)
		zipName := base + ".zip"
		dst := filepath.Join(dstDir, zipName)

		status.SetText("Zipping...")
		go func() {
			err := zipFolder(src, dst)
			if err != nil {
				status.SetText("Error: " + err.Error())
			} else {
				status.SetText("Done: " + dst)
			}
		}()
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
