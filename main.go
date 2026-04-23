package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
)

const BlockSize = 1 << 20 // 1MB

// ===== Structures =====
type FileMeta struct {
	Name       string
	StartBlock uint32
	EndBlock   uint32
}

// ===== PACK (SOLID COMPRESSION) =====
func pack(output string, files []string) error {
	out, err := os.Create(output)
	if err != nil {
		return err
	}
	defer out.Close()

	writer := bufio.NewWriter(out)
	defer writer.Flush()

	enc, _ := zstd.NewWriter(nil)

	var metas []FileMeta
	var solidBuffer []byte
	blockIndex := 0

	for _, f := range files {
		data, _ := os.ReadFile(f)

		start := blockIndex

		// append to solid buffer
		solidBuffer = append(solidBuffer, data...)

		// split into blocks AFTER combining
		for len(solidBuffer) >= BlockSize {
			chunk := solidBuffer[:BlockSize]
			compressed := enc.EncodeAll(chunk, nil)

			binary.Write(writer, binary.LittleEndian, uint32(len(compressed)))
			writer.Write(compressed)

			solidBuffer = solidBuffer[BlockSize:]
			blockIndex++
		}

		end := blockIndex

		metas = append(metas, FileMeta{
			Name:       filepath.Base(f),
			StartBlock: uint32(start),
			EndBlock:   uint32(end),
		})
	}

	// flush remaining buffer
	if len(solidBuffer) > 0 {
		compressed := enc.EncodeAll(solidBuffer, nil)
		binary.Write(writer, binary.LittleEndian, uint32(len(compressed)))
		writer.Write(compressed)
	}

	// ===== write metadata at end =====
	metaStart, _ := out.Seek(0, io.SeekCurrent)

	binary.Write(writer, binary.LittleEndian, uint32(len(metas)))
	for _, m := range metas {
		nameBytes := []byte(m.Name)
		binary.Write(writer, binary.LittleEndian, uint16(len(nameBytes)))
		writer.Write(nameBytes)
		binary.Write(writer, binary.LittleEndian, m.StartBlock)
		binary.Write(writer, binary.LittleEndian, m.EndBlock)
	}

	binary.Write(writer, binary.LittleEndian, metaStart)

	fmt.Println("Packed (SOLID):", output)
	return nil
}

// ===== UNPACK (SOLID) =====
func unpack(input, dest, targetFile string) error {
	f, err := os.Open(input)
	if err != nil {
		return err
	}
	defer f.Close()

	dec, _ := zstd.NewReader(nil)

	// read meta position
	stat, _ := f.Stat()
	f.Seek(stat.Size()-8, io.SeekStart)

	var metaStart int64
	binary.Read(f, binary.LittleEndian, &metaStart)

	f.Seek(metaStart, io.SeekStart)

	var count uint32
	binary.Read(f, binary.LittleEndian, &count)

	var metas []FileMeta

	for i := 0; i < int(count); i++ {
		var nameLen uint16
		binary.Read(f, binary.LittleEndian, &nameLen)

		nameBytes := make([]byte, nameLen)
		f.Read(nameBytes)

		var start, end uint32
		binary.Read(f, binary.LittleEndian, &start)
		binary.Read(f, binary.LittleEndian, &end)

		metas = append(metas, FileMeta{
			Name:       string(nameBytes),
			StartBlock: start,
			EndBlock:   end,
		})
	}

	var target *FileMeta
	for i := range metas {
		if metas[i].Name == targetFile {
			target = &metas[i]
			break
		}
	}

	if target == nil {
		return fmt.Errorf("file not found")
	}

	// go to beginning
	f.Seek(0, io.SeekStart)
	reader := bufio.NewReader(f)

	os.MkdirAll(dest, os.ModePerm)
	outFile, _ := os.Create(filepath.Join(dest, target.Name))
	defer outFile.Close()

	currentBlock := 0

	for {
		var size uint32
		err := binary.Read(reader, binary.LittleEndian, &size)
		if err != nil {
			break
		}

		buf := make([]byte, size)
		io.ReadFull(reader, buf)

		if currentBlock >= int(target.StartBlock) && currentBlock <= int(target.EndBlock) {
			decompressed, _ := dec.DecodeAll(buf, nil)
			outFile.Write(decompressed)
		}

		currentBlock++
	}

	fmt.Println("Extracted (SOLID):", target.Name)
	return nil
}

// ===== CLI =====
func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage:")
		fmt.Println("  pack output.myfmt file1 file2 ...")
		fmt.Println("  unpack file.myfmt output_dir filename")
		return
	}

	switch os.Args[1] {
	case "pack":
		pack(os.Args[2], os.Args[3:])

	case "unpack":
		unpack(os.Args[2], os.Args[3], os.Args[4])
	}
}
