package flacutils

import (
	"errors"
	"os"

	"subflac/metautils"

	"github.com/mewkiz/flac"
)

type Subflac struct {
	file              *os.File
	stream            *flac.Stream
	frameHeaderBuffer []byte
	searchBuffer      []byte
	fileSize          int64
}

func New(file *os.File, stream *flac.Stream) (*Subflac, error) {
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	utfFieldLen := 56 / 8
	if stream.Info.BlockSizeMax == stream.Info.BlockSizeMin {
		utfFieldLen = 48 / 8
	}

	return &Subflac{file: file, stream: stream,
		frameHeaderBuffer: make([]byte, 32/8+utfFieldLen+2+2+1), // maximum length
		searchBuffer:      make([]byte, stream.Info.FrameSizeMax),
		fileSize:          fileInfo.Size()}, nil
}

func (s *Subflac) IsFixedBlk() bool {
	return s.stream.Info.BlockSizeMax == s.stream.Info.BlockSizeMin
}

func (s *Subflac) FileSize() int64 {
	return s.fileSize
}

func (s *Subflac) FrameStartByAddress(offset int64) (int64, int, int, byte, error) {
	var frameStart int64 = -1
	var frameStartRel int = -1
	var utfLen int = -1
	var crc8 byte = 0

	// 読み込み開始位置にシーク
	_, err := s.file.Seek(offset, 0)
	if err != nil {
		return frameStart, frameStartRel, utfLen, crc8, err
	}

	bytesRead, err := s.file.Read(s.searchBuffer)
	buffer := s.searchBuffer
	if err != nil {
		return frameStart, frameStartRel, utfLen, crc8, err
	}

	// 16ビットのSync code: 0xFFF8 (0xFFF9 for variable)
	syncCode := uint16(0xFFF9)
	if s.IsFixedBlk() {
		syncCode--
	}

	frameHeaderLen := len(s.frameHeaderBuffer)

	for i := 0; i < bytesRead-1; i++ {
		target := (uint16(buffer[i]) << 8) | uint16(buffer[i+1])

		if target == syncCode {

			// allocate frame header buffer
			remainLen := bytesRead - i + 1
			if remainLen < frameHeaderLen {
				_, err := s.file.Seek(offset+int64(i), 0)
				if err != nil {
					return frameStart, frameStartRel, utfLen, crc8, err
				}
				_, err = s.file.Read(s.frameHeaderBuffer)
				if err != nil {
					return frameStart, frameStartRel, utfLen, crc8, err
				}
			} else {
				copy(s.frameHeaderBuffer, buffer[i:(i+len(s.frameHeaderBuffer))])
			}

			utfLen, err := metautils.SampleNumFieldLen(s.frameHeaderBuffer[0+32/8])
			if err != nil {
				continue
			}
			//fmt.Printf("Len: %d\n", utfLen)

			crc8 := metautils.CalcCRC8(s.frameHeaderBuffer, 0, 32/8+utfLen+0+0)
			crc8OnMem := s.frameHeaderBuffer[0+32/8+utfLen+0+0]
			//fmt.Printf("CRC: %X, onmem: %X\n", crc8, crc8OnMem)
			if crc8 != crc8OnMem {
				continue
			}

			frameStartRel = i
			frameStart = offset + int64(i)
			return frameStart, frameStartRel, utfLen, crc8, nil
		}
	}

	return frameStart, frameStartRel, utfLen, crc8, errors.New("frame start not found")
}

// frame number for fixed block size
func (s *Subflac) SampleNumber(utfLen int) uint64 {
	return metautils.DecodeGeneralizedUTF8Number(s.frameHeaderBuffer, 0+32/8, utfLen)
}
