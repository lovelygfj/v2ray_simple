package trojan

import (
	"bufio"
	"bytes"
	"io"
	"net"

	"github.com/e1732a364fed/v2ray_simple/netLayer"
	"github.com/e1732a364fed/v2ray_simple/utils"
)

type UDPConn struct {
	net.Conn
	User
	optionalReader io.Reader

	bufr *bufio.Reader

	handshakeBuf *bytes.Buffer

	isfullcone bool
}

func NewUDPConn(conn net.Conn, optionalReader io.Reader, fullcone bool) (uc *UDPConn) {
	uc = new(UDPConn)
	uc.Conn = conn
	uc.isfullcone = fullcone
	if optionalReader != nil {
		uc.optionalReader = optionalReader
		uc.bufr = bufio.NewReader(optionalReader)
	} else {
		uc.bufr = bufio.NewReader(conn)
	}
	return
}

func (u *UDPConn) Fullcone() bool {
	return u.isfullcone
}
func (u *UDPConn) CloseConnWithRaddr(raddr netLayer.Addr) error {
	return u.Close()
}
func (u *UDPConn) ReadMsgFrom() ([]byte, netLayer.Addr, error) {

	addr, err := GetAddrFrom(u.bufr, false)
	if err != nil {
		return nil, addr, err
	}

	addr.Network = "udp"

	lb1, err := u.bufr.ReadByte()
	if err != nil {
		return nil, addr, err
	}
	lb2, err := u.bufr.ReadByte()
	if err != nil {
		return nil, addr, err
	}
	lenth := uint16(lb1)<<8 + uint16(lb2)
	if lenth == 0 {
		return nil, addr, utils.ErrInvalidData
	}

	cr_b, err := u.bufr.ReadByte()
	if err != nil {
		return nil, addr, err
	}
	lf_b, err := u.bufr.ReadByte()
	if err != nil {
		return nil, addr, err
	}
	if cr_b != crlf[0] || lf_b != crlf[1] {
		return nil, addr, utils.ErrInvalidData
	}

	bs := utils.GetBytes(int(lenth))
	n, err := io.ReadFull(u.bufr, bs) //如果不用 io.ReadFull, 一次最多读取到 4085 字节

	if err != nil {
		if n > 0 {
			return bs[:n], addr, err
		}
		return nil, addr, err
	}

	return bs[:n], addr, nil
}

func (u *UDPConn) WriteMsgTo(bs []byte, addr netLayer.Addr) error {
	var buf *bytes.Buffer
	if u.handshakeBuf != nil {
		buf = u.handshakeBuf
		u.handshakeBuf = nil
	} else {
		buf = utils.GetBuf()
	}

	abs, atype := addr.AddressBytes()

	atype = netLayer.ATypeToSocks5Standard(atype)

	buf.WriteByte(atype)
	buf.Write(abs)

	buf.WriteByte(byte(addr.Port >> 8))
	buf.WriteByte(byte(addr.Port << 8 >> 8))

	buf.WriteByte(byte(len(bs) >> 8))
	buf.WriteByte(byte(len(bs) << 8 >> 8))
	buf.Write(crlf)
	buf.Write(bs)

	_, err := u.Conn.Write(buf.Bytes())
	utils.PutBuf(buf)

	return err
}
