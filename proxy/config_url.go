package proxy

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/e1732a364fed/v2ray_simple/httpLayer"
	"github.com/e1732a364fed/v2ray_simple/netLayer"
	"github.com/e1732a364fed/v2ray_simple/utils"
)

const (
	UrlNativeFormat   = iota //proxy对应的标准文档所定义的url模式，一般散布于对应github的文档上
	UrlStandardFormat        //VS定义的 供 所有proxy 使用的 标准 url模式

	Url_FormatUnknown
)

var (
	// Url格式 设置以何种方式解析 命令行模式/极简模式 中出现的url配置
	//
	//关于url格式的详细， 见 docs/url.md
	UrlFormat = UrlStandardFormat
)

// try find from map, trim tail s if necessary
func GetRealProtocolFromClientUrl(s string) (u *url.URL, schemeName string, creator ClientCreator, okTls bool, err error) {
	u, err = url.Parse(s)
	if err != nil {

		err = utils.ErrInErr{ErrDesc: "Can't parse client url", ErrDetail: err, Data: s}
		return
	}

	schemeName = strings.ToLower(u.Scheme)
	var ok bool
	creator, ok = clientCreatorMap[schemeName]

	if !ok {
		schemeName = strings.TrimSuffix(schemeName, "s")
		creator, okTls = clientCreatorMap[schemeName]
		if okTls {
			ok = true
		}
	}
	if !ok {
		err = utils.ErrInErr{ErrDesc: "Unknown client protocol ", Data: u.Scheme}
	}
	return

}

// try find from map, trim tail s if necessary
func GetRealProtocolFromServerUrl(s string) (u *url.URL, schemeName string, creator ServerCreator, okTls bool, err error) {
	u, err = url.Parse(s)
	if err != nil {

		err = utils.ErrInErr{ErrDesc: "Can't parse server url", ErrDetail: err, Data: s}
		return
	}

	schemeName = strings.ToLower(u.Scheme)
	var ok bool
	creator, ok = serverCreatorMap[schemeName]

	if !ok {
		schemeName = strings.TrimSuffix(schemeName, "s")
		creator, okTls = serverCreatorMap[schemeName]
		if okTls {
			ok = true
		}
	}
	if !ok {
		err = utils.ErrInErr{ErrDesc: "Unknown server protocol ", Data: u.Scheme}
	}
	return

}

// ClientFromURL calls the registered creator to create client. The returned bool is true if has err.
func ClientFromURL(s string) (Client, error) {
	u, sn, creator, okTls, err := GetRealProtocolFromClientUrl(s)
	if err != nil {
		return nil, err
	}

	var dc *DialConf

	if UrlFormat == UrlStandardFormat {
		dc = &DialConf{}
		dc.TLS = okTls
		dc.Protocol = sn
		e := URLToDialConf(u, dc)
		if e != nil {
			return nil, e
		}

	}
	var e error
	dc, e = creator.URLToDialConf(u, dc, UrlFormat)

	if e != nil {
		return nil, e
	}

	c, e := newClient(creator, dc, false)

	if e != nil {
		return nil, e
	}

	return c, nil
}

// ServerFromURL calls the registered creator to create proxy servers.
func ServerFromURL(s string) (Server, error) {
	u, sn, creator, okTls, err := GetRealProtocolFromServerUrl(s)
	if err != nil {
		return nil, err
	}

	var lc *ListenConf

	if UrlFormat == UrlStandardFormat {
		lc = &ListenConf{}
		lc.TLS = okTls
		lc.Protocol = sn

		e := URLToListenConf(u, lc)
		if e != nil {
			return nil, e
		}
	}

	lc, err = creator.URLToListenConf(u, lc, UrlFormat)
	if err != nil {
		return nil, utils.ErrInErr{
			ErrDesc:   "URLToListenConf err ",
			ErrDetail: err,
			Data:      s,
		}
	}

	return newServer(creator, lc, false)

}

// setup conf with vs standard url format
func URLToCommonConf(u *url.URL, conf *CommonConf) error {

	if u.Scheme != DirectName {

		hn := u.Hostname()

		ip := net.ParseIP(hn)
		if ip != nil {
			conf.IP = hn
		} else {
			conf.Host = hn
		}

		if hn != u.Host { //给出了port
			colon := strings.LastIndexByte(u.Host, ':')
			p, err := strconv.Atoi(u.Host[colon+1:])
			if err != nil {
				return err
			} else if p < 0 || p > 65535 {
				return utils.ErrInvalidData
			}
			conf.Port = p

		}
	}
	q := u.Query()

	conf.Network = q.Get("network")

	conf.UUID = u.User.Username()

	conf.Fullcone = utils.QueryPositive(q, "fullcone")
	conf.Tag = u.Fragment
	conf.Path = u.Path
	conf.AdvancedLayer = q.Get("adv")
	conf.EncryptAlgo = q.Get("security")

	if q.Get("v") != "" {
		v, e := strconv.Atoi(q.Get("v"))
		if e != nil {
			return e
		}
		conf.Version = v
	}

	if utils.QueryPositive(q, "http") {
		conf.HttpHeader = &httpLayer.HeaderPreset{}
	}

	for k, list := range q {
		if strings.HasPrefix(k, "extra.") && len(list) > 0 {
			k = strings.TrimPrefix(k, "extra.")
			if conf.Extra == nil {
				conf.Extra = make(map[string]any)
			}
			conf.Extra[k] = list[0]
		}
	}

	return nil
}

func setHeaders(rawq, headers map[string][]string) {
	for k, list := range rawq {
		if strings.HasPrefix(k, "header.") && len(list) > 0 {
			k = strings.TrimPrefix(k, "header.")
			headers[k] = list
		}
	}

}

// setup conf with vs standard URL format
func URLToDialConf(u *url.URL, conf *DialConf) error {
	e := URLToCommonConf(u, &conf.CommonConf)
	if e != nil {
		return e
	}

	q := u.Query()

	if conf.HttpHeader != nil {
		rh := &httpLayer.RequestHeader{
			Method:  q.Get("http.method"),
			Version: q.Get("http.version"),
			Path:    []string{conf.Path},
			Headers: map[string][]string{},
		}
		setHeaders(q, rh.Headers)
		conf.HttpHeader.Request = rh

	}

	if conf.TLS {
		conf.Insecure = utils.QueryPositive(q, "insecure")
		//conf.Utls = utils.QueryPositive(q, "utls")
		conf.TlsType = q.Get("tls_type")

	}

	if utils.QueryPositive(q, "mux") {
		conf.Mux = true
	}

	return e
}

// setup conf with vs standard URL format
func URLToListenConf(u *url.URL, conf *ListenConf) error {
	e := URLToCommonConf(u, &conf.CommonConf)
	if e != nil {
		return e
	}

	q := u.Query()

	conf.NoRoute = utils.QueryPositive(q, "noroute")

	if f := q.Get("fallback"); f != "" {
		conf.Fallback = f
	}

	if conf.HttpHeader != nil {
		rh := &httpLayer.ResponseHeader{
			StatusCode: q.Get("http.status_code"),
			Version:    q.Get("http.version"),
			Reason:     q.Get("http.reason"),
			Headers:    map[string][]string{},
		}
		setHeaders(q, rh.Headers)
		conf.HttpHeader.Response = rh

	}

	if conf.TLS {
		conf.Insecure = utils.QueryPositive(q, "insecure")

		certFile := q.Get("cert")
		keyFile := q.Get("key")

		conf.TLSCert = certFile
		conf.TLSKey = keyFile
	}

	targetStr := q.Get("target.ip")

	if targetStr != "" {
		target_portStr := q.Get("target.port")
		if target_portStr != "" {
			if conf.Network == "" {
				conf.Network = "tcp"
			}
			taStr := conf.Network + "://" + targetStr + ":" + target_portStr
			conf.TargetAddr = taStr
		}

	}

	return e
}

// convert DialConf or ListenConf to verysimple Official URL format.
// cc must not be nil or it will panic.
// See docs/url.md and https://github.com/e1732a364fed/v2ray_simple/discussions/163
func ToStandardUrl(cc *CommonConf, dc *DialConf, lc *ListenConf) string {
	var u url.URL

	u.Scheme = cc.Protocol
	if cc.TLS {
		u.Scheme += "s"
	}

	u.User = url.User(cc.UUID)
	if cc.IP != "" {
		u.Host = cc.IP + ":" + strconv.Itoa(cc.Port)
	} else {
		u.Host = cc.Host + ":" + strconv.Itoa(cc.Port)

	}
	if cc.Path != "" {
		u.Path = cc.Path
	}

	q := u.Query()
	if cc.Network != "" {
		q.Add("network", cc.Network)

	}

	if cc.Fullcone {
		q.Add("fullcone", "true")
	}

	if lc != nil {
		if lc.TargetAddr != "" {
			a, e := netLayer.NewAddrFromAny(lc.TargetAddr)
			if e == nil {
				q.Add("target.ip", a.IP.String())
				q.Add("target.network", a.Network)
				q.Add("target.port", strconv.Itoa(a.Port))
			}
		}
	}

	if dc != nil {
		if dc.SendThrough != "" {
			q.Add("sendThrough", dc.SendThrough)
		}
		if dc.Mux {
			q.Add("mux", "true")
		}
	}

	if cc.TLS {
		if cc.Insecure {
			q.Add("insecure", "true")
		}
		// if dc != nil && dc.Utls {
		// 	q.Add("utls", "true")
		// }
		if dc.TlsType != "" {
			q.Add("tls_type", dc.TlsType)
		}
		if cc.TLSCert != "" {
			q.Add("cert", cc.TLSCert)
		}
		if cc.TLSKey != "" {
			q.Add("key", cc.TLSKey)
		}
	}

	if hh := cc.HttpHeader; hh != nil {

		q.Add("http", "true")

		if r := hh.Request; r != nil {

			if r.Method != "" {
				q.Add("http.method", r.Method)
			}
			if r.Version != "" {
				q.Add("http.version", r.Version)
			}

			for k, headers := range r.Headers {

				q.Add("header."+k, strings.Join(headers, ", "))
			}
		}
	}

	if cc.AdvancedLayer != "" {
		q.Add("adv", cc.AdvancedLayer)
	}

	if cc.EncryptAlgo != "" {
		q.Add("security", cc.EncryptAlgo)
	}

	u.RawQuery = q.Encode()
	if cc.Tag != "" {
		u.Fragment = cc.Tag

	}

	return u.String()
}
