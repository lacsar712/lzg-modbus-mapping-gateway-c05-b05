#!/usr/bin/env python3
"""Minimal Modbus TCP server with pre-seeded holding registers."""

from __future__ import annotations

import socket
import struct
import threading

HOST = "0.0.0.0"
PORT = 5020
NUM_REGS = 200

# Holding registers (uint16), big-endian register values
holding = [0] * NUM_REGS


def pack_float32_abcd(value: float, start: int) -> None:
    raw = struct.pack(">f", value)
    holding[start] = (raw[0] << 8) | raw[1]
    holding[start + 1] = (raw[2] << 8) | raw[3]


def pack_float32_cdab(value: float, start: int) -> None:
    raw = struct.pack(">f", value)
    # word-swapped: CD AB
    holding[start] = (raw[2] << 8) | raw[3]
    holding[start + 1] = (raw[0] << 8) | raw[1]


def seed() -> None:
    # motor_rpm @ 0: float32_abcd = 1500.0
    pack_float32_abcd(1500.0, 0)
    # temperature @ 10: float32_cdab = 36.5
    pack_float32_cdab(36.5, 10)
    # pressure @ 20: int16 = 120
    holding[20] = 120 & 0xFFFF
    # status_word @ 30: uint16 = 0x00A5
    holding[30] = 0x00A5
    # run_flag @ 40 bit0: bool — set bit 0
    holding[40] = 0x0001
    # setpoint @ 50: float32_abcd = 80.0
    pack_float32_abcd(80.0, 50)
    print(f"[mock-plc] seeded {NUM_REGS} holding registers", flush=True)


def handle_pdu(unit_id: int, pdu: bytes) -> bytes:
    if not pdu:
        return b""
    fc = pdu[0]
    if fc == 0x03:  # Read Holding Registers
        addr = (pdu[1] << 8) | pdu[2]
        qty = (pdu[3] << 8) | pdu[4]
        if qty < 1 or qty > 125 or addr + qty > NUM_REGS:
            return bytes([0x83, 0x02])  # illegal address
        data = bytearray([0x03, qty * 2])
        for i in range(qty):
            v = holding[addr + i] & 0xFFFF
            data.append((v >> 8) & 0xFF)
            data.append(v & 0xFF)
        return bytes(data)

    if fc == 0x06:  # Write Single Register
        addr = (pdu[1] << 8) | pdu[2]
        value = (pdu[3] << 8) | pdu[4]
        if addr >= NUM_REGS:
            return bytes([0x86, 0x02])
        holding[addr] = value & 0xFFFF
        return pdu[:5]  # echo

    if fc == 0x10:  # Write Multiple Registers
        addr = (pdu[1] << 8) | pdu[2]
        qty = (pdu[3] << 8) | pdu[4]
        byte_count = pdu[5]
        if qty < 1 or qty > 123 or addr + qty > NUM_REGS or byte_count != qty * 2:
            return bytes([0x90, 0x02])
        for i in range(qty):
            off = 6 + i * 2
            holding[addr + i] = ((pdu[off] << 8) | pdu[off + 1]) & 0xFFFF
        return bytes([0x10, (addr >> 8) & 0xFF, addr & 0xFF, (qty >> 8) & 0xFF, qty & 0xFF])

    return bytes([fc | 0x80, 0x01])  # illegal function


def handle_client(conn: socket.socket, addr) -> None:
    print(f"[mock-plc] client connected {addr}", flush=True)
    try:
        while True:
            header = b""
            while len(header) < 7:
                chunk = conn.recv(7 - len(header))
                if not chunk:
                    return
                header += chunk
            tid = header[0:2]
            proto = header[2:4]
            length = (header[4] << 8) | header[5]
            unit_id = header[6]
            if proto != b"\x00\x00" or length < 1:
                return
            remaining = length - 1
            pdu = b""
            while len(pdu) < remaining:
                chunk = conn.recv(remaining - len(pdu))
                if not chunk:
                    return
                pdu += chunk
            resp_pdu = handle_pdu(unit_id, pdu)
            resp_len = 1 + len(resp_pdu)
            resp = tid + b"\x00\x00" + bytes([(resp_len >> 8) & 0xFF, resp_len & 0xFF, unit_id]) + resp_pdu
            conn.sendall(resp)
    except Exception as exc:  # noqa: BLE001
        print(f"[mock-plc] client error {addr}: {exc}", flush=True)
    finally:
        conn.close()
        print(f"[mock-plc] client closed {addr}", flush=True)


def main() -> None:
    seed()
    srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    srv.bind((HOST, PORT))
    srv.listen(32)
    print(f"[mock-plc] listening on {HOST}:{PORT}", flush=True)
    while True:
        conn, addr = srv.accept()
        t = threading.Thread(target=handle_client, args=(conn, addr), daemon=True)
        t.start()


if __name__ == "__main__":
    main()
