package main

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const (
	commandConnect int16 = iota + 1
	commandForward
	commandClose
)

var deviceID = time.Now().UnixMilli()

type datagram struct {
	version int16
	device  int64
	session int32
	number  int32
	command int16
	payload []byte
}

func (dg datagram) String() string {
	devShort := dg.device % 1000

	return fmt.Sprintf(
		"ver=%v dev=%v ses=%v num=%v cmd=%v pld=%v",
		dg.version, devShort, dg.session, dg.number, dg.command, len(dg.payload),
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
		version: 1,
		device:  deviceID,
		session: ses,
		number:  num,
		command: cmd,
		payload: pld,
	}
}

func encodeDatagram(dg datagram) string {
	data := []byte{}

	data = binary.BigEndian.AppendUint16(data, uint16(dg.version))
	data = binary.BigEndian.AppendUint64(data, uint64(dg.device))
	data = binary.BigEndian.AppendUint32(data, uint32(dg.session))
	data = binary.BigEndian.AppendUint32(data, uint32(dg.number))
	data = binary.BigEndian.AppendUint16(data, uint16(dg.command))
	data = append(data, dg.payload...)

	s := base64.StdEncoding.EncodeToString(data)

	return s
}

func decodeDatagram(s string) (datagram, error) {
	data, err := base64.StdEncoding.DecodeString(s)

	if err != nil {
		return datagram{}, err
	}

	if len(data) < 20 {
		return datagram{}, errors.New("malformed datagram")
	}

	ver := int16(binary.BigEndian.Uint16(data[0:2]))
	dev := int64(binary.BigEndian.Uint64(data[2:10]))
	ses := int32(binary.BigEndian.Uint32(data[10:14]))
	num := int32(binary.BigEndian.Uint32(data[14:18]))
	cmd := int16(binary.BigEndian.Uint16(data[18:20]))
	pld := data[20:]

	dg := datagram{
		version: ver,
		device:  dev,
		session: ses,
		number:  num,
		command: cmd,
		payload: pld,
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
		return errors.New("malformed payload")
	}

	hostb := data[:len(data)-2]
	pld.host = string(hostb)

	portb := data[len(data)-2:]
	pld.port = binary.BigEndian.Uint16(portb)

	return nil
}
