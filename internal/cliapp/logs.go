package cliapp

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
)

// tailFollow prints the last `lines` lines of path and then polls for
// appended content, similar to `tail -n <lines> -f`. Implemented purely
// with Go's standard library (rather than shelling out to `tail`, which
// doesn't exist on Windows) so `wor host logs` works on every OS.
func tailFollow(w io.Writer, path, linesStr string) error {
	n, err := strconv.Atoi(linesStr)
	if err != nil || n <= 0 {
		n = 100
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot open log file: %s: %w", path, err)
	}
	defer f.Close()

	tail, err := lastNLines(f, n)
	if err != nil {
		return err
	}
	for _, line := range tail {
		fmt.Fprintln(w, line)
	}

	offset, _ := f.Seek(0, io.SeekEnd)
	for {
		info, err := f.Stat()
		if err == nil && info.Size() > offset {
			f.Seek(offset, io.SeekStart)
			buf := make([]byte, info.Size()-offset)
			nRead, _ := f.Read(buf)
			w.Write(buf[:nRead])
			offset += int64(nRead)
		} else if err == nil && info.Size() < offset {
			// File was truncated/rotated; restart from the beginning.
			offset = 0
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func lastNLines(f *os.File, n int) ([]string, error) {
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var buf []string
	for scanner.Scan() {
		buf = append(buf, scanner.Text())
		if len(buf) > n {
			buf = buf[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return buf, nil
}
