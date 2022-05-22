package socks5

import (
	"io"
	"net"
	"net/url"

	"github.com/e1732a364fed/v2ray_simple/netLayer"
	"github.com/e1732a364fed/v2ray_simple/proxy"
	"github.com/e1732a364fed/v2ray_simple/utils"
)

func init() {
	proxy.RegisterClient(Name, &ClientCreator{})
}

type ClientCreator struct{}

func (ClientCreator) NewClientFromURL(u *url.URL) (proxy.Client, error) {
	c := &Client{}
	c.InitWithUrl(u)
	return c, nil
}

func (ClientCreator) NewClient(dc *proxy.DialConf) (proxy.Client, error) {
	c := &Client{}
	if str := dc.Uuid; str != "" {
		c.InitWithStr(str)
	}
	return c, nil
}

type Client struct {
	proxy.Base
	utils.UserPass
}

func (*Client) Name() string {
	return Name
}

func (c *Client) Handshake(underlay net.Conn, firstPayload []byte, target netLayer.Addr) (result io.ReadWriteCloser, err error) {

	if underlay == nil {
		panic("socks5 client handshake, nil underlay is not allowed")
	}

	var ba [10]byte

	//握手阶段
	ba[0] = Version5
	ba[1] = 1

	var adoptedMethod byte

	if len(c.Password) > 0 && len(c.UserID) > 0 {
		adoptedMethod = AuthPassword
	} else {
		adoptedMethod = AuthNone

	}
	ba[2] = adoptedMethod

	_, err = underlay.Write(ba[:3])
	if err != nil {
		return
	}

	proxy.SetCommonReadTimeout(underlay)

	n, err := underlay.Read(ba[:])
	if err != nil {
		return
	}
	netLayer.PersistConn(underlay)

	if n != 2 || ba[0] != Version5 || ba[1] != adoptedMethod {
		return nil, utils.ErrInErr{ErrDesc: "socks5 client handshake,protocol err", Data: ba[1]}
	}
	if adoptedMethod == AuthPassword {
		buf := utils.GetBuf()
		buf.WriteByte(1)
		buf.WriteByte(byte(len(c.UserID)))
		buf.Write(c.UserID)
		buf.WriteByte(byte(len(c.Password)))
		buf.Write(c.Password)

		_, err = underlay.Write(buf.Bytes())
		utils.PutBuf(buf)
		if err != nil {
			return nil, err
		}
		proxy.SetCommonReadTimeout(underlay)

		n, err = underlay.Read(ba[:])
		if err != nil {
			return
		}
		netLayer.PersistConn(underlay)

		if n != 2 || ba[0] != 1 || ba[1] != 0 {
			return nil, utils.ErrInErr{ErrDesc: "socks5 client handshake,auth failed", Data: ba[1]}
		}
	}

	buf := utils.GetBuf()
	buf.WriteByte(Version5)
	buf.WriteByte(CmdConnect)
	buf.WriteByte(0)
	abs, atype := target.AddressBytes()

	buf.WriteByte(netLayer.ATypeToSocks5Standard(atype))
	buf.Write(abs)
	buf.WriteByte(byte(target.Port >> 8))
	buf.WriteByte(byte(target.Port << 8 >> 8))

	_, err = underlay.Write(buf.Bytes())
	utils.PutBuf(buf)
	if err != nil {
		return
	}

	proxy.SetCommonReadTimeout(underlay)
	n, err = underlay.Read(ba[:])

	if err != nil {
		return
	}
	netLayer.PersistConn(underlay)

	if n < 10 || ba[0] != 5 || ba[1] != 0 || ba[2] != 0 {
		return nil, utils.NumStrErr{Prefix: "socks5 client handshake failed when reading response", N: 2}

	}
	if len(firstPayload) > 0 {
		underlay.Write(firstPayload)

	}

	return underlay, nil

}

func (c *Client) EstablishUDPChannel(underlay net.Conn, firstPayload []byte, target netLayer.Addr) (netLayer.MsgConn, error) {
	var err error
	serverPort := 0
	serverPort, err = Client_EstablishUDPAssociate(underlay)
	if err != nil {
		return nil, err
	}

	ua, err := net.ResolveUDPAddr("udp", c.Addr)
	if err != nil {
		return nil, err
	}
	cpc := ClientUDPConn{
		associated:          true,
		ServerUDPPort_forMe: serverPort,
		ServerAddr: &net.TCPAddr{
			IP: ua.IP,
		},
		fullcone: c.IsFullcone,
	}
	cpc.UDPConn, err = net.DialUDP("udp", nil, ua)
	if err != nil {
		return nil, err
	}

	if len(firstPayload) == 0 {
		return &cpc, nil

	} else {
		return &cpc, cpc.WriteMsgTo(firstPayload, target)
	}
}
