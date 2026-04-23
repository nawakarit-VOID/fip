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

const BlockSize = 1 << 20
const SolidThreshold = 512 * 1024 // 512KB

// ===== Structures =====
type FileMeta struct {
	Name       string
	Mode       uint8 // 0=block,1=solid
	StartBlock uint32
	EndBlock   uint32
}

// ===== PACK (HYBRID) =====
func pack(output string, files []string) error {
	out, _ := os.Create(output)
	defer out.Close()

	writer := bufio.NewWriter(out)
	defer writer.Flush()

	encFast, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	encStrong, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))

	var metas []FileMeta
	var solidBuffer []byte
	blockIndex := 0

	flushSolid := func() {
		for len(solidBuffer) >= BlockSize {
			chunk := solidBuffer[:BlockSize]
			compressed := encStrong.EncodeAll(chunk, nil)

			binary.Write(writer, binary.LittleEndian, uint32(len(compressed)))
			writer.Write(compressed)

			solidBuffer = solidBuffer[BlockSize:]
			blockIndex++
		}
	}

	for _, f := range files {
		data, _ := os.ReadFile(f)
		size := len(data)

		// small file → solid
		if size < SolidThreshold {
			start := blockIndex
			solidBuffer = append(solidBuffer, data...)
			flushSolid()

			end := blockIndex
			metas = append(metas, FileMeta{
				Name:       filepath.Base(f),
				Mode:       1,
				StartBlock: uint32(start),
				EndBlock:   uint32(end),
			})
			continue
		}

		// large file → block mode
		start := blockIndex

		for offset := 0; offset < size; offset += BlockSize {
			end := offset + BlockSize
			if end > size {
				end = size
			}

			chunk := data[offset:end]

			// quick test compression ratio
			test := encFast.EncodeAll(chunk[:min(len(chunk), 1024)], nil)

			var compressed []byte
			if len(test) < len(chunk)/2 {
				compressed = encStrong.EncodeAll(chunk, nil)
			} else {
				compressed = encFast.EncodeAll(chunk, nil)
			}

			binary.Write(writer, binary.LittleEndian, uint32(len(compressed)))
			writer.Write(compressed)

			blockIndex++
		}

		end := blockIndex - 1

		metas = append(metas, FileMeta{
			Name:       filepath.Base(f),
			Mode:       0,
			StartBlock: uint32(start),
			EndBlock:   uint32(end),
		})
	}

	// flush remaining solid
	if len(solidBuffer) > 0 {
		compressed := encStrong.EncodeAll(solidBuffer, nil)
		binary.Write(writer, binary.LittleEndian, uint32(len(compressed)))
		writer.Write(compressed)
	}

	// ===== metadata =====
	metaStart, _ := out.Seek(0, io.SeekCurrent)

	binary.Write(writer, binary.LittleEndian, uint32(len(metas)))
	for _, m := range metas {
		nameBytes := []byte(m.Name)
		binary.Write(writer, binary.LittleEndian, uint16(len(nameBytes)))
		writer.Write(nameBytes)
		writer.WriteByte(m.Mode)
		binary.Write(writer, binary.LittleEndian, m.StartBlock)
		binary.Write(writer, binary.LittleEndian, m.EndBlock)
	}

	binary.Write(writer, binary.LittleEndian, metaStart)

	fmt.Println("Packed (HYBRID):", output)
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ===== UNPACK =====
func unpack(input, dest, targetFile string) error {
	f, _ := os.Open(input)
	defer f.Close()

	dec, _ := zstd.NewReader(nil)

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

		mode, _ := f.ReadByte()

		var start, end uint32
		binary.Read(f, binary.LittleEndian, &start)
		binary.Read(f, binary.LittleEndian, &end)

		metas = append(metas, FileMeta{
			Name:       string(nameBytes),
			Mode:       mode,
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

	fmt.Println("Extracted (HYBRID):", target.Name)
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
