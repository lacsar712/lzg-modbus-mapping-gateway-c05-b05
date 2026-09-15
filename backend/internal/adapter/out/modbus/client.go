package modbus

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync/atomic"
	"time"
)

// Client is a simple Modbus TCP client (FC 0x03/0x06/0x10).
type Client struct {
	hostOverride string
	portOverride string
	txn          uint32
}

func NewClient() *Client {
	return &Client{
		hostOverride: os.Getenv("MODBUS_HOST_OVERRIDE"),
		portOverride: os.Getenv("MODBUS_PORT_OVERRIDE"),
	}
}

func (c *Client) resolveEndpoint(endpoint string) string {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		// maybe host only with default
		host = endpoint
		port = "502"
	}
	if c.hostOverride != "" {
		host = c.hostOverride
	}
	if c.portOverride != "" {
		port = c.portOverride
	}
	return net.JoinHostPort(host, port)
}

func (c *Client) nextTxn() uint16 {
	return uint16(atomic.AddUint32(&c.txn, 1))
}

func (c *Client) transact(endpoint string, unitID byte, timeoutMs int, pdu []byte) ([]byte, error) {
	addr := c.resolveEndpoint(endpoint)
	if timeoutMs <= 0 {
		timeoutMs = 2000
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	tid := c.nextTxn()
	length := uint16(1 + len(pdu))
	mbap := make([]byte, 7+len(pdu))
	binary.BigEndian.PutUint16(mbap[0:2], tid)
	binary.BigEndian.PutUint16(mbap[2:4], 0)
	binary.BigEndian.PutUint16(mbap[4:6], length)
	mbap[6] = unitID
	copy(mbap[7:], pdu)

	if _, err := conn.Write(mbap); err != nil {
		return nil, err
	}

	hdr := make([]byte, 7)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return nil, err
	}
	respLen := binary.BigEndian.Uint16(hdr[4:6])
	if respLen < 1 {
		return nil, fmt.Errorf("invalid mbap length")
	}
	body := make([]byte, respLen-1)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty PDU")
	}
	if body[0]&0x80 != 0 {
		code := byte(0)
		if len(body) > 1 {
			code = body[1]
		}
		return nil, fmt.Errorf("modbus exception fc=0x%02X code=%d", body[0]&0x7F, code)
	}
	return body, nil
}

func (c *Client) ReadHoldingRegisters(endpoint string, unitID byte, timeoutMs int, address, quantity uint16) ([]uint16, error) {
	pdu := []byte{
		0x03,
		byte(address >> 8), byte(address),
		byte(quantity >> 8), byte(quantity),
	}
	resp, err := c.transact(endpoint, unitID, timeoutMs, pdu)
	if err != nil {
		return nil, err
	}
	if len(resp) < 2 || resp[0] != 0x03 {
		return nil, fmt.Errorf("unexpected read response")
	}
	byteCount := int(resp[1])
	if len(resp) < 2+byteCount || byteCount != int(quantity)*2 {
		return nil, fmt.Errorf("bad byte count %d", byteCount)
	}
	out := make([]uint16, quantity)
	for i := 0; i < int(quantity); i++ {
		out[i] = binary.BigEndian.Uint16(resp[2+i*2 : 4+i*2])
	}
	return out, nil
}

func (c *Client) WriteSingleRegister(endpoint string, unitID byte, timeoutMs int, address, value uint16) error {
	pdu := []byte{
		0x06,
		byte(address >> 8), byte(address),
		byte(value >> 8), byte(value),
	}
	resp, err := c.transact(endpoint, unitID, timeoutMs, pdu)
	if err != nil {
		return err
	}
	if len(resp) < 5 || resp[0] != 0x06 {
		return fmt.Errorf("unexpected write single response")
	}
	return nil
}

func (c *Client) WriteMultipleRegisters(endpoint string, unitID byte, timeoutMs int, address uint16, values []uint16) error {
	if len(values) == 0 {
		return fmt.Errorf("no values")
	}
	qty := uint16(len(values))
	pdu := make([]byte, 6+len(values)*2)
	pdu[0] = 0x10
	pdu[1] = byte(address >> 8)
	pdu[2] = byte(address)
	pdu[3] = byte(qty >> 8)
	pdu[4] = byte(qty)
	pdu[5] = byte(len(values) * 2)
	for i, v := range values {
		binary.BigEndian.PutUint16(pdu[6+i*2:], v)
	}
	resp, err := c.transact(endpoint, unitID, timeoutMs, pdu)
	if err != nil {
		return err
	}
	if len(resp) < 5 || resp[0] != 0x10 {
		return fmt.Errorf("unexpected write multiple response: %v", resp)
	}
	return nil
}
