package flacutils

import (
	"fmt"
	"io"
	"math"
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

	frameStartLast    int64
	frameStartRelLast int
	utfLenLast        int
	crc8Last          byte
	offsetSBLast      int64
	bytesReadSBLast   int // For searchBuffer
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
		fileSize:          fileInfo.Size(),
		frameStartLast:    -1, frameStartRelLast: -1}, nil
}

func (s *Subflac) IsFixedBlk() bool {
	return s.stream.Info.BlockSizeMax == s.stream.Info.BlockSizeMin
}

func (s *Subflac) FileSize() int64 {
	return s.fileSize
}

func (s *Subflac) FrameStartByAddress(offset int64) (int64, int, int, byte, error) {

	// first time or can use existing search buffer
	// TODO: Reuse buffer
	//if s.frameStartLast < 0 ||
	//	!(s.offsetSBLast <= offset && offset < s.offsetSBLast+int64(s.bytesReadSBLast)) {

	// 読み込み開始位置にシーク
	_, err := s.file.Seek(offset, 0)
	if err != nil {
		return -1, -1, -1, 0, err
	}
	s.offsetSBLast = offset

	s.bytesReadSBLast, err = s.file.Read(s.searchBuffer)
	if err != nil {
		return -1, -1, -1, 0, err
	}

	//}
	buffer := s.searchBuffer
	bytesRead := s.bytesReadSBLast

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
					return -1, -1, -1, 0, err
				}
				_, err = s.file.Read(s.frameHeaderBuffer)
				if err != nil {
					return -1, -1, -1, 0, err
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

			s.crc8Last = crc8
			s.utfLenLast = utfLen
			return offset + int64(i), i, utfLen, crc8, nil
		}
	}

	return -1, -1, -1, 0, fmt.Errorf("frame start not found from address %d", offset)
}

// For fixed block size (0-origin)
// target Nth frame -> that address
func (s *Subflac) GetNthFrame(target int64) (int64, int64, error) {

	if !(uint64(target) < s.stream.Info.NSamples) {
		return -1, -1, fmt.Errorf("invalid target: %d", target)
	}

	targetAddress := s.FileSize() / int64(s.stream.Info.NSamples) * target
	//fmt.Printf("GetNthFrame from at %d\n", targetAddress)

	targetAddress, _, utfLen, _, err := s.FrameStartByAddress(targetAddress)
	if err != nil {
		return -1, -1, err
	}

	sampleNumber := s.SampleNumber(utfLen)
	diff := target - sampleNumber
	FrameSizeMinInt64 := int64(s.stream.Info.FrameSizeMin)

	//fmt.Printf("GetNthFrame 1st seek %d at %d (diff=%d)\n", sampleNumber, targetAddress, diff)

	for diff < 0 || 50 < diff {
		if diff < 0 { // over
			targetAddress = max(0, targetAddress+diff*FrameSizeMinInt64)
		} else {
			targetAddress = min(s.FileSize()-FrameSizeMinInt64, targetAddress+diff*FrameSizeMinInt64)
		}
		//fmt.Printf("GetNthFrame estim addr %d\n", targetAddress)

		targetAddress, _, utfLen, _, err = s.FrameStartByAddress(targetAddress)
		if err != nil {
			return -1, -1, err
		}

		sampleNumber = s.SampleNumber(utfLen)
		diff = target - sampleNumber
	}

	//fmt.Printf("  Stop at :%d of %dth sample (diff=%d, crc=%02X)\n", targetAddress, sampleNumber, diff, s.crc8Last)

	for ; diff > 0; diff-- {
		targetAddress, _, utfLen, _, err = s.FrameStartByAddress(targetAddress + FrameSizeMinInt64)
		if err != nil {
			return -1, -1, err
		}

		tmp := s.SampleNumber(utfLen)
		if tmp != sampleNumber+1 {
			return -1, -1, fmt.Errorf("while seeking to target, %d should be %d", tmp, sampleNumber+1)
		}
		sampleNumber = tmp
	}

	return targetAddress, sampleNumber, nil
}

// Fixed block size
func (s *Subflac) GetInterval(start float64, end float64) (int64, int64, int64, int64, error) {
	duration := float64(s.stream.Info.NSamples) / float64(s.stream.Info.SampleRate)
	start = min(max(0, start), duration)
	end = min(max(0, end), duration)

	NFramesFloat := float64(s.stream.Info.NSamples / uint64(s.stream.Info.BlockSizeMax))
	startSample := int64(math.Floor(NFramesFloat * (start / duration)))
	endSample := int64(math.Floor(NFramesFloat * (end / duration)))

	//fmt.Printf("NFrames: %f, start,end,dur: %f,%f,%f\n", NFramesFloat, start, end, duration)
	//fmt.Printf("startSample: %d\n", startSample)
	startAddr, _, err := s.GetNthFrame(startSample)
	if err != nil {
		return -1, -1, -1, -1, err
	}
	//fmt.Printf("endSample: %d\n", endSample)
	endAddr, _, err := s.GetNthFrame(endSample)
	if err != nil {
		return -1, -1, -1, -1, err
	}

	//fmt.Printf("durat: %f, start: %d, end %d\n", duration, startSample, endSample)
	return startAddr, startSample, endAddr, endSample, nil
}

func (s *Subflac) ModifyFLACMetadata(file *os.File, newNSamples uint64) ([]byte, error) {

	// 4 for fLaC signature + 34 for STREAMINFO + 4 for metadata block header
	metadataSize, _, _, _, err := s.FrameStartByAddress(42)
	if err != nil {
		return nil, err
	}
	header := make([]byte, metadataSize)

	_, err = s.file.Seek(0, io.SeekStart)
	if err != nil {
		return nil, err
	}
	_, err = file.Read(header)
	if err != nil {
		return nil, err
	}

	fieldLen := 36/8 + 1
	offsetBit := 32 + (1 + 7 + 24) + (16 + 16 + 24 + 24 + 20 + 3 + 5)
	offsetBitInByte := offsetBit % 8
	restBit := uint64((offsetBit + 36) % 8)
	startByte := offsetBit / 8

	// Modify NSamples (positions 27 to 34 in the STREAMINFO block)
	//newField := make([]byte, 64/8)
	//buf := bytes.NewBuffer(newField)
	//binary.Write(buf, binary.BigEndian, newNSamples<<restBit)
	newNSamplesBytes := newNSamples << restBit

	for idx := 0; idx < fieldLen; idx++ {
		byt := byte((newNSamplesBytes >> (uint64(fieldLen-1-idx) * 8)) & 0xFF)
		var maskedByte byte
		if idx == 0 {
			newVal := (0xFF >> offsetBitInByte) & byt
			preserveVal := (0xFF << (8 - offsetBitInByte)) & header[startByte+idx]
			maskedByte = preserveVal | newVal
		} else if idx == fieldLen-1 {
			newVal := (0xFF << restBit) & byt
			preserveVal := (0xFF >> (8 - restBit)) & header[startByte+idx]
			maskedByte = preserveVal | newVal
		} else {
			maskedByte = byt
		}
		//fmt.Printf("%08b ", byt)
		header[startByte+idx] = maskedByte
	}
	//fmt.Printf("\n%036b\n", newNSamples)

	var readValue uint64 = 0
	for idx, byt := range header[startByte:(startByte + fieldLen)] {
		var maskedByte byte
		if idx == 0 {
			maskedByte = (0xFF >> offsetBitInByte) & byt
		} else if idx == fieldLen-1 {
			maskedByte = (0xFF << restBit) & byt
		} else {
			maskedByte = byt
		}
		//fmt.Printf("%08b ", maskedByte)
		readValue = (readValue << 8) | uint64(maskedByte)
	}
	readValue = (readValue >> restBit)
	//fmt.Printf("\n%036b\n", readValue)

	if readValue != newNSamples {
		return nil, fmt.Errorf("read NSamples %d != newNSamples %d", readValue, newNSamples)
	}

	return header, nil
}

// frame number for fixed block size
func (s *Subflac) SampleNumber(utfLen int) int64 {
	return metautils.DecodeGeneralizedUTF8Number(s.frameHeaderBuffer, 0+32/8, utfLen)
}
