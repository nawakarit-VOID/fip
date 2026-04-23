package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
)

// ===== File Entry =====
type FileEntry struct {
	Name string
	Size uint64
}

// ===== Pack =====
func pack(output string, files []string) error {
	var table []FileEntry
	var combined bytes.Buffer

	// collect data
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}

		table = append(table, FileEntry{
			Name: filepath.Base(f),
			Size: uint64(len(data)),
		})

		combined.Write(data)
	}

	// compress
	enc, _ := zstd.NewWriter(nil)
	compressed := enc.EncodeAll(combined.Bytes(), nil)

	out, err := os.Create(output)
	if err != nil {
		return err
	}
	defer out.Close()

	// write header
	binary.Write(out, binary.LittleEndian, uint32(len(table)))

	// write file table
	for _, e := range table {
		nameBytes := []byte(e.Name)
		binary.Write(out, binary.LittleEndian, uint16(len(nameBytes)))
		out.Write(nameBytes)
		binary.Write(out, binary.LittleEndian, e.Size)
	}

	// write data
	out.Write(compressed)

	fmt.Println("Packed:", output)
	return nil
}

// ===== Unpack =====
func unpack(input, dest string) error {
	f, err := os.Open(input)
	if err != nil {
		return err
	}
	defer f.Close()

	var count uint32
	binary.Read(f, binary.LittleEndian, &count)

	var table []FileEntry

	for i := 0; i < int(count); i++ {
		var nameLen uint16
		binary.Read(f, binary.LittleEndian, &nameLen)

		nameBytes := make([]byte, nameLen)
		f.Read(nameBytes)

		var size uint64
		binary.Read(f, binary.LittleEndian, &size)

		table = append(table, FileEntry{
			Name: string(nameBytes),
			Size: size,
		})
	}

	compressed, _ := io.ReadAll(f)

	dec, _ := zstd.NewReader(nil)
	decompressed, err := dec.DecodeAll(compressed, nil)
	if err != nil {
		return err
	}

	// split files
	offset := 0
	for _, e := range table {
		data := decompressed[offset : offset+int(e.Size)]
		err := os.WriteFile(filepath.Join(dest, e.Name), data, 0644)
		if err != nil {
			return err
		}
		offset += int(e.Size)
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
		err := pack(output, files)
		if err != nil {
			fmt.Println("Error:", err)
		}

	case "unpack":
		input := os.Args[2]
		dest := os.Args[3]
		os.MkdirAll(dest, os.ModePerm)
		err := unpack(input, dest)
		if err != nil {
			fmt.Println("Error:", err)
		}
	}
}
