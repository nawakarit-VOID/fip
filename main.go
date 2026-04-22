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
	sourceEntry.SetPlaceHolder("เลือกไฟล์/โฟลเดอร์")

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

	zipBtn := widget.NewButton("ZIP", func() {
		src := sourceEntry.Text
		dst := targetEntry.Text + "/output.zip"

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
		widget.NewLabel("Simple Zip/Unzip Tool"),
		sourceEntry,
		browseSrc,
		targetEntry,
		browseDst,
		zipBtn,
		unzipBtn,
		status,
	)

	w.SetContent(ui)
	w.Resize(fyne.NewSize(400, 300))
	w.ShowAndRun()
}
