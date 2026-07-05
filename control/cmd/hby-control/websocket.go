package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

func (a *app) handleConsoleWS(w http.ResponseWriter, r *http.Request) {
	conn, reader, err := upgradeWebSocket(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer conn.Close()

	writer := &wsWriter{conn: conn}
	if snapshot := a.sup.logs.Snapshot(); len(snapshot) > 0 {
		_ = writer.WriteJSON(map[string]string{"type": "output", "data": string(snapshot)})
	}

	ch, unsubscribe := a.sup.logs.Subscribe()
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for event := range ch {
			msg := map[string]string{"type": event.Type}
			if event.Type == "output" || event.Type == "error" {
				msg["data"] = string(event.Data)
			}
			if err := writer.WriteJSON(msg); err != nil {
				return
			}
		}
	}()

	for {
		op, payload, err := readWSFrame(reader)
		if err != nil {
			return
		}
		switch op {
		case wsOpcodeClose:
			_ = writer.WriteClose()
			return
		case wsOpcodePing:
			_ = writer.WritePong(payload)
		case wsOpcodeText:
			var msg struct {
				Type string `json:"type"`
				Data string `json:"data"`
			}
			if json.Unmarshal(payload, &msg) == nil && msg.Type == "input" {
				if err := a.sup.SendInput(msg.Data); err != nil {
					_ = writer.WriteJSON(map[string]string{"type": "error", "data": err.Error()})
				}
			}
		}
		select {
		case <-done:
			return
		default:
		}
	}
}

const (
	wsGUID        = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	wsOpcodeText  = 1
	wsOpcodeClose = 8
	wsOpcodePing  = 9
	wsOpcodePong  = 10
)

func upgradeWebSocket(w http.ResponseWriter, r *http.Request) (net.Conn, *bufio.Reader, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, nil, errors.New("missing websocket upgrade")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, nil, errors.New("missing websocket key")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("websocket hijacking is not supported")
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(sum[:])
	headers := bytes.NewBuffer(nil)
	fmt.Fprintf(headers, "HTTP/1.1 101 Switching Protocols\r\n")
	fmt.Fprintf(headers, "Upgrade: websocket\r\n")
	fmt.Fprintf(headers, "Connection: Upgrade\r\n")
	fmt.Fprintf(headers, "Sec-WebSocket-Accept: %s\r\n\r\n", accept)
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}
	if _, err := conn.Write(headers.Bytes()); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return conn, rw.Reader, nil
}

func readWSFrame(r *bufio.Reader) (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}
	opcode := header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(r, ext); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(r, ext); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext)
	}
	if length > 1024*1024 {
		return 0, nil, errors.New("websocket frame too large")
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

type wsWriter struct {
	mu   sync.Mutex
	conn net.Conn
}

func (w *wsWriter) WriteJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.writeFrame(wsOpcodeText, data)
}

func (w *wsWriter) WritePong(payload []byte) error {
	return w.writeFrame(wsOpcodePong, payload)
}

func (w *wsWriter) WriteClose() error {
	return w.writeFrame(wsOpcodeClose, nil)
}

func (w *wsWriter) writeFrame(opcode byte, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	header := []byte{0x80 | opcode}
	l := len(payload)
	switch {
	case l < 126:
		header = append(header, byte(l))
	case l <= 65535:
		header = append(header, 126, byte(l>>8), byte(l))
	default:
		header = append(header, 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(l))
		header = append(header, ext[:]...)
	}
	if _, err := w.conn.Write(header); err != nil {
		return err
	}
	_, err := w.conn.Write(payload)
	return err
}
