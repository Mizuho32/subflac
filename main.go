package main

import (
	"fmt"
	"log"
	"os"
	"reflect"

	"subflac/flacutils"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/meta"
)

func main() {
	// コマンドライン引数からファイルパスを取得
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <flac_file_path>", os.Args[0])
	}
	filePath := os.Args[1]

	// FLACファイルを開く
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("failed to open FLAC file: %v", err)
	}
	defer file.Close()

	// FLACファイルを読み込む
	stream, err := flac.Parse(file)
	if err != nil {
		log.Fatalf("failed to parse FLAC file: %v", err)
	}

	// メタデータブロックを処理する
	fmt.Println("Size", len(stream.Blocks))
	fmt.Println("NSam", stream.Info.NSamples)
	for _, block := range stream.Blocks {
		fmt.Println("type", reflect.TypeOf(block.Body))
		switch blockBody := block.Body.(type) {
		case *meta.StreamInfo:
			fmt.Println("STREAMINFO block found")
			fmt.Printf("Min Block Size: %d\n", blockBody.BlockSizeMin)
			fmt.Printf("Max Block Size: %d\n", blockBody.BlockSizeMax)
			fmt.Printf("Min Frame Size: %d\n", blockBody.FrameSizeMin)
			fmt.Printf("Max Frame Size: %d\n", blockBody.FrameSizeMax)
			fmt.Printf("Sample Rate: %d\n", blockBody.SampleRate)
			fmt.Printf("Number of Channels: %d\n", blockBody.NChannels)
			fmt.Printf("Bits Per Sample: %d\n", blockBody.BitsPerSample)
			fmt.Printf("Total Samples: %d\n", blockBody.NSamples)
			fmt.Printf("MD5 Signature: %x\n", blockBody.MD5sum)
			// 書き込み（例: サンプルレートを変更）
			//blockBody.SampleRate = 44100
			fmt.Printf("Updated Sample Rate: %d\n", blockBody.SampleRate)

		case *meta.SeekTable:
			fmt.Println("SEEKTABLE block found")
			for i, seekPoint := range blockBody.Points {
				fmt.Printf("Seek Point %d: Sample Number: %d, Offset: %d, Number of Samples: %d\n",
					i, seekPoint.SampleNum, seekPoint.Offset, seekPoint.NSamples)
				// 書き込み（例: SeekPointのサンプル番号を変更）
				//seekPoint.SampleNumber = int64(i * 1000)
				fmt.Printf("Updated Seek Point %d: Sample Number: %d\n", i, seekPoint.SampleNum)
			}
		default:
			fmt.Println("Other block type found, ignoring")
		}
	}

	// subflac init
	subflac, err := flacutils.New(file, stream)
	if err != nil {
		fmt.Printf("Error Subflac init: %v\n", err)
		return
	}

	// 読み込み開始位置を計算
	offsets := []int64{subflac.FileSize() / 2, 86, int64(86 + stream.Info.FrameSizeMin*1), 87}

	for _, offset := range offsets {
		fmt.Printf("Find from %d:\n", offset)

		frameStart, frameStartRel, utfLen, crc8, err := subflac.FrameStartByAddress(offset)
		if err != nil {
			fmt.Printf("Error reading frame start: %v\n", err)
			return
		}
		fmt.Printf("  Sync code found at byte %d (rel: %d)\n", frameStart, frameStartRel)

		// ヘッダーの解析
		sampleNumber := subflac.SampleNumber(utfLen)
		fmt.Printf("  Sample Number: %d, UTF len: %d, CRC8: %X\n", sampleNumber, utfLen, crc8)
	}
}
