package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/klauspost/compress/zstd"
)

const BlockSize = 1 << 20 // 1MB

// ===== Block =====
type Block struct {
	Index int
	Data  []byte
}

// ===== PACK (streaming + parallel) =====
func pack(output string, files []string) error {
	out, err := os.Create(output)
	if err != nil {
		return err
	}
	defer out.Close()

	writer := bufio.NewWriter(out)
	defer writer.Flush()

	enc, _ := zstd.NewWriter(nil)

	jobs := make(chan Block, 16)
	results := make(chan Block, 16)

	// ===== workers =====
	var wg sync.WaitGroup
	workerCount := runtime.NumCPU()

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				compressed := enc.EncodeAll(job.Data, nil)
				results <- Block{Index: job.Index, Data: compressed}
			}
		}()
	}

	// close results
	go func() {
		wg.Wait()
		close(results)
	}()

	// ===== read + split =====
	go func() {
		idx := 0
		for _, f := range files {
			file, _ := os.Open(f)
			defer file.Close()

			buf := make([]byte, BlockSize)
			for {
				n, err := file.Read(buf)
				if n > 0 {
					chunk := make([]byte, n)
					copy(chunk, buf[:n])
					jobs <- Block{Index: idx, Data: chunk}
					idx++
				}
				if err == io.EOF {
					break
				}
			}
		}
		close(jobs)
	}()

	// ===== ordered write =====
	buffer := make(map[int][]byte)
	expected := 0

	for res := range results {
		buffer[res.Index] = res.Data

		for {
			data, ok := buffer[expected]
			if !ok {
				break
			}

			binary.Write(writer, binary.LittleEndian, uint32(len(data)))
			writer.Write(data)

			delete(buffer, expected)
			expected++
		}
	}

	fmt.Println("Packed:", output)
	return nil
}

// ===== UNPACK =====
func unpack(input, dest string) error {
	f, err := os.Open(input)
	if err != nil {
		return err
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	dec, _ := zstd.NewReader(nil)

	os.MkdirAll(dest, os.ModePerm)

	outFile, _ := os.Create(filepath.Join(dest, "output.bin"))
	defer outFile.Close()

	for {
		var size uint32
		err := binary.Read(reader, binary.LittleEndian, &size)
		if err == io.EOF {
			break
		}

		buf := make([]byte, size)
		io.ReadFull(reader, buf)

		decompressed, err := dec.DecodeAll(buf, nil)
		if err != nil {
			return err
		}

		outFile.Write(decompressed)
	}

	fmt.Println("Unpacked to:", dest)
	return nil
}

// ===== CLI =====
func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage:")
		fmt.Println("  pack output.myfmt file1 file2 ...")
		fmt.Println("  unpack file.myfmt output_dir")
		return
	}

	cmd := os.Args[1]

	switch cmd {
	case "pack":
		output := os.Args[2]
		files := os.Args[3:]
		pack(output, files)

	case "unpack":
		input := os.Args[2]
		dest := os.Args[3]
		unpack(input, dest)
	}
}
