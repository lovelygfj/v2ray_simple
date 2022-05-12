/*Package http implements http proxy for proxy.Server.

Reference

rfc: https://datatracker.ietf.org/doc/html/rfc7231#section-4.3.6

about basic auth:

https://en.wikipedia.org/wiki/Basic_access_authentication


https://datatracker.ietf.org/doc/html/rfc7617

example header:

	Authorization: Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ==


*/
package http

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"

	"github.com/e1732a364fed/v2ray_simple/httpLayer"
	"github.com/e1732a364fed/v2ray_simple/netLayer"
	"github.com/e1732a364fed/v2ray_simple/proxy"
	"github.com/e1732a364fed/v2ray_simple/utils"
)

const Name = "http"

var (
	connectReturnBytes    = []byte("HTTP/1.1 200 Connection established\r\n\r\n")
	basicAuthValue_prefix = []byte("Basic ")

	proxyAuth_headerBytes = []byte("Proxy-Authorization")
)

func init() {
	proxy.RegisterServer(Name, &ServerCreator{})
}

type ServerCreator struct{}

func (ServerCreator) NewServerFromURL(u *url.URL) (proxy.Server, error) {

	s := &Server{}
	s.InitWithUrl(u)
	return s, nil
}

func (ServerCreator) NewServer(lc *proxy.ListenConf) (proxy.Server, error) {
	s := &Server{}
	if str := lc.Uuid; str != "" {
		s.InitWithStr(str)
	}
	return s, nil
}

//implements proxy.Server
type Server struct {
	proxy.Base

	utils.UserPass

	OnlyConnect bool //是否仅支持Connect命令; 如果为true, 则直接通过 GET http://xxx 这种请求不再被认为是有效的。

}

func (s *Server) CanFallback() bool {
	return true
}

func (*Server) Name() string {
	return Name
}

func (s *Server) Handshake(underlay net.Conn) (newconn net.Conn, _ netLayer.MsgConn, targetAddr netLayer.Addr, err error) {
	var bs = utils.GetMTU() //一般要获取请求信息，不需要那么长; 就算是http，加了path，也不用太长

	n := 0

	n, err = underlay.Read(bs[:])
	if err != nil {
		utils.PutBytes(bs)
		return
	}

	defer func() {
		if err != nil {
			err = utils.ErrBuffer{
				Buf: bytes.NewBuffer(bs[:n]),
				Err: err,
			}
		}
	}()

	//rfc: https://datatracker.ietf.org/doc/html/rfc7231#section-4.3.6
	// "CONNECT is intended only for use in requests to a proxy.  " 总之CONNECT命令专门用于代理.
	// GET如果 path也是带 http:// 头的话，也是可以的，但是这种只适用于http代理，无法用于https。

	_, method, path, headers, failreason := httpLayer.ParseH1Request(bs[:n], true)
	if failreason != 0 {
		err = utils.ErrInErr{ErrDesc: "get method/path failed", ErrDetail: utils.ErrInvalidData, Data: []any{method, failreason}}

		return
	}

	if s.Valid() {
		var ok bool
		for _, h := range headers {

			if bytes.Equal(h.Head, proxyAuth_headerBytes) {
				if !bytes.HasPrefix(h.Value, basicAuthValue_prefix) {
					break
				}
				bs := utils.GetMTU()
				n, err = base64.StdEncoding.Decode(bs, h.Value[len(basicAuthValue_prefix):])
				if err != nil {
					break
				}
				colonIndex := bytes.IndexByte(bs[:n], ':')
				if colonIndex < 0 {
					break
				}

				if bytes.Equal(bs[:colonIndex], s.UserID) && bytes.Equal(bs[colonIndex+1:n], s.Password) {
					ok = true
				}

				break
			}
		}
		if !ok {
			err = errors.New("http auth not pass")
			return
		}
	}

	var isCONNECT bool

	if method == "CONNECT" {
		isCONNECT = true
	}

	var addressStr string

	if isCONNECT {
		addressStr = path //实测都会自带:443, 也就不需要我们额外判断了

	} else {
		if s.OnlyConnect {
			err = errors.New("non-connect method not supported")
			return
		}

		hostPortURL, err2 := url.Parse(path)
		if err2 != nil {
			err = err2

			return
		}
		addressStr = hostPortURL.Host

		if !strings.Contains(hostPortURL.Host, ":") { //host不带端口， 默认80
			addressStr = hostPortURL.Host + ":80"
		}
	}

	targetAddr, err = netLayer.NewAddr(addressStr)
	if err != nil {

		return
	}
	//如果使用CONNECT方式进行代理，需先向客户端表示连接建立完毕
	if isCONNECT {
		underlay.Write(connectReturnBytes) //这个也是https代理的特征，所以不适合 公网使用

		//正常来说我们的服务器要先dial，dial成功之后再返回200，但是因为我们目前的架构是在main函数里dial，
		// 所以就直接写入了.

		//另外，nginx是没有实现 CONNECT的，不过有插件

		newconn = underlay

	} else {
		newconn = &ProxyConn{
			firstData: bs[:n],
			Conn:      underlay,
		}

	}
	return
}

//用于纯http的 代理，dial后，第一次要把客户端的数据原封不动发送给远程服务端
// 就是说，第一次从 ProxyConn Read时，读到的一定是之前读过的数据，原理有点像 fallback
type ProxyConn struct {
	net.Conn
	firstData []byte
	notFirst  bool
}

func (pc *ProxyConn) Read(p []byte) (int, error) {
	if pc.notFirst {
		return pc.Conn.Read(p)
	}
	pc.notFirst = true

	bs := pc.firstData
	pc.firstData = nil

	n := copy(p, bs)
	utils.PutBytes(bs)
	return n, nil
}

// ReadFrom implements the io.ReaderFrom ReadFrom method.
// 专门用于适配 tcp的splice.
func (pc *ProxyConn) ReadFrom(r io.Reader) (n int64, e error) {

	//pc.Conn肯定不是udp，但有可能是 unix domain socket。暂时先不考虑这种情况

	return pc.Conn.(*net.TCPConn).ReadFrom(r)
}
