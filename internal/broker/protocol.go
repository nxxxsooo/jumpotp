package broker

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	protocolVersion = 1
	maxFrameBytes   = 8 * 1024
)

type request struct {
	Version int    `json:"version"`
	Profile string `json:"profile"`
	Target  string `json:"target"`
	Nonce   string `json:"nonce"`
}

type response struct {
	Version int    `json:"version"`
	Status  string `json:"status"`
	Kind    string `json:"kind,omitempty"`
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

func writeFrame(writer io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	defer zero(data)
	if len(data) > maxFrameBytes {
		return errors.New("broker frame is too large")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if _, err := writer.Write(header[:]); err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

func readFrame(reader io.Reader, value any) error {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > maxFrameBytes {
		return fmt.Errorf("invalid broker frame size %d", size)
	}
	data := make([]byte, size)
	defer zero(data)
	if _, err := io.ReadFull(reader, data); err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("decode broker frame: %w", err)
	}
	return nil
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
