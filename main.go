package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ===== ZIP (Worker Pool + Pipeline) =====
type job struct {
	path string
	info os.FileInfo
}

type result struct {
	header *zip.FileHeader
	data   []byte
	err    error
}

func zipFolder(source, target string) error {
	zipfile, err := os.Create(target)
	if err != nil {
		return err
	}
	defer zipfile.Close()

	archive := zip.NewWriter(zipfile)
	defer archive.Close()

	jobs := make(chan job, 32)
	results := make(chan result, 32)

	// ===== workers (compress parallel) =====
	workerCount := 4
	var wg sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				relPath, _ := filepath.Rel(source, j.path)

				header, err := zip.FileInfoHeader(j.info)
				if err != nil {
					results <- result{err: err}
					continue
				}

				header.Name = relPath

				if j.info.IsDir() {
					header.Name += "/"
					results <- result{header: header}
					continue
				}

				header.Method = zip.Deflate

				file, err := os.Open(j.path)
				if err != nil {
					results <- result{err: err}
					continue
				}

				var buf bytes.Buffer
				_, err = io.Copy(&buf, file)
				file.Close()

				if err != nil {
					results <- result{err: err}
					continue
				}

				results <- result{header: header, data: buf.Bytes()}
			}
		}()
	}

	// walk files → send jobs
	go func() {
		filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			jobs <- job{path: path, info: info}
			return nil
		})
		close(jobs)
	}()

	// close results after workers done
	go func() {
		wg.Wait()
		close(results)
	}()

	// ===== writer (sequential) =====
	for res := range results {
		if res.err != nil {
			return res.err
		}

		writer, err := archive.CreateHeader(res.header)
		if err != nil {
			return err
		}

		if res.data != nil {
			_, err = writer.Write(res.data)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// ===== UNZIP (Parallel) =====
func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	base := filepath.Base(src)
	name := base[:len(base)-len(filepath.Ext(base))]
	dest = filepath.Join(dest, name)

	if err := os.MkdirAll(dest, os.ModePerm); err != nil {
		return err
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(r.File))
	sem := make(chan struct{}, 4)

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

	w.SetOnDropped(func(pos fyne.Position, uris []fyne.URI) {
		if len(uris) > 0 {
			sourceEntry.SetText(uris[0].Path())
		}
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
		widget.NewLabel("Zip Tool (Worker Pool + Pipeline)"),
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
