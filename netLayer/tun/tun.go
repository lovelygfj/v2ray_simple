/*
Packages tun provides utilities for tun.

tun包提供 创建tun设备的方法，以及监听tun，将数据解析为tcp/udp数据的方法。

tun 工作在第三层 IP层上。

我们基本上抄了 xjasonlyu/tun2socks, 因此把GPL证书放在了本包的文件夹中

本来最好是直接import的，但是目前（22.12.18）tun2socks的最新代码还没有打tag，而老代码又不可用，所以只能先复制过来。

windows中,
需要从 https://www.wintun.net/ 下载 wintun.dll 放到vs可执行文件旁边
*/
package tun

import (
	"errors"
	"io"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/e1732a364fed/v2ray_simple/netLayer"
	"github.com/e1732a364fed/v2ray_simple/netLayer/tun/device"
	"github.com/e1732a364fed/v2ray_simple/netLayer/tun/device/fdbased"
	"github.com/e1732a364fed/v2ray_simple/netLayer/tun/device/tun"
	"github.com/e1732a364fed/v2ray_simple/netLayer/tun/option"
	"github.com/e1732a364fed/v2ray_simple/utils"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

// 若name为空则会返回错误. 若name可转换为数字，则会将其解析为fd 号
func Open(name string) (device.Device, error) {
	if name == "" {
		return nil, errors.New("tun: dev name can't be empty")
	}

	_, err := strconv.Atoi(name)
	if err == nil {
		return fdbased.Open(name, uint32(utils.MTU))
	}

	return tun.Open(name, uint32(utils.MTU))
}

type StackCloser struct {
	*stack.Stack
}

func (sc *StackCloser) Close() error {
	sc.Stack.Close()
	//sc.Stack.Wait() //这个会卡住; 经测试，不调用它也不影响什么
	return nil
}

// 非阻塞
func Listen(dev device.Device, tcpFunc func(netLayer.TCPRequestInfo), udpFunc func(netLayer.UDPRequestInfo)) (closer io.Closer, err error) {

	s := stack.New(stack.Options{
		NetworkProtocols: []stack.NetworkProtocolFactory{
			ipv4.NewProtocol,
			ipv6.NewProtocol,
		},
		TransportProtocols: []stack.TransportProtocolFactory{
			tcp.NewProtocol,
			udp.NewProtocol,
			icmp.NewProtocol4,
			icmp.NewProtocol6,
		},
	})

	closer = &StackCloser{Stack: s}

	opts := []option.Option{option.WithDefault()}

	for _, opt := range opts {
		if err = opt(s); err != nil {
			return
		}
	}

	nicID := tcpip.NICID(s.UniqueID())

	if ex := s.CreateNICWithOptions(nicID, dev,
		stack.NICOptions{
			Disabled: false,
			// If no queueing discipline was specified
			// provide a stub implementation that just
			// delegates to the lower link endpoint.
			QDisc: nil,
		}); ex != nil {
		err = utils.ErrInErr{ErrDesc: ex.String()}
		return
	}

	const defaultWndSize = 0
	const maxConnAttempts int = 2048

	tcpForwarder := tcp.NewForwarder(s, defaultWndSize, maxConnAttempts, func(r *tcp.ForwarderRequest) {
		var (
			wq  waiter.Queue
			ep  tcpip.Endpoint
			err tcpip.Error
			id  = r.ID()
		)

		// Perform a TCP three-way handshake.
		ep, err = r.CreateEndpoint(&wq)
		if err != nil {
			// RST: prevent potential half-open TCP connection leak.
			r.Complete(true)
			return
		}

		setSocketOptions(s, ep)

		tcpConn := gonet.NewTCPConn(&wq, ep)

		info := netLayer.TCPRequestInfo{
			Conn: tcpConn,

			//比较反直觉
			Target: netLayer.Addr{
				Network: "tcp",
				IP:      net.IP(id.LocalAddress),
				Port:    int(id.LocalPort),
			},
		}

		// log.Printf("forward tcp request %s:%d->%s:%d\n",
		// 	id.RemoteAddress, id.RemotePort, id.LocalAddress, id.LocalPort)

		go tcpFunc(info)

		r.Complete(false)
	})
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpForwarder.HandlePacket)

	udpForwarder := udp.NewForwarder(s, func(r *udp.ForwarderRequest) {
		var (
			wq waiter.Queue
			id = r.ID()
		)
		ep, err := r.CreateEndpoint(&wq)
		if err != nil {
			log.Printf("tun Err, udp forwarder request %s:%d->%s:%d: %\n",
				id.RemoteAddress, id.RemotePort, id.LocalAddress, id.LocalPort, err)
			return
		}

		udpConn := gonet.NewUDPConn(s, &wq, ep)

		ad := netLayer.Addr{
			Network: "udp",
			IP:      net.IP(id.LocalAddress),
			Port:    int(id.LocalPort),
		}

		info := netLayer.UDPRequestInfo{
			MsgConn: &UdpMsgConn{
				PacketConn: udpConn,
				RealTarget: ad,
			},
			Target: ad,
		}

		go udpFunc(info)
	})
	s.SetTransportProtocolHandler(udp.ProtocolNumber, udpForwarder.HandlePacket)

	s.SetPromiscuousMode(nicID, true) //必须调用这个,否则tun什么也收不到
	s.SetSpoofing(nicID, true)

	s.SetRouteTable([]tcpip.Route{
		{
			Destination: header.IPv4EmptySubnet,
			NIC:         nicID,
		},
		{
			Destination: header.IPv6EmptySubnet,
			NIC:         nicID,
		},
	})

	return
}

func setSocketOptions(s *stack.Stack, ep tcpip.Endpoint) tcpip.Error {
	{ /* TCP keepalive options */
		ep.SocketOptions().SetKeepAlive(true)

		const tcpKeepaliveIdle time.Duration = time.Minute

		idle := tcpip.KeepaliveIdleOption(tcpKeepaliveIdle)
		if err := ep.SetSockOpt(&idle); err != nil {
			return err
		}

		const tcpKeepaliveInterval time.Duration = 30 * time.Second
		interval := tcpip.KeepaliveIntervalOption(tcpKeepaliveInterval)
		if err := ep.SetSockOpt(&interval); err != nil {
			return err
		}

		const tcpKeepaliveCount int = 9
		if err := ep.SetSockOptInt(tcpip.KeepaliveCountOption, tcpKeepaliveCount); err != nil {
			return err
		}
	}
	{ /* TCP recv/send buffer size */
		var ss tcpip.TCPSendBufferSizeRangeOption
		if err := s.TransportProtocolOption(header.TCPProtocolNumber, &ss); err == nil {
			ep.SocketOptions().SetReceiveBufferSize(int64(ss.Default), false)
		}

		var rs tcpip.TCPReceiveBufferSizeRangeOption
		if err := s.TransportProtocolOption(header.TCPProtocolNumber, &rs); err == nil {
			ep.SocketOptions().SetReceiveBufferSize(int64(rs.Default), false)
		}
	}
	return nil
}

// Wraps net.PacketConn and implements MsgConn
type UdpMsgConn struct {
	net.PacketConn
	RealTarget netLayer.Addr

	tunSrcAddr net.Addr
}

func (mc *UdpMsgConn) ReadMsg() ([]byte, netLayer.Addr, error) {
	bs := utils.GetPacket()
	n, ad, err := mc.ReadFrom(bs)
	if err != nil {
		return nil, mc.RealTarget, err
	}
	mc.tunSrcAddr = ad

	return bs[:n], mc.RealTarget, nil
}

func (mc *UdpMsgConn) WriteMsg(p []byte, peera netLayer.Addr) error {
	//这里的peera是 远程地址，不是我们要写向的地址。在tun中我们要发向之前的tun的地址

	//笔记：那么在哪里传回远程地址的信息呢，如果不设该信息，不就无法进行fullcone了吗？

	//根据下面讨论，果然，这样不行。
	//https://github.com/xjasonlyu/tun2socks/issues/112

	// 看来似乎不应该采用tun2socks目前重构后的方法而应该用它在2.4.0之前的旧方法
	// 然而旧方法所使用的 gvisor包已经过时了。我尝试了一次，失败了，代码放在 tun_failed中备用。

	_, err := mc.WriteTo(p, mc.tunSrcAddr)

	return err
}
func (mc *UdpMsgConn) CloseConnWithRaddr(raddr netLayer.Addr) error {
	return mc.PacketConn.Close()

}
func (mc *UdpMsgConn) Close() error {
	return mc.PacketConn.Close()
}
func (mc *UdpMsgConn) Fullcone() bool {
	return false
}
