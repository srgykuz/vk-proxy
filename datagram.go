package main

import (
	"bytes"
	"encoding/ascii85"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"time"
)

type (
	dgVer uint16
	dgSum uint32
	dgDev int64
	dgSes int32
	dgNum int32
	dgCmd int16
)

const datagramHeaderLen = 2 + 4 + 8 + 4 + 4 + 2

const (
	commandConnect dgCmd = iota + 1
	commandForward
	commandClose
	commandRetry
)

var (
	errDatagramMalformed = errors.New("datagram is malformed")
)

var datagramHeaderLenEncoded = newDatagram(0, 0, 0, nil).LenEncoded()
var deviceID = dgDev(time.Now().UnixMilli())

type datagram struct {
	version  dgVer
	checksum dgSum
	device   dgDev
	session  dgSes
	number   dgNum
	command  dgCmd
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

func (dg datagram) Len() int {
	return datagramHeaderLen + len(dg.payload)
}

func (dg datagram) LenEncoded() int {
	return 5 * int(math.Ceil(float64(dg.Len())/4))
}

func (dg datagram) isLoopback() bool {
	return dg.device == deviceID
}

func (dg datagram) isZero() bool {
	return dg.version == 0
}

func (dg datagram) clone() datagram {
	dg.payload = bytes.Clone(dg.payload)

	return dg
}

func datagramCalcMaxLen(maxLenEncoded int) int {
	max := 4 * float64(maxLenEncoded) / 5
	min := max - 4
	isValidBase85Len := maxLenEncoded%5 == 0

	if isValidBase85Len {
		return int(max)
	}

	return int(math.Ceil(min))
}

func newDatagram(ses dgSes, num dgNum, cmd dgCmd, pld []byte) datagram {
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

func encodeDatagram(dg datagram, enc int) string {
	data := make([]byte, 0, dg.Len())

	data = binary.BigEndian.AppendUint16(data, uint16(dg.version))
	data = binary.BigEndian.AppendUint32(data, uint32(dg.checksum))
	data = binary.BigEndian.AppendUint64(data, uint64(dg.device))
	data = binary.BigEndian.AppendUint32(data, uint32(dg.session))
	data = binary.BigEndian.AppendUint32(data, uint32(dg.number))
	data = binary.BigEndian.AppendUint16(data, uint16(dg.command))
	data = append(data, dg.payload...)

	crc := crc32.ChecksumIEEE(data)
	binary.BigEndian.PutUint32(data[2:6], crc)

	s := base85Encode(data, enc)

	return s
}

func decodeDatagram(s string) (datagram, error) {
	data, err := base85Decode(s)

	if err != nil {
		return datagram{}, err
	}

	if len(data) < datagramHeaderLen {
		return datagram{}, errDatagramMalformed
	}

	ver := binary.BigEndian.Uint16(data[0:2])
	sum := binary.BigEndian.Uint32(data[2:6])
	dev := binary.BigEndian.Uint64(data[6:14])
	ses := binary.BigEndian.Uint32(data[14:18])
	num := binary.BigEndian.Uint32(data[18:22])
	cmd := binary.BigEndian.Uint16(data[22:24])
	pld := data[datagramHeaderLen:]

	binary.BigEndian.PutUint32(data[2:6], 0)
	crc := crc32.ChecksumIEEE(data)

	if sum != crc {
		return datagram{}, errDatagramMalformed
	}

	dg := datagram{
		version:  dgVer(ver),
		checksum: dgSum(sum),
		device:   dgDev(dev),
		session:  dgSes(ses),
		number:   dgNum(num),
		command:  dgCmd(cmd),
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

type payloadRetry struct {
	number dgNum
}

func (pld *payloadRetry) encode() []byte {
	data := make([]byte, 4)

	binary.BigEndian.PutUint32(data, uint32(pld.number))

	return data
}

func (pld *payloadRetry) decode(data []byte) error {
	if len(data) < 4 {
		return errDatagramMalformed
	}

	pld.number = dgNum(binary.BigEndian.Uint32(data))

	return nil
}

const (
	datagramEncodingASCII = iota + 1
	datagramEncodingRU
)

var (
	base85CharsetASCII = []rune("!\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstu")
	base85CharsetRU    = []rune("абвгдеёжзийклмн0123456789опрстуфABCDEFGHIJKLMNOPQRSTUVWXYZхцчшщъabcdefghijklmnopqrstu")
)

func base85Encode(in []byte, enc int) string {
	dst := make([]byte, ascii85.MaxEncodedLen(len(in)))
	n := ascii85.Encode(dst, in)
	dst = dst[:n]

	if enc == datagramEncodingRU {
		dst = bytes.Map(func(r rune) rune {
			for i, rASCII := range base85CharsetASCII {
				rRU := base85CharsetRU[i]

				if r == rASCII {
					return rRU
				}
			}

			return r
		}, dst)
	}

	out := string(dst)

	return out
}

func base85Decode(in string) ([]byte, error) {
	enc := datagramEncodingASCII

	for _, r := range in {
		if r > 127 {
			enc = datagramEncodingRU
			break
		}
	}

	src := []byte(in)

	if enc == datagramEncodingRU {
		src = bytes.Map(func(r rune) rune {
			for i, rASCII := range base85CharsetASCII {
				rRU := base85CharsetRU[i]

				if r == rRU {
					return rASCII
				}
			}

			return r
		}, src)
	}

	dst := make([]byte, len(src))
	n, _, err := ascii85.Decode(dst, src, true)
	out := dst[:n]

	return out, err
}
