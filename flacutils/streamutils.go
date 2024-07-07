package flacutils

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"subflac/metautils"
)

type FileSection struct {
	*io.SectionReader
	start, end int64
}

func NewFileSection(file *os.File, start, end int64) (*FileSection, error) {
	sectionReader := io.NewSectionReader(file, start, end-start)
	return &FileSection{
		SectionReader: sectionReader,
		start:         start,
		end:           end,
	}, nil
}

func (fs *FileSection) Size() int64 {
	return fs.end - fs.start
}

type FakeFrameNumStream struct {
	s        io.Reader
	lastSeek int64
	startNum int64
}

func NewFFNS(stream io.Reader, startNum int64) *FakeFrameNumStream {
	return &FakeFrameNumStream{s: stream, lastSeek: 0, startNum: startNum}
}

func (m *FakeFrameNumStream) Read(p []byte) (n int, err error) {
	n, err = m.s.Read(p)
	if err != nil {
		return -1, err
	}

	//FIXME: very naive
	for i := 0; i < n-1; i++ {
		target := (uint16(p[i]) << 8) | uint16(p[i+1])
		if target == 0xFFF8 {
			utfLen, err := metautils.SampleNumFieldLen(p[i+32/8])
			if err != nil {
				continue
			}
			//fmt.Printf("Len: %d\n", utfLen)
			crc8OnMem := p[i+32/8+utfLen+0+0]
			FrameNumber := metautils.DecodeGeneralizedUTF8Number(p, i+32/8, utfLen)
			metautils.EncodeGeneralizedUTF8Number(FrameNumber-m.startNum, p, i+32/8, utfLen)
			newCrc8 := metautils.CalcCRC8(p, i, 32/8+utfLen+0+0)
			p[i+32/8+utfLen+0+0] = newCrc8
			fmt.Printf("Read at %d (%d bytes), frame number %d, crc %02X, utf len %d\n", m.lastSeek+int64(i), n, FrameNumber, crc8OnMem, utfLen)

			subFrameHeader := p[i+32/8+utfLen+0+0+1]
			fmt.Printf("Sub frame header 0b%08b\n", subFrameHeader)
		}
	}
	m.lastSeek += int64(n)
	return n, err
}

/*
func (m *FakeFrameNumStream) Seek(offset int64, whence int) (int64, error) {
	readOffset, err := m.s.Seek(offset, whence)
	if err != nil {
		return -1, err
	}
	m.lastSeek = readOffset
	return readOffset, err
}
*/

//func (s *io.SectionReader) ReadAt(p []byte, off int64) (n int, err error)
//reader := &MyReader{r: strings.NewReader("Hello, World!")}

// Fixed Block
func (s *Subflac) GenSubFlac(start float64, end float64) (io.Reader, error) {

	startAddr, startNum, endAddr, endNum, err := s.GetInterval(start, end)
	if err != nil {
		return nil, err
	}

	// Modify the FLAC metadata
	newSamples := (endNum - startNum) * int64(s.stream.Info.BlockSizeMax)
	modifiedMetadata, err := s.ModifyFLACMetadata(s.file, uint64(newSamples))
	if err != nil {
		return nil, err
	}
	fmt.Printf("New Frames %d, Samples %d\n", endNum-startNum, newSamples)

	// Create the file section
	section, err := NewFileSection(s.file, startAddr, endAddr)
	if err != nil {
		return nil, err
	}

	// Create a MultiReader
	multiReader := io.MultiReader(bytes.NewReader(modifiedMetadata), section)
	fakeReader := NewFFNS(multiReader, startNum)
	return fakeReader, nil
}
