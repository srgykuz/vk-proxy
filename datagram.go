package main

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"time"
)

const (
	commandConnect int16 = iota + 1
	commandForward
	commandClose
)

var (
	errDatagramMalformed = errors.New("datagram is malformed")
)

var deviceID = time.Now().UnixMilli()

type datagram struct {
	version  uint16
	checksum uint32
	device   int64
	session  int32
	number   int32
	command  int16
	payload  []byte
}

func (dg datagram) String() string {
	sumShort := dg.checksum % 1000
	devShort := dg.device % 1000

	return fmt.Sprintf(
		"ver=%v sum=%v dev=%v ses=%v num=%v cmd=%v pld=%v",
		dg.version, sumShort, devShort, dg.session, dg.number, dg.command, len(dg.payload),
	)
}

func (dg datagram) isLoopback() bool {
	return dg.device == deviceID
}

func (dg datagram) isZero() bool {
	return dg.version == 0
}

func (dg datagram) clone() datagram {
	pld := make([]byte, len(dg.payload))
	copy(pld, dg.payload)
	dg.payload = pld

	return dg
}

func newDatagram(ses int32, num int32, cmd int16, pld []byte) datagram {
	return datagram{
		version:  1,
		checksum: 0,
		device:   deviceID,
		session:  ses,
		number:   num,
		command:  cmd,
		payload:  pld,
	}
}

func encodeDatagram(dg datagram) string {
	data := make([]byte, 0, 24+len(dg.payload))

	data = binary.BigEndian.AppendUint16(data, dg.version)
	data = binary.BigEndian.AppendUint32(data, dg.checksum)
	data = binary.BigEndian.AppendUint64(data, uint64(dg.device))
	data = binary.BigEndian.AppendUint32(data, uint32(dg.session))
	data = binary.BigEndian.AppendUint32(data, uint32(dg.number))
	data = binary.BigEndian.AppendUint16(data, uint16(dg.command))
	data = append(data, dg.payload...)

	crc := crc32.ChecksumIEEE(data)
	binary.BigEndian.PutUint32(data[2:6], crc)

	s := base64.StdEncoding.EncodeToString(data)

	return s
}

func decodeDatagram(s string) (datagram, error) {
	data, err := base64.StdEncoding.DecodeString(s)

	if err != nil {
		return datagram{}, err
	}

	if len(data) < 24 {
		return datagram{}, errDatagramMalformed
	}

	ver := binary.BigEndian.Uint16(data[0:2])
	sum := binary.BigEndian.Uint32(data[2:6])
	dev := int64(binary.BigEndian.Uint64(data[6:14]))
	ses := int32(binary.BigEndian.Uint32(data[14:18]))
	num := int32(binary.BigEndian.Uint32(data[18:22]))
	cmd := int16(binary.BigEndian.Uint16(data[22:24]))
	pld := data[24:]

	binary.BigEndian.PutUint32(data[2:6], 0)
	crc := crc32.ChecksumIEEE(data)

	if sum != crc {
		return datagram{}, errDatagramMalformed
	}

	dg := datagram{
		version:  ver,
		checksum: sum,
		device:   dev,
		session:  ses,
		number:   num,
		command:  cmd,
		payload:  pld,
	}

	return dg, nil
}

type payloadConnect struct {
	host string
	port uint16
}

func (pld *payloadConnect) encode() []byte {
	data := []byte(pld.host)
	data = binary.BigEndian.AppendUint16(data, pld.port)

	return data
}

func (pld *payloadConnect) decode(data []byte) error {
	if len(data) < 2 {
		return errDatagramMalformed
	}

	pld.host = string(data[:len(data)-2])
	pld.port = binary.BigEndian.Uint16(data[len(data)-2:])

	return nil
}
