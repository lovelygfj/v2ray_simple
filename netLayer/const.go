package netLayer

const UnknownNetwork = 0
const (
	// Transport Layer Protocols, 使用uint16 mask，所以最多支持16种
	TCP uint16 = 1 << iota
	UDP
	Dual //use both tcp and udp
	UNIX //unix domain socket
	IP
	Raw_socket
	KCP
	Quic //quic是一个横跨多个层的协议，这里也算一个，毕竟与kcp类似

)

// 若字符串无法被解析为网络类型，则返回 UnknownNetwork
func StrToTransportProtocol(s string) uint16 {
	switch s {
	case "tcp", "tcp4", "tcp6", "TCP", "TCP4", "TCP6":
		return TCP
	case "ip", "ipv4", "ipv6", "ip4", "ip6":
		return IP
	case "udp", "udp4", "udp6", "UDP", "UDP4", "UDP6":
		return UDP
	case DualNetworkName, "mix", "mixed", "Mix", "MIX":
		return Dual
	case "unix", "Unix", "UNIX":
		return UNIX
	case "raw", "RAW":
		return Raw_socket
	case "kcp", "KCP":
		return KCP
	case "quic", "Quic", "QUIC":
		return Quic
	}
	return UnknownNetwork
}

func NetworkHasNoPortField(s string) bool {
	n := StrToTransportProtocol(s)
	switch n {
	case UNIX, IP, Raw_socket:
		return true
	}
	return false
}
