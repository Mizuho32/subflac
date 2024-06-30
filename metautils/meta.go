package metautils

import (
	"errors"
	"fmt"
)

func SampleNumFieldLen(firstByte byte) (int, error) {
	if firstByte&0x80 > 0 { // non 0 left bit
		for i := 0; i <= 56/8; i++ {
			if (firstByte<<i)&0x80 == 0 {
				return i, nil
			}
		}
		return -1, errors.New(fmt.Sprint("Value error", "firstByte", firstByte))
	} else {
		return 1, nil
	}
}

var CRC8 byte = 0b00000111

func CalcCRC8(buff []byte, offset int, len int) byte {
	crc := byte(0)

	for idx := 0; idx < len; idx++ {
		byt := buff[idx+offset]
		//fmt.Printf("%X ", byt)
		crc ^= byt
		for i := 0; i < 8; i++ {
			if crc&0x80 > 0 {
				crc = (crc << 1) ^ CRC8
			} else {
				crc = (crc << 1)
			}
		}
	}
	//fmt.Printf("\n")
	return crc
}

func FindFrameStart(buffer []byte, offset int64, bytesRead int, isFixedBlk bool) (int64, int, int) {
	syncCode := uint16(0xFFF9) // 16ビットのSync code: 0xFFF8 (0xFFF9 for variable)
	if isFixedBlk {
		syncCode--
	}

	var frameStart int64 = -1
	var frameStartRel int = -1
	var utfLen int = -1

	for i := 0; i < bytesRead-1; i++ {
		target := (uint16(buffer[i]) << 8) | uint16(buffer[i+1])

		if target == syncCode {
			utfLen, err := SampleNumFieldLen(buffer[i+32/8])
			if err != nil {
				continue
			}
			//fmt.Printf("Len: %d\n", utfLen)

			crc8 := CalcCRC8(buffer, i, 32/8+utfLen)
			crc8OnMem := buffer[i+32/8+utfLen]
			if crc8 != crc8OnMem {
				continue
			}

			frameStartRel = i
			frameStart = offset + int64(i)
			return frameStart, frameStartRel, utfLen
		}
	}
	return frameStart, frameStartRel, utfLen
}

// UTF-8スタイルのエンコーディングをデコードする関数
func DecodeGeneralizedUTF8Number(buff []byte, offsetRel int, len int) uint64 {
	if len == 1 {
		return uint64(buff[offsetRel])
	}

	number := uint64(0)
	var mask byte = 0xFF

	for idx := 0; idx < len; idx++ {
		byt := buff[offsetRel+idx]
		//fmt.Printf("%02X ", byt)
		if idx == 0 {
			number = uint64((mask >> (len + 1)) & byt)
		} else {
			number = (number << 6) | uint64(0b00111111&byt)
		}
	}
	//fmt.Println("")
	return number
}
