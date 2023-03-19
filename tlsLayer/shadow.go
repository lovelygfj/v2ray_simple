package tlsLayer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/e1732a364fed/v2ray_simple/netLayer"
	"github.com/e1732a364fed/v2ray_simple/utils"
	"go.uber.org/zap"
)

//https://www.ihcblog.com/a-better-tls-obfs-proxy/
//https://github.com/ihciah/shadow-tls/blob/master/docs/protocol-cn.md

func getShadowTlsPasswordFromExtra(extra map[string]any) string {
	if len(extra) > 0 {
		if thing := extra["shadowtls_password"]; thing != nil {
			if str, ok := thing.(string); ok {
				return str
			}
		}
	}
	return ""
}

// 转发并判断tls1.2握手结束后直接返回
func shadowTls1(servername string, clientConn net.Conn) (err error) {
	var fakeConn net.Conn
	fakeConn, err = net.Dial("tcp", servername+":443")
	if err != nil {
		if ce := utils.CanLogErr("Failed shadowTls server fake dial server "); ce != nil {
			ce.Write(zap.Error(err))
		}
		return
	}
	if ce := utils.CanLogDebug("shadowTls ready to fake "); ce != nil {
		ce.Write()
	}

	var e1, e2 error

	finish1 := make(chan struct{})
	go func() {
		e1 = CopyTls12Handshake(true, fakeConn, clientConn)

		if ce := utils.CanLogDebug("shadowTls copy client end"); ce != nil {
			ce.Write(zap.Error(e1))
		}

		close(finish1)

	}()

	e2 = CopyTls12Handshake(false, clientConn, fakeConn)

	if ce := utils.CanLogDebug("shadowTls copy server end"); ce != nil {
		ce.Write(
			zap.Error(e2),
		)
	}

	<-finish1

	if e1 != nil || e2 != nil {
		e := utils.ErrList{}
		if e1 != nil {
			e.Add(utils.ErrItem{Index: 1, E: e1})
		}
		if e2 != nil {
			e.Add(utils.ErrItem{Index: 2, E: e2})
		}

		return e
	}

	if ce := utils.CanLogDebug("shadowTls fake ok "); ce != nil {
		ce.Write()
	}

	return
}

func shadowTls2(servername string, clientConn net.Conn, password string) (result *FakeAppDataConn, err error) {
	var fakeConn net.Conn
	fakeConn, err = net.Dial("tcp", servername+":443")
	if err != nil {
		if ce := utils.CanLogErr("Failed shadowTls2 server fake dial server "); ce != nil {
			ce.Write(zap.Error(err))
		}
		return
	}
	if ce := utils.CanLogDebug("shadowTls2 ready to fake "); ce != nil {
		ce.Write()
	}

	hashW := utils.NewHashWriter(clientConn, []byte(password))

	go io.Copy(hashW, fakeConn) //write real server response back to client

	var firstPayload *bytes.Buffer
	firstPayload, err = shadowCopyHandshakeClientToFake(fakeConn, clientConn, hashW)

	if err == nil {
		fakeConn.Close()

		if ce := utils.CanLogDebug("shadowTls2 fake ok!"); ce != nil {
			ce.Write()
		}

		realconn := &FakeAppDataConn{
			Conn: clientConn,

			OptionalReader:          firstPayload,
			OptionalReaderRemainLen: firstPayload.Len(),
		}

		return realconn, nil
	} else if err == utils.ErrFailed {
		if ce := utils.CanLogWarn("shadowTls2 fake failed!"); ce != nil {
			ce.Write()
		}

		hashW.StopHashing()
		go io.Copy(fakeConn, clientConn) //write client request to real server

		return nil, utils.ErrInErr{ErrDetail: netLayer.ErrDoNotClose, ErrDesc: "not real shadowTlsClient, fallback"}
	}
	return nil, err

}

// 根据shadowTls v2的方式，它一定会返回一个 firstPayload
func shadowCopyHandshakeClientToFake(fakeConn, clientConn net.Conn, hashW *utils.HashWriter) (*bytes.Buffer, error) {
	var header [5]byte
	step := 0
	var applicationDataCount int

	var firstPayload *bytes.Buffer

	for {
		if ce := utils.CanLogDebug("shadowTls2 copy "); ce != nil {
			ce.Write(zap.Int("step", step))
		}

		netLayer.SetCommonReadTimeout(clientConn)

		_, err := io.ReadFull(clientConn, header[:])

		netLayer.PersistConn(clientConn)

		if err != nil {
			if firstPayload != nil {
				utils.PutBuf(firstPayload)

			}
			return nil, utils.ErrInErr{ErrDetail: err, ErrDesc: "shadowTls2, io.ReadFull err"}
		}

		contentType := header[0]

		length := binary.BigEndian.Uint16(header[3:])
		if ce := utils.CanLogDebug("shadowTls2 copy "); ce != nil {
			ce.Write(zap.Int("step", step),
				zap.Uint8("contentType", contentType),
				zap.Uint16("length", length),
			)
		}

		if contentType == 23 {

			if firstPayload == nil {
				firstPayload = utils.GetBuf()
			}

			netLayer.SetCommonReadTimeout(clientConn)

			_, err = io.Copy(firstPayload, io.LimitReader(clientConn, int64(length)))

			netLayer.PersistRead(clientConn)

			if err != nil {
				utils.PutBuf(firstPayload)
				return nil, utils.ErrInErr{ErrDetail: err, ErrDesc: "shadowTls2, copy err1"}
			}

			if hashW.Written() && length >= 8 {

				checksum := hashW.Sum()
				first8 := firstPayload.Bytes()[:8]

				if ce := utils.CanLogDebug("shadowTls2 check "); ce != nil {
					ce.Write(zap.Int("step", step),
						zap.String("checksum", fmt.Sprintf("%v", checksum)),
						zap.String("real8", fmt.Sprintf("%v", first8)),
					)
				}

				if bytes.Equal(first8, checksum) {
					firstPayload.Next(8)
					return firstPayload, nil
				}
			}

			netLayer.SetCommonWriteTimeout(fakeConn)

			_, err = io.Copy(fakeConn, io.MultiReader(bytes.NewReader(header[:]), firstPayload))

			netLayer.PersistWrite(fakeConn)

			if err != nil {
				utils.PutBuf(firstPayload)
				return nil, utils.ErrInErr{ErrDetail: err, ErrDesc: "shadowTls2, copy err2"}
			}

			firstPayload.Reset()

			applicationDataCount++
		} else {

			netLayer.SetCommonReadTimeout(clientConn)
			netLayer.SetCommonWriteTimeout(fakeConn)

			_, err = io.Copy(fakeConn, io.MultiReader(bytes.NewReader(header[:]), io.LimitReader(clientConn, int64(length))))

			netLayer.PersistRead(clientConn)
			netLayer.PersistWrite(fakeConn)

			if err != nil {
				if firstPayload != nil {
					utils.PutBuf(firstPayload)
				}
				return nil, utils.ErrInErr{ErrDetail: err, ErrDesc: "shadowTls2, copy err3"}
			}
		}

		const maxAppDataCount = 3
		if applicationDataCount > maxAppDataCount {
			utils.PutBuf(firstPayload)

			return nil, utils.ErrFailed
		}
		step++

		if step > 8 {
			if firstPayload != nil {
				utils.PutBuf(firstPayload)
			}
			return nil, errors.New("shadowTls2 copy loop > 8, maybe under attack")

		}
	}

}

// 第一次写时写入一个hash，其余直接使用 FakeAppDataConn. 实现 utils.MultiWriter
type shadowClientConn struct {
	*FakeAppDataConn
	sum []byte
}

func (c *shadowClientConn) Write(p []byte) (n int, err error) {
	if c.sum != nil {
		sum := c.sum
		c.sum = nil
		buf := utils.GetBuf()
		if ce := utils.CanLogDebug("write hash"); ce != nil {
			ce.Write(zap.Any("sum", fmt.Sprintf("%v", sum)))
		}
		buf.Write(sum)
		buf.Write(p)

		_, err = c.FakeAppDataConn.Write(buf.Bytes())
		utils.PutBuf(buf)

		if err == nil {
			n = len(p)
		}
		return
	}
	return c.FakeAppDataConn.Write(p)
}

func (c *shadowClientConn) WriteBuffers(bss [][]byte) (int64, error) {
	if c.sum != nil {
		sum := c.sum
		c.sum = nil

		result, dup := utils.MergeBuffersWithPrefix(sum, bss)

		allDataLen := len(result) - len(sum)

		_, err := c.FakeAppDataConn.Write(result)

		if dup {
			utils.PutPacket(result)
		}
		if err != nil {
			return 0, err
		}
		return int64(allDataLen), nil

	}
	return c.FakeAppDataConn.WriteBuffers(bss)
}
