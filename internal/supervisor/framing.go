package supervisor

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

var (
	ErrFrameTooLarge  = errors.New("stdio frame exceeds configured limit")
	ErrMalformedFrame = errors.New("stdio frame is not a JSON object")
)

type FrameReader struct {
	reader  *bufio.Reader
	maximum int
}

func NewFrameReader(reader io.Reader, maximum int) *FrameReader {
	return &FrameReader{reader: bufio.NewReaderSize(reader, maximum+1), maximum: maximum}
}
func (reader *FrameReader) Read() ([]byte, error) {
	line, err := reader.reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(line) > reader.maximum+1 {
		return nil, ErrFrameTooLarge
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 && errors.Is(err, io.EOF) {
		return nil, io.EOF
	}
	if len(line) > reader.maximum || !json.Valid(line) || line[0] != '{' {
		return nil, ErrMalformedFrame
	}
	return append([]byte(nil), line...), nil
}

type FrameWriter struct {
	mu      sync.Mutex
	writer  io.Writer
	maximum int
}

func NewFrameWriter(writer io.Writer, maximum int) *FrameWriter {
	return &FrameWriter{writer: writer, maximum: maximum}
}
func (writer *FrameWriter) Write(frame []byte) error {
	if len(frame) > writer.maximum {
		return ErrFrameTooLarge
	}
	if !json.Valid(frame) || len(frame) == 0 || frame[0] != '{' {
		return ErrMalformedFrame
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	_, err := writer.writer.Write(append(append([]byte(nil), frame...), '\n'))
	return err
}
