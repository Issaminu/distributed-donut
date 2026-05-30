//go:build debug

// Package debug contains an optional, build-tagged console renderer used to
// eyeball decoded frames in the terminal. It is compiled only with
// `-tags debug` and is not wired into the default build. Feed frames into
// LogChan to see them drawn.
package debug

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// LogChan receives encoded frame bytes to draw. Nothing writes to it by
// default; wire it up where frames are produced when debugging.
var LogChan = make(chan []byte)

func ConsoleDrawer(ctx context.Context) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	var frames []byte
	clearScreen := getClearScreenCommand()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Animation renderer shutting down...")
			return
		case newFrames := <-LogChan:
			frames = append(frames, newFrames...)
		case <-ticker.C:
			if len(frames) > 0 {
				clearScreen()
				frame := getNextFrame(frames)
				fmt.Print(frame)
				frames = frames[len(frame):]
			}
		}
	}
}

func getNextFrame(frames []byte) string {
	const frameSize = 880
	end := frameSize
	if end > len(frames) {
		end = len(frames)
	}
	encodedFrame := frames[:end]
	return decodeFrame(encodedFrame)
}

func decodeFrame(encodedFrame []byte) string {
	const possibleCharacters = ".,-~:;=!*#$@ \n"
	var decodedFrame strings.Builder
	decodedFrame.Grow(len(encodedFrame) * 2)

	for _, currentByte := range encodedFrame {
		highNibble := (currentByte >> 4)
		lowNibble := (currentByte & 0x0f)
		decodedFrame.WriteByte(possibleCharacters[highNibble])
		decodedFrame.WriteByte(possibleCharacters[lowNibble])
	}

	return decodedFrame.String()
}

func getClearScreenCommand() func() {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "cls")
		cmd.Stdout = os.Stdout
		return func() {
			cmd.Run()
		}
	}
	return func() {
		fmt.Print("\033[2J\033[H")
	}
}
