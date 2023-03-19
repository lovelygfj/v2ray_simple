package machine

import (
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/e1732a364fed/v2ray_simple"
	"github.com/e1732a364fed/v2ray_simple/proxy"
	"github.com/e1732a364fed/v2ray_simple/utils"
	"go.uber.org/zap"
)

func (m *M) LoadDialConf(conf []*proxy.DialConf) (ok bool) {
	ok = true

	for _, d := range conf {

		if d.UUID == "" && m.DefaultUUID != "" {
			d.UUID = m.DefaultUUID
		}

		outClient, err := proxy.NewClient(d)
		if err != nil {
			if ce := utils.CanLogErr("can not create outClient: "); ce != nil {
				ce.Write(zap.Error(err), zap.Any("raw", d))
			}
			ok = false
			continue
		}

		m.allClients = append(m.allClients, outClient)
		if tag := outClient.GetTag(); tag != "" {
			m.tryInitEnv()
			m.routingEnv.SetClient(tag, outClient)

		}
	}

	if len(m.allClients) > 0 {
		m.DefaultOutClient = m.allClients[0]

	} else {
		m.DefaultOutClient = v2ray_simple.DirectClient
	}
	return

}

// 试图通过 conf数组创建server，并保存到m中。若 hot = false, 如果有任何一个server创建出错，则不会有任何server被保存入m。
// 若 hot=true, 监听每一个成功创建的server. hot = true只有在 m.IsRunning() 时有效，否则视为 false
func (m *M) LoadListenConf(conf []*proxy.ListenConf, hot bool) (ok bool) {
	ok = true

	if m.DefaultOutClient == nil {
		m.DefaultOutClient = v2ray_simple.DirectClient
	}

	var tmplist []proxy.Server

	h_r := hot && m.running

	for _, l := range conf {
		if l.UUID == "" && m.DefaultUUID != "" {
			l.UUID = m.DefaultUUID
		}

		inServer, err := proxy.NewServer(l)
		if err != nil {

			if ce := utils.CanLogErr("Can not create listen server"); ce != nil {
				ce.Write(zap.Error(err), zap.Any("raw", l))
			}
			ok = false
			continue
		}

		if h_r {
			lis := v2ray_simple.ListenSer(inServer, m.DefaultOutClient, &m.routingEnv, &m.GlobalInfo)
			if lis != nil {
				m.listenCloserList = append(m.listenCloserList, lis)
				m.allServers = append(m.allServers, inServer)

			} else {
				ok = false
			}
		} else {
			tmplist = append(tmplist, inServer)

		}
	}

	if ok && !h_r {
		m.allServers = append(m.allServers, tmplist...)
	}

	return
}

func (m *M) RemoveAllClient() {
	count := m.ClientCount()

	for i := 0; i < count; i++ {
		m.HotDeleteClient(0)
	}
}

func (m *M) RemoveAllServer() {
	count := m.ServerCount()

	for i := 0; i < count; i++ {
		m.HotDeleteServer(0)
	}
}

// delete and stop the client
func (m *M) HotDeleteClient(index int) {
	if index < 0 || index >= len(m.allClients) {
		return
	}

	doomedClient := m.allClients[index]

	m.routingEnv.DelClient(doomedClient.GetTag())
	doomedClient.Stop()
	m.allClients = utils.TrimSlice(m.allClients, index)
}

// delete and close the server
func (m *M) HotDeleteServer(index int) {
	if index < 0 || index >= len(m.allServers) {
		return
	}
	running := m.IsRunning()
	if running {
		if index >= len(m.listenCloserList) {
			return
		}
	}

	m.allServers[index].Stop()
	m.allServers = utils.TrimSlice(m.allServers, index)

	if running {
		m.listenCloserList[index].Close()
		m.listenCloserList = utils.TrimSlice(m.listenCloserList, index)
	}
}

func (m *M) loadUrlConf(hot bool) (result int) {
	var ser proxy.Server
	result, ser = m.loadUrlServer(m.urlConf)
	if result < 0 {
		return
	}
	var cli proxy.Client
	result, cli = m.loadUrlClient(m.urlConf)
	if result < 0 {
		return
	}

	if hot {
		lis := v2ray_simple.ListenSer(ser, cli, &m.routingEnv, &m.GlobalInfo)
		if lis != nil {
			m.listenCloserList = append(m.listenCloserList, lis)
		} else {
			result = -1
		}
	} else {
		m.DefaultOutClient = cli
	}

	return
}

// load failed if result <0,
func (m *M) loadUrlServer(urlConf proxy.UrlConf) (result int, server proxy.Server) {
	var e error
	server, e = proxy.ServerFromURL(urlConf.ListenUrl)
	if e != nil {
		if ce := utils.CanLogErr("can not create local server"); ce != nil {
			ce.Write(zap.String("error", e.Error()))
		}
		result = -1
		return
	}

	m.allServers = append(m.allServers, server)

	return
}

func (m *M) loadUrlClient(urlConf proxy.UrlConf) (result int, client proxy.Client) {
	var e error
	client, e = proxy.ClientFromURL(urlConf.DialUrl)
	if e != nil {
		if ce := utils.CanLogErr("can not create remote client"); ce != nil {
			ce.Write(zap.String("error", e.Error()))
		}
		result = -1
		return
	}

	m.allClients = append(m.allClients, client)
	return
}

// 从当前内存中的配置 导出 VSConf
func (m *M) DumpVSConf() (vc VSConf) {
	vc.StandardConf = m.DumpStandardConf()
	vc.ApiServerConf = &m.ApiServerConf
	vc.AppConf = &m.AppConf

	return
}

// 从当前内存中的配置 导出 proxy.StandardConf
func (m *M) DumpStandardConf() (sc proxy.StandardConf) {
	for i := range m.allClients {
		dc := m.dumpDialConf(i)
		sc.Dial = append(sc.Dial, &dc)

	}
	for i := range m.allServers {
		lc := m.dumpListenConf(i)
		sc.Listen = append(sc.Listen, &lc)

	}

	return
}

func (m *M) dumpDialConf(i int) (dc proxy.DialConf) {
	c := m.allClients[i]
	dc = *c.GetBase().DialConf

	return
}

func (m *M) dumpListenConf(i int) (lc proxy.ListenConf) {
	c := m.allServers[i]
	lc = *c.GetBase().ListenConf

	return
}

func (m *M) HotLoadDialUrl(theUrlStr string, format int) error {
	u, sn, creator, okTls, err := proxy.GetRealProtocolFromClientUrl(theUrlStr)
	if err != nil {
		fmt.Printf("parse url failed %v\n", err)
		return err
	}
	dc := &proxy.DialConf{}
	dc.Protocol = sn

	dc.TLS = okTls
	err = proxy.URLToDialConf(u, dc)
	if err != nil {
		fmt.Printf("parse url failed %v\n", err)
		return err
	}
	dc, err = creator.URLToDialConf(u, dc, format)
	if err != nil {
		fmt.Printf("parse url step 2 failed %v\n", err)
		return err
	}

	if !m.LoadDialConf([]*proxy.DialConf{dc}) {
		return utils.ErrFailed
	}
	return nil

}

// 热加载url格式的listen配置。format为1表示vs标准url格式, 0表示协议原生url格式，一般为1。
func (m *M) HotLoadListenUrl(theUrlStr string, format int) error {
	u, sn, creator, okTls, err := proxy.GetRealProtocolFromServerUrl(theUrlStr)
	if err != nil {
		fmt.Printf("parse url failed %v\n", err)
		return err
	}

	lc := &proxy.ListenConf{}
	lc.Protocol = sn

	lc.TLS = okTls

	err = proxy.URLToListenConf(u, lc)
	if err != nil {
		fmt.Printf("parse url failed %v\n", err)
		return err
	}
	lc, err = creator.URLToListenConf(u, lc, format)
	if err != nil {
		fmt.Printf("parse url step 2 failed %v\n", err)
		return err
	}
	if !m.LoadListenConf([]*proxy.ListenConf{lc}, true) {
		return utils.ErrFailed
	}
	return nil
}

// 热加载toml格式的listen配置
func (m *M) HotLoadListenConfStr(theStr string) error {
	bs := []byte(theStr)
	bs = utils.ReplaceBytesSynonyms(bs, proxy.StandardConfBytesSynonyms)
	var lc proxy.ListenConf
	err := toml.Unmarshal(bs, &lc)
	if err != nil {
		return err
	}
	if !m.LoadListenConf([]*proxy.ListenConf{&lc}, true) {
		return utils.ErrFailed
	}
	return nil

}

func (m *M) HotLoadDialConfStr(theStr string) error {
	bs := []byte(theStr)
	bs = utils.ReplaceBytesSynonyms(bs, proxy.StandardConfBytesSynonyms)
	var lc proxy.DialConf
	err := toml.Unmarshal(bs, &lc)
	if err != nil {
		return err
	}
	if !m.LoadDialConf([]*proxy.DialConf{&lc}) {
		return utils.ErrFailed
	}
	return nil

}
