package proxy

import (
	"errors"
	"io"
	"net"
	"net/url"

	"github.com/e1732a364fed/v2ray_simple/netLayer"
	"github.com/e1732a364fed/v2ray_simple/utils"
)

const (
	DirectName = "direct"
	DirectURL  = DirectName + "://"
)

// implements ClientCreator for direct
type DirectCreator struct{ CreatorCommonStruct }

// true
func (DirectCreator) MultiTransportLayer() bool {
	return true
}

func (DirectCreator) URLToDialConf(url *url.URL, iv *DialConf, format int) (*DialConf, error) {
	if iv != nil {
		return iv, nil
	} else {
		d := &DialConf{}

		return d, nil
	}

}

func (DirectCreator) NewClient(dc *DialConf) (Client, error) {
	d := &DirectClient{}

	if dc.Network == "" {
		dc.Network = netLayer.DualNetworkName
	}

	return d, nil
}

type DirectClient struct {
	Base
}

func (*DirectClient) Name() string { return DirectName }
func (*DirectClient) GetCreator() ClientCreator {
	return DirectCreator{}
}

// 若 underlay 为nil，则会对target进行拨号, 否则返回underlay本身
func (d *DirectClient) Handshake(underlay net.Conn, firstPayload []byte, target netLayer.Addr) (result io.ReadWriteCloser, err error) {
	if d.Network() == "udp" {
		return nil, errors.New("direct's network set to udp, but Handshake called")
	}

	if underlay == nil {

		result, err = d.Base.DialTCP(target)

	} else {
		result = underlay

	}
	if err != nil {
		return
	}
	if len(firstPayload) > 0 {
		_, err = result.Write(firstPayload)
		utils.PutBytes(firstPayload)

	}

	return

}

// direct的Client的 EstablishUDPChannel 直接 监听一个udp端口，无视传入的net.Conn.
// 这是因为要考虑到fullcone.
func (d *DirectClient) EstablishUDPChannel(_ net.Conn, firstPayload []byte, target netLayer.Addr) (netLayer.MsgConn, error) {
	if d.Network() == "tcp" {
		return nil, errors.New("direct's network set to tcp, but EstablishUDPChannel called")
	}

	if len(firstPayload) == 0 {

		return d.Base.DialUDP(target)

	} else {
		mc, err := d.Base.DialUDP(target)
		if err != nil {
			return nil, err
		}
		return mc, mc.WriteMsg(firstPayload, target)

	}
}
