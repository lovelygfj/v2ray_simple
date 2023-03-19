package simplesocks

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
	optionalReader io.Reader

	bufr         *bufio.Reader
	handshakeBuf *bytes.Buffer
	fullcone     bool

	upstreamUser utils.User
}

func NewUDPConn(conn net.Conn, optionalReader io.Reader) (uc *UDPConn) {
	uc = new(UDPConn)
	uc.Conn = conn
	if optionalReader != nil {
		uc.optionalReader = optionalReader
		uc.bufr = bufio.NewReader(optionalReader)
	} else {
		uc.bufr = bufio.NewReader(conn)
	}
	return
}

func (c *UDPConn) SetUser(u utils.User) {
	c.upstreamUser = u
}

func (c *UDPConn) IdentityStr() string {
	if c.upstreamUser != nil {
		return c.upstreamUser.IdentityStr()
	}
	return ""
}

func (c *UDPConn) IdentityBytes() []byte {
	if c.upstreamUser != nil {
		return c.upstreamUser.IdentityBytes()
	}
	return nil
}

func (c *UDPConn) AuthStr() string {
	if c.upstreamUser != nil {
		return c.upstreamUser.AuthStr()
	}
	return ""
}
func (c *UDPConn) AuthBytes() []byte {
	if c.upstreamUser != nil {
		return c.upstreamUser.AuthBytes()
	}
	return nil
}

func (u *UDPConn) Fullcone() bool {
	return u.fullcone
}
func (u *UDPConn) CloseConnWithRaddr(raddr netLayer.Addr) error {
	return u.Close()
}
func (u *UDPConn) ReadMsg() ([]byte, netLayer.Addr, error) {

	//simplesocks 文档里并没有提及udp如何传输，而在trojan-go的代码里, 发现simplesocks完全使用trojan的udp格式。
	// https://github.com/p4gefau1t/trojan-go/blob/2dc60f52e79ff8b910e78e444f1e80678e936450/tunnel/simplesocks/conn.go#L41
	// https://github.com/p4gefau1t/trojan-go/blob/2dc60f52e79ff8b910e78e444f1e80678e936450/tunnel/trojan/packet.go#L34
	//可以看到和trojan协议一样，长度后面要跟随 crlf

	addr, err := GetAddrFrom(u.bufr)
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
	n, err := io.ReadFull(u.bufr, bs)
	if err != nil {
		if n > 0 {
			return bs[:n], addr, err
		}
		return nil, addr, err
	}

	return bs[:n], addr, nil
}

func (u *UDPConn) WriteMsg(bs []byte, addr netLayer.Addr) error {

	var buf *bytes.Buffer
	if u.handshakeBuf != nil {
		buf = u.handshakeBuf
		u.handshakeBuf = nil
	} else {
		buf = utils.GetBuf()
	}
	abs, atype := addr.AddressBytes()

	atype = netLayer.ATypeToSocks5Standard(atype)
	buf.WriteByte(netLayer.ATypeToSocks5Standard(atype))
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
