// Package tproxy implements proxy.Server for tproxy.
package tproxy

import (
	"io"
	"net"
	"net/url"
	"sync"

	"github.com/e1732a364fed/v2ray_simple/netLayer"
	"github.com/e1732a364fed/v2ray_simple/netLayer/tproxy"
	"github.com/e1732a364fed/v2ray_simple/proxy"
	"github.com/e1732a364fed/v2ray_simple/utils"
	"go.uber.org/zap"
)

const name = "tproxy"

func init() {
	proxy.RegisterServer(name, &ServerCreator{})
}

type ServerCreator struct{ proxy.CreatorCommonStruct }

func (ServerCreator) URLToListenConf(url *url.URL, lc *proxy.ListenConf, format int) (*proxy.ListenConf, error) {
	if lc == nil {
		return nil, utils.ErrNilParameter
	}

	return lc, nil
}

func (ServerCreator) NewServer(lc *proxy.ListenConf) (proxy.Server, error) {

	s := &Server{}
	if thing := lc.Extra["auto_iptables"]; thing != nil {
		if auto, ok := utils.AnyToBool(thing); ok && auto {
			s.shouldSetIPTable = true
		}
	}
	return s, nil
}

func (ServerCreator) AfterCommonConfServer(ps proxy.Server) (err error) {
	s := ps.(*Server)
	if s.Sockopt != nil {
		s.Sockopt.TProxy = true
	} else {
		s.Sockopt = &netLayer.Sockopt{TProxy: true}
	}
	if s.shouldSetIPTable {
		err = tproxy.SetRouteByPort(s.ListenConf.Port)
	}
	return
}

// implements proxy.ListenerServer
type Server struct {
	proxy.Base

	shouldSetIPTable bool

	tm *tproxy.Machine
	sync.Once
}

func NewServer() (proxy.Server, error) {
	d := &Server{}
	return d, nil
}
func (*Server) Name() string { return name }

func (s *Server) SelfListen() (is bool, tcp, udp int) {
	switch n := s.Network(); n {
	case "", netLayer.DualNetworkName:
		tcp = 1
		udp = 1

	case "tcp":
		tcp = 1
		udp = -1
	case "udp":
		udp = 1
		tcp = -1
	}

	is = true

	return
}

func (s *Server) Close() error {
	s.Stop()
	return nil
}

func (s *Server) Stop() {
	s.Once.Do(func() {
		s.tm.Stop()
	})

}

func (s *Server) StartListen(tcpFunc func(netLayer.TCPRequestInfo), udpFunc func(netLayer.UDPRequestInfo)) io.Closer {

	tm := new(tproxy.Machine)

	_, lt, lu := s.SelfListen()

	if lt > 0 {

		lis, err := netLayer.ListenAndAccept("tcp", s.Addr, s.Sockopt, 0, func(conn net.Conn) {
			tcpconn := conn.(*net.TCPConn)
			targetAddr := tproxy.HandshakeTCP(tcpconn)

			info := netLayer.TCPRequestInfo{
				Conn:   tcpconn,
				Target: targetAddr,
			}

			if ce := utils.CanLogInfo("TProxy got new tcp"); ce != nil {
				ce.Write(zap.String("->", targetAddr.String()))
			}
			if tm.Closed() {
				return
			}
			go tcpFunc(info)
		})
		if err != nil {
			if ce := utils.CanLogErr("TProxy listen tcp failed"); ce != nil {
				ce.Write(zap.Error(err))
			}
		}
		tm.Listener = lis

	}

	if lu > 0 {

		ad, err := netLayer.NewAddr(s.Addr)
		if err != nil {
			if ce := utils.CanLogErr("TProxy convert listen addr failed"); ce != nil {
				ce.Write(zap.Error(err))
			}
		}

		uconn, err := ad.ListenUDP_withOpt(s.Sockopt)
		if err != nil {
			if ce := utils.CanLogErr("TProxy listen udp failed"); ce != nil {
				ce.Write(zap.Error(err))
			}
			return nil
		}

		udpConn := uconn.(*net.UDPConn)
		tm.Addr = ad
		tm.UDPConn = udpConn

		go func() {
			for {
				msgConn, raddr, err := tm.HandshakeUDP(udpConn)
				if err != nil {
					if ce := utils.CanLogErr("TProxy startLoopUDP loop read failed"); ce != nil {
						ce.Write(zap.Error(err))
					}
					break
				} else {
					if ce := utils.CanLogInfo("TProxy got new udp"); ce != nil {
						ce.Write(zap.String("->", raddr.String()))
					}
				}
				msgConn.SetFullcone(s.IsFullcone)
				if tm.Closed() {
					return
				}

				go udpFunc(netLayer.UDPRequestInfo{MsgConn: msgConn, Target: raddr})

			}
		}()

	}

	tm.Init()
	s.tm = tm

	return s
}

func (s *Server) Handshake(underlay net.Conn) (net.Conn, netLayer.MsgConn, netLayer.Addr, error) {
	return nil, nil, netLayer.Addr{}, utils.ErrUnImplemented
}
