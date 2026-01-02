package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
)

// An .ico is a small directory followed by one payload per size. Each payload
// is either a bottom-up 32-bit DIB or, since Windows Vista, a whole PNG file.
// We use PNG for 256x256, where the DIB form would waste 256 KiB, and DIBs
// below that, which every Windows version understands.
const pngThreshold = 256

type iconDirEntry struct {
	Width       byte // 0 means 256
	Height      byte
	ColorCount  byte
	Reserved    byte
	Planes      uint16
	BitCount    uint16
	BytesInRes  uint32
	ImageOffset uint32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32 // XOR image and AND mask stacked, so twice the real height
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// encodeICO writes a multi-size icon. Images must all be square; sizes are
// taken from each image's bounds.
func encodeICO(images []*image.NRGBA) ([]byte, error) {
	payloads := make([][]byte, len(images))
	for i, img := range images {
		var err error
		if img.Bounds().Dx() >= pngThreshold {
			payloads[i], err = encodePNGPayload(img)
		} else {
			payloads[i], err = encodeDIBPayload(img)
		}
		if err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	// ICONDIR: reserved, type 1 (icon), count.
	binary.Write(&buf, binary.LittleEndian, [3]uint16{0, 1, uint16(len(images))})

	const dirSize, entrySize = 6, 16
	offset := uint32(dirSize + entrySize*len(images))
	for i, img := range images {
		side := img.Bounds().Dx()
		// 256 is stored as 0; the field is a single byte.
		dim := byte(side)
		if side >= 256 {
			dim = 0
		}
		binary.Write(&buf, binary.LittleEndian, iconDirEntry{
			Width:       dim,
			Height:      dim,
			Planes:      1,
			BitCount:    32,
			BytesInRes:  uint32(len(payloads[i])),
			ImageOffset: offset,
		})
		offset += uint32(len(payloads[i]))
	}
	for _, p := range payloads {
		buf.Write(p)
	}
	return buf.Bytes(), nil
}

func encodePNGPayload(img *image.NRGBA) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeDIBPayload writes the BITMAPINFOHEADER + BGRA pixels + AND mask form.
func encodeDIBPayload(img *image.NRGBA) ([]byte, error) {
	side := img.Bounds().Dx()
	xorSize := side * side * 4
	// The AND mask is 1 bit per pixel with each row padded to 4 bytes. It is
	// redundant with the alpha channel, but Windows still expects it to be
	// there, so we write a zero-filled one.
	maskStride := ((side + 31) / 32) * 4
	andSize := maskStride * side

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, bitmapInfoHeader{
		Size:      40,
		Width:     int32(side),
		Height:    int32(side * 2),
		Planes:    1,
		BitCount:  32,
		SizeImage: uint32(xorSize + andSize),
	})

	// DIB rows run bottom to top, and the channel order is BGRA.
	row := make([]byte, side*4)
	for y := side - 1; y >= 0; y-- {
		for x := 0; x < side; x++ {
			c := img.NRGBAAt(img.Bounds().Min.X+x, img.Bounds().Min.Y+y)
			row[x*4+0] = c.B
			row[x*4+1] = c.G
			row[x*4+2] = c.R
			row[x*4+3] = c.A
		}
		buf.Write(row)
	}
	buf.Write(make([]byte, andSize))
	return buf.Bytes(), nil
}
