package main

import (
	"fmt"
	"net"
	"time"
)

func sendRconCommand(host string, port int, password, command string) (string, error) {
	addr := fmt.Sprintf("%s:%d", host, port)

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("rcon connect failed: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	if err := rconSend(conn, 1, 3, password); err != nil {
		return "", fmt.Errorf("rcon auth send failed: %w", err)
	}

	authResp, err := rconRead(conn)
	if err != nil {
		return "", fmt.Errorf("rcon auth read failed: %w", err)
	}
	if authResp.id == -1 {
		return "", fmt.Errorf("rcon auth failed: wrong password")
	}

	if err := rconSend(conn, 2, 2, command); err != nil {
		return "", fmt.Errorf("rcon command send failed: %w", err)
	}

	resp, err := rconRead(conn)
	if err != nil {
		return "", fmt.Errorf("rcon command read failed: %w", err)
	}

	return resp.body, nil
}

type rconPacket struct {
	size int32
	id   int32
	typ  int32
	body string
}

func rconSend(conn net.Conn, id, typ int32, body string) error {
	bodyBytes := []byte(body)
	size := int32(4 + 4 + len(bodyBytes) + 2)

	buf := make([]byte, 4+size)
	writeInt32(buf[0:], size)
	writeInt32(buf[4:], id)
	writeInt32(buf[8:], typ)
	copy(buf[12:], bodyBytes)

	_, err := conn.Write(buf)
	return err
}

func rconRead(conn net.Conn) (rconPacket, error) {
	var p rconPacket

	sizeBuf := make([]byte, 4)
	if _, err := readFull(conn, sizeBuf); err != nil {
		return p, err
	}
	p.size = readInt32(sizeBuf)

	if p.size < 10 || p.size > 4096 {
		return p, fmt.Errorf("invalid packet size: %d", p.size)
	}

	rest := make([]byte, p.size)
	if _, err := readFull(conn, rest); err != nil {
		return p, err
	}

	p.id = readInt32(rest[0:])
	p.typ = readInt32(rest[4:])

	bodyEnd := len(rest) - 2
	if bodyEnd > 8 {
		p.body = string(rest[8:bodyEnd])
	}

	return p, nil
}

func readFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func writeInt32(buf []byte, v int32) {
	buf[0] = byte(v)
	buf[1] = byte(v >> 8)
	buf[2] = byte(v >> 16)
	buf[3] = byte(v >> 24)
}

func readInt32(buf []byte) int32 {
	return int32(buf[0]) | int32(buf[1])<<8 | int32(buf[2])<<16 | int32(buf[3])<<24
}
